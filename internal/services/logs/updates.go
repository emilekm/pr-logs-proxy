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
	logParser     func(string) (*T, error)
	entryToProto  func(*T) *V
	updateStreams []UpdateService_UpdateLogsServer[V]
	logPath       string
	mutex         sync.RWMutex

	// In-memory storage
	entries   []*T
	entriesMu sync.RWMutex
	lineCount int64
}

func newUpdateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
](logPath string, parser func(string) (*T, error), entryToProto func(*T) *V) (*updateService[T, V], error) {
	s := &updateService[T, V]{
		logParser:     parser,
		entryToProto:  entryToProto,
		updateStreams: make([]UpdateService_UpdateLogsServer[V], 0),
		logPath:       logPath,
		entries:       make([]*T, 0),
	}

	// Load all entries from file into memory
	if err := s.loadAllEntries(); err != nil {
		return nil, fmt.Errorf("failed to load entries from %s: %w", logPath, err)
	}

	// Start tailing the log file for new entries
	if err := s.startTailer(); err != nil {
		return nil, fmt.Errorf("failed to start tailer for %s: %w", logPath, err)
	}

	log.Printf("Loaded %d entries from %s", s.lineCount, logPath)

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
	lineCount := int64(0)

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		entry, err := s.logParser(line)
		if err != nil {
			// Skip unparseable lines
			continue
		}

		s.entries = append(s.entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	s.lineCount = lineCount
	log.Printf("Parsed %d/%d lines from %s", len(s.entries), lineCount, s.logPath)

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
func (s *updateService[T, V]) GetEntryCount() int {
	s.entriesMu.RLock()
	defer s.entriesMu.RUnlock()
	return len(s.entries)
}

func (s *updateService[T, V]) startTailing(sub UpdateService_UpdateLogsServer[V]) error {
	s.mutex.Lock()
	s.updateStreams = append(s.updateStreams, sub)
	s.mutex.Unlock()

	<-sub.Context().Done()

	return nil
}

func (s *updateService[T, V]) startTailer() error {
	// Start tailing from the line after the last loaded line
	tailer, err := tail.TailFile(s.logPath, tail.Config{
		Follow:        true,
		CompleteLines: true,
		Location:      &tail.SeekInfo{Offset: s.lineCount, Whence: 0}, // Start at end of file
	})
	if err != nil {
		return err
	}

	go func() {
		defer tailer.Stop()
		for line := range tailer.Lines {
			entry, err := s.logParser(line.Text)
			if err != nil {
				continue
			}

			// Append new entry to in-memory storage
			s.entriesMu.Lock()
			s.entries = append(s.entries, entry)
			s.lineCount++
			s.entriesMu.Unlock()

			resp := s.entryToProto(entry)

			toRemove := make([]int, 0)
			s.mutex.RLock()
			for i, sub := range s.updateStreams {
				err = sub.Send(resp)
				if err != nil {
					toRemove = append(toRemove, i)
				}
			}
			s.mutex.RUnlock()

			s.mutex.Lock()
			for _, i := range toRemove {
				s.updateStreams = append(s.updateStreams[:i], s.updateStreams[i+1:]...)
			}
			s.mutex.Unlock()

		}
	}()

	return nil
}
