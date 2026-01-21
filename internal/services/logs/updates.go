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
	entryToProto  func(*T, uint64) *V
	updateStreams []UpdateService_UpdateLogsServer[V]
	logPath       string
	mutex         sync.RWMutex

	// In-memory storage
	entries   []*T
	entriesMu sync.RWMutex

	// Dual counter tracking
	totalLinesRead      uint64 // Total lines read from file (including unparseable)
	parsedEntriesCount  uint64 // Number of successfully parsed entries
}

func newUpdateService[
	T logs.AdminEntry | logs.JoinEntry | logs.PlayerProfileEntry,
	V v1.AdminLogUpdatesResponse | v1.JoinLogUpdatesResponse | v1.PlayerProfilesUpdatesResponse,
](logPath string, parser func(string) (*T, error), entryToProto func(*T, uint64) *V) (*updateService[T, V], error) {
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

	log.Printf("Loaded %d parsed entries from %d total lines in %s", s.parsedEntriesCount, s.totalLinesRead, logPath)

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
		s.totalLinesRead++
		line := scanner.Text()

		entry, err := s.logParser(line)
		if err != nil {
			// Skip unparseable lines but still count total lines
			continue
		}

		s.entries = append(s.entries, entry)
		s.parsedEntriesCount++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log file: %w", err)
	}

	log.Printf("Parsed %d/%d lines from %s", s.parsedEntriesCount, s.totalLinesRead, s.logPath)

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

func (s *updateService[T, V]) startTailing(sub UpdateService_UpdateLogsServer[V], fromParsedLine uint64) error {
	// First, send buffered entries from the requested line
	s.entriesMu.RLock()
	var bufferedEntries []*T
	startingLineNum := fromParsedLine

	if fromParsedLine > 0 && fromParsedLine <= s.parsedEntriesCount {
		// Client wants to resume from a specific line (1-based)
		// Send entries starting from fromParsedLine (index fromParsedLine-1)
		bufferedEntries = s.entries[fromParsedLine-1:]
		startingLineNum = fromParsedLine
	} else if fromParsedLine == 0 {
		// Client wants all entries from the beginning
		bufferedEntries = s.entries
		startingLineNum = 1
	}
	// If fromParsedLine > parsedEntriesCount, bufferedEntries remains nil (no buffered entries to send)

	s.entriesMu.RUnlock()

	// Send buffered entries
	for i, entry := range bufferedEntries {
		resp := s.entryToProto(entry, startingLineNum+uint64(i))
		if err := sub.Send(resp); err != nil {
			return err
		}
	}

	// Now register for live updates
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
		Location:      &tail.SeekInfo{Offset: int64(s.totalLinesRead), Whence: 0}, // Start at end of file
	})
	if err != nil {
		return err
	}

	go func() {
		defer tailer.Stop()
		for line := range tailer.Lines {
			// Always increment total lines read
			s.entriesMu.Lock()
			s.totalLinesRead++
			s.entriesMu.Unlock()

			entry, err := s.logParser(line.Text)
			if err != nil {
				// Skip unparseable lines but continue tracking total lines
				continue
			}

			// Append new entry to in-memory storage and increment parsed count
			s.entriesMu.Lock()
			s.entries = append(s.entries, entry)
			s.parsedEntriesCount++
			currentParsedLine := s.parsedEntriesCount
			s.entriesMu.Unlock()

			resp := s.entryToProto(entry, currentParsedLine)

			toRemove := make([]int, 0)
			s.mutex.RLock()
			for i, sub := range s.updateStreams {
				err = sub.Send(resp)
				if err != nil {
					toRemove = append(toRemove, i)
				}
			}
			s.mutex.RUnlock()

			// Remove failed streams in reverse order to maintain correct indices
			s.mutex.Lock()
			for i := len(toRemove) - 1; i >= 0; i-- {
				idx := toRemove[i]
				s.updateStreams = append(s.updateStreams[:idx], s.updateStreams[idx+1:]...)
			}
			s.mutex.Unlock()

		}
	}()

	return nil
}
