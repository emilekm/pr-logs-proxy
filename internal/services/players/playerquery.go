package players

import (
	"fmt"
	"log/slog"

	"github.com/Alliance-Community/pr-logs-proxy/internal/services/logs"
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
)

var _ v1.PlayerQueryServiceServer = (*PlayerQueryService)(nil)

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
func (s *PlayerQueryService) Start() error {
	slog.Info("Starting PlayerQueryService")

	// Perform cold start: load all existing logs BEFORE streaming
	if err := s.performColdStart(); err != nil {
		return fmt.Errorf("cold start failed: %w", err)
	}

	// Start streaming updates (after cold start completes)
	go s.streamAdminLogs()
	go s.streamJoinLogs()
	go s.streamProfileUpdates()

	slog.Info("PlayerQueryService started successfully")
	return nil
}

// performColdStart loads all existing log data from in-memory storage
func (s *PlayerQueryService) performColdStart() error {
	slog.Info("Performing cold start - loading existing logs from memory")

	// Load all join logs first (contains key player data)
	if err := s.loadJoinLogs(); err != nil {
		return fmt.Errorf("failed to load join logs: %w", err)
	}

	// Load player profiles
	if err := s.loadPlayerProfiles(); err != nil {
		return fmt.Errorf("failed to load player profiles: %w", err)
	}

	// Load admin logs
	if err := s.loadAdminLogs(); err != nil {
		return fmt.Errorf("failed to load admin logs: %w", err)
	}

	slog.Info("Cold start completed", slog.Int("players", len(s.db.players)))
	return nil
}

// loadJoinLogs loads all join log entries
func (s *PlayerQueryService) loadJoinLogs() error {
	entries := s.joinLogService.GetAllEntries()

	for _, entry := range entries {
		s.processJoinLogEntry(entry)
	}

	slog.Info("Loaded join logs", slog.Int("entries", len(entries)))
	return nil
}

// loadPlayerProfiles loads all player profile entries
func (s *PlayerQueryService) loadPlayerProfiles() error {
	entries := s.playerProfilesService.GetAllEntries()

	for _, entry := range entries {
		s.processPlayerProfileEntry(entry)
	}

	slog.Info("Loaded player profiles", slog.Int("entries", len(entries)))
	return nil
}

// loadAdminLogs loads all admin log entries
func (s *PlayerQueryService) loadAdminLogs() error {
	entries := s.adminLogService.GetAllEntries()

	for _, entry := range entries {
		s.processAdminLogEntry(entry)
	}

	slog.Info("Loaded admin logs", slog.Int("entries", len(entries)))
	return nil
}
