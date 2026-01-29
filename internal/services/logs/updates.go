package logs

import (
	"fmt"
	"io"
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
	location     int64
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

	// Start tailing the log file for new entries
	if err := s.startTailer(); err != nil {
		return nil, fmt.Errorf("failed to start tailer for %s: %w", logPath, err)
	}

	log.Printf("Loaded %d parsed entries in %s", s.totalEntries, logPath)

	return s, nil
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
	waitCh := make(chan struct{})
	initial := true

	file, err := os.Open(s.logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	currentEnd, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end of log file: %w", err)
	}

	tailer, err := tail.TailFile(s.logPath, tail.Config{
		Follow:        true,
		CompleteLines: true,
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

			if initial && line.SeekInfo.Offset >= currentEnd {
				initial = false
				waitCh <- struct{}{}
			}

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

	<-waitCh

	return nil
}
