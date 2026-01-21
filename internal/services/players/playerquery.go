package players

import (
	"context"
	"fmt"
	"log/slog"
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

	db *PlayerDatabase
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
	}
}

// Start begins the service lifecycle
func (s *PlayerQueryService) Start(ctx context.Context) error {
	slog.Info("Starting PlayerQueryService")

	// Perform cold start: load all existing logs BEFORE streaming
	if err := s.performColdStart(ctx); err != nil {
		return fmt.Errorf("cold start failed: %w", err)
	}

	// Start streaming updates (after cold start completes)
	go s.streamAdminLogs(ctx)
	go s.streamJoinLogs(ctx)
	go s.streamProfileUpdates(ctx)

	slog.Info("PlayerQueryService started successfully")
	return nil
}

// performColdStart loads all existing log data from in-memory storage
func (s *PlayerQueryService) performColdStart(ctx context.Context) error {
	slog.Info("Performing cold start - loading existing logs from memory")

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

// Streaming methods are defined in playerquery_streaming.go
