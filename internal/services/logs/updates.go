package logs

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sync"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
	"github.com/nxadm/tail"
	"google.golang.org/grpc"
)

type UpdateService_UpdateLogsServer[
	T v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
] interface {
	Send(*T) error
	grpc.ServerStream
}

type updateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
] struct {
	logParser      func(string) (*T, error)
	entryToProto   func(*T, uint64) *V
	updateStreams  []UpdateService_UpdateLogsServer[V]
	updateChannels []chan *T
	logPath        string
	streamsMutex   sync.RWMutex

	// In-memory storage
	entries      []*T
	totalEntries uint64
	entriesMu    sync.RWMutex
}

func newUpdateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
](logPath string, parser func(string) (*T, error), entryToProto func(*T, uint64) *V) (*updateService[T, V], error) {
	s := &updateService[T, V]{
		logParser:      parser,
		entryToProto:   entryToProto,
		updateStreams:  make([]UpdateService_UpdateLogsServer[V], 0),
		updateChannels: make([]chan *T, 0),
		logPath:        logPath,
		entries:        make([]*T, 0),
	}

	// Load all entries from file into memory
	if err := s.loadAllEntries(); err != nil {
		return nil, fmt.Errorf("failed to load entries from %s: %w", logPath, err)
	}

	// Start tailing the log file for new entries
	if err := s.startTailer(); err != nil {
		return nil, fmt.Errorf("failed to start tailer for %s: %w", logPath, err)
	}

	log.Printf("Loaded %d parsed entries in %s", s.totalEntries, logPath)

	return s, nil
}

// loadAllEntries reads the entire log file and parses all entries into memory
func (s *updateService[T, V]) loadAllEntries() error {
	file, err := os.Open(s.logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		entry, err := s.logParser(line)
		if err != nil {
			entry = new(T)
		}

		s.entries = append(s.entries, entry)
		s.totalEntries++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	log.Printf("Parsed %d lines from %s", s.totalEntries, s.logPath)

	return nil
}

// GetAllEntries returns a copy of all entries in memory
func (s *updateService[T, V]) GetAllEntries() []*T {
	s.entriesMu.RLock()
	defer s.entriesMu.RUnlock()

	// Return a copy to prevent external modification
	entriesCopy := make([]*T, len(s.entries))
	copy(entriesCopy, s.entries)
	return entriesCopy
}

// GetEntryCount returns the number of entries currently in memory
func (s *updateService[T, V]) GetEntryCount() uint64 {
	s.entriesMu.RLock()
	defer s.entriesMu.RUnlock()
	return s.totalEntries
}

func (s *updateService[T, V]) Updates() chan *T {
	ch := make(chan *T, 10)

	s.streamsMutex.Lock()
	s.updateChannels = append(s.updateChannels, ch)
	s.streamsMutex.Unlock()

	return ch
}

func (s *updateService[T, V]) startTailing(sub UpdateService_UpdateLogsServer[V], fromParsedLine *uint64) error {
	// Register for live updates
	s.streamsMutex.Lock()
	s.updateStreams = append(s.updateStreams, sub)
	s.streamsMutex.Unlock()

	// Send buffered entries from the requested line
	s.entriesMu.RLock()
	var bufferedEntries []*T

	startingLineNum := uint64(1)

	if fromParsedLine != nil {
		// Adjust for 1-based line numbers in requests
		if *fromParsedLine > 0 {
			startingLineNum = *fromParsedLine
		}

		currentParsedLine := s.totalEntries
		if startingLineNum < currentParsedLine {
			bufferedEntries = s.entries[startingLineNum:currentParsedLine]
		}
	}
	s.entriesMu.RUnlock()

	// Send buffered entries
	for i, entry := range bufferedEntries {
		resp := s.entryToProto(entry, startingLineNum+uint64(i))
		if err := sub.Send(resp); err != nil {
			return err
		}
	}

	<-sub.Context().Done()

	return nil
}

func (s *updateService[T, V]) startTailer() error {
	// Start tailing from the line after the last loaded line
	tailer, err := tail.TailFile(s.logPath, tail.Config{
		Follow:        true,
		CompleteLines: true,
		Location:      &tail.SeekInfo{Offset: int64(s.totalEntries), Whence: 0}, // Start at end of file
	})
	if err != nil {
		return err
	}

	go func() {
		defer tailer.Stop()
		for line := range tailer.Lines {
			entry, err := s.logParser(line.Text)
			if err != nil {
				entry = new(T)
			}

			// Append new entry to in-memory storage and increment parsed count
			s.entriesMu.Lock()
			s.entries = append(s.entries, entry)
			s.totalEntries++
			currentLineNum := s.totalEntries
			s.entriesMu.Unlock()

			resp := s.entryToProto(entry, currentLineNum)

			s.streamsMutex.RLock()
			if len(s.updateStreams) == 0 {
				s.streamsMutex.RUnlock()
				continue
			}

			toRemove := make([]int, 0)
			for i, sub := range s.updateStreams {
				err = sub.Send(resp)
				if err != nil {
					toRemove = append(toRemove, i)
				}
			}
			s.streamsMutex.RUnlock()

			// Remove failed streams in reverse order to maintain correct indices
			if len(toRemove) > 0 {
				s.streamsMutex.Lock()
				for i := len(toRemove) - 1; i >= 0; i-- {
					idx := toRemove[i]
					s.updateStreams = append(s.updateStreams[:idx], s.updateStreams[idx+1:]...)
				}
				s.streamsMutex.Unlock()
			}
		}
	}()

	return nil
}
