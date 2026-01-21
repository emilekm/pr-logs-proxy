package players

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Alliance-Community/pr-logs-proxy/internal/services/logs"
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
)

var _ v1.PlayerQueryServiceServer = (*PlayerQueryService)(nil)

// nowFunc is a variable for testing time-based logic
var nowFunc = time.Now

// PlayerQueryService aggregates player data from all log sources
type PlayerQueryService struct {
	v1.UnimplementedPlayerQueryServiceServer

	adminLogService       *logs.AdminLogService
	joinLogService        *logs.JoinLogService
	playerProfilesService *logs.PlayerProfilesService

	db              *PlayerDatabase
	coldStartDone   bool
	coldStartMu     sync.RWMutex
	bufferedEntries *bufferedEntries
}

type bufferedEntries struct {
	adminLogs      []*v1.AdminLogEntry
	joinLogs       []*v1.JoinLogEntry
	profileUpdates []*v1.PlayerProfileEntry
	mu             sync.Mutex
}

// NewPlayerQueryService creates a new PlayerQueryService
func NewPlayerQueryService(
	adminLogService *logs.AdminLogService,
	joinLogService *logs.JoinLogService,
	playerProfilesService *logs.PlayerProfilesService,
) *PlayerQueryService {
	return &PlayerQueryService{
		adminLogService:       adminLogService,
		joinLogService:        joinLogService,
		playerProfilesService: playerProfilesService,
		db:                    newPlayerDatabase(),
		bufferedEntries: &bufferedEntries{
			adminLogs:      make([]*v1.AdminLogEntry, 0),
			joinLogs:       make([]*v1.JoinLogEntry, 0),
			profileUpdates: make([]*v1.PlayerProfileEntry, 0),
		},
	}
}

// Start begins the service lifecycle
func (s *PlayerQueryService) Start(ctx context.Context) error {
	slog.Info("Starting PlayerQueryService")

	// Start streaming updates immediately (buffer during cold start)
	go s.streamAdminLogs(ctx)
	go s.streamJoinLogs(ctx)
	go s.streamProfileUpdates(ctx)

	// Perform cold start: load all existing logs
	if err := s.performColdStart(ctx); err != nil {
		return fmt.Errorf("cold start failed: %w", err)
	}

	// Merge buffered entries
	s.mergeBufferedEntries()

	slog.Info("PlayerQueryService started successfully")
	return nil
}

// performColdStart loads all existing log data
func (s *PlayerQueryService) performColdStart(ctx context.Context) error {
	slog.Info("Performing cold start - loading existing logs")

	// Load all join logs first (contains key player data)
	if err := s.loadJoinLogs(ctx); err != nil {
		return fmt.Errorf("failed to load join logs: %w", err)
	}

	// Load player profiles
	if err := s.loadPlayerProfiles(ctx); err != nil {
		return fmt.Errorf("failed to load player profiles: %w", err)
	}

	// Load admin logs
	if err := s.loadAdminLogs(ctx); err != nil {
		return fmt.Errorf("failed to load admin logs: %w", err)
	}

	s.coldStartMu.Lock()
	s.coldStartDone = true
	s.coldStartMu.Unlock()

	slog.Info("Cold start completed", slog.Int("players", len(s.db.players)))
	return nil
}

// loadJoinLogs loads all join log entries
func (s *PlayerQueryService) loadJoinLogs(ctx context.Context) error {
	resp, err := s.joinLogService.JoinLogs(ctx, &v1.JoinLogsRequest{})
	if err != nil {
		return err
	}

	for _, entry := range resp.Entries {
		s.processJoinLogEntry(entry)
	}

	slog.Info("Loaded join logs", slog.Int("entries", len(resp.Entries)))
	return nil
}

// loadPlayerProfiles loads all player profile entries
func (s *PlayerQueryService) loadPlayerProfiles(ctx context.Context) error {
	resp, err := s.playerProfilesService.PlayerProfilesLogs(ctx, &v1.PlayerProfilesLogsRequest{})
	if err != nil {
		return err
	}

	for _, entry := range resp.Entries {
		s.processPlayerProfileEntry(entry)
	}

	slog.Info("Loaded player profiles", slog.Int("entries", len(resp.Entries)))
	return nil
}

// loadAdminLogs loads all admin log entries
func (s *PlayerQueryService) loadAdminLogs(ctx context.Context) error {
	resp, err := s.adminLogService.AdminsLogs(ctx, &v1.AdminsLogsRequest{})
	if err != nil {
		return err
	}

	for _, entry := range resp.Entries {
		s.processAdminLogEntry(entry)
	}

	slog.Info("Loaded admin logs", slog.Int("entries", len(resp.Entries)))
	return nil
}

// mergeBufferedEntries merges buffered entries into the database
func (s *PlayerQueryService) mergeBufferedEntries() {
	s.bufferedEntries.mu.Lock()
	defer s.bufferedEntries.mu.Unlock()

	slog.Info("Merging buffered entries",
		slog.Int("admin_logs", len(s.bufferedEntries.adminLogs)),
		slog.Int("join_logs", len(s.bufferedEntries.joinLogs)),
		slog.Int("profile_updates", len(s.bufferedEntries.profileUpdates)))

	for _, entry := range s.bufferedEntries.joinLogs {
		s.processJoinLogEntry(entry)
	}

	for _, entry := range s.bufferedEntries.profileUpdates {
		s.processPlayerProfileEntry(entry)
	}

	for _, entry := range s.bufferedEntries.adminLogs {
		s.processAdminLogEntry(entry)
	}

	// Clear buffers
	s.bufferedEntries.adminLogs = nil
	s.bufferedEntries.joinLogs = nil
	s.bufferedEntries.profileUpdates = nil
}

// Streaming methods are defined in playerquery_streaming.go
