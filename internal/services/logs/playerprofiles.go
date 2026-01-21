package logs

import (
	"context"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

var _ v1.PlayerProfilesServiceServer = (*PlayerProfilesService)(nil)

type PlayerProfilesService struct {
	v1.UnimplementedPlayerProfilesServiceServer
	*updateService[logs.PlayerProfileEntry, v1.PlayerProfilesUpdatesResponse]
	logPath string
}

func NewPlayerProfilesService(logPath string) (*PlayerProfilesService, error) {
	updateSvc, err := newUpdateService(
		logPath, logs.ParsePlayerProfileEntry,
		func(entry *logs.PlayerProfileEntry) *v1.PlayerProfilesUpdatesResponse {
			return &v1.PlayerProfilesUpdatesResponse{
				Entry: playerProfileEntryToProto(entry),
			}
		},
	)
	if err != nil {
		return nil, err
	}

	return &PlayerProfilesService{
		updateService: updateSvc,
		logPath:       logPath,
	}, nil
}

func (s *PlayerProfilesService) PlayerProfileUpdates(req *v1.PlayerProfilesUpdatesRequest, stream v1.PlayerProfilesService_PlayerProfilesUpdatesServer) error {
	return s.startTailing(stream)
}

func (s *PlayerProfilesService) PlayerProfiles(ctx context.Context, req *v1.PlayerProfilesLogsRequest) (*v1.PlayerProfilesLogsResponse, error) {
	// Get all entries from in-memory storage
	allEntries := s.GetAllEntries()

	// Convert to protobuf format
	entries := make([]*v1.PlayerProfileEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		entries = append(entries, playerProfileEntryToProto(entry))
	}

	return &v1.PlayerProfilesLogsResponse{
		Entries: entries,
	}, nil
}

func playerProfileEntryToProto(entry *logs.PlayerProfileEntry) *v1.PlayerProfileEntry {
	return &v1.PlayerProfileEntry{
		Timestamp:  entry.Timestamp.Unix(),
		KeyHash:    entry.KeyHash,
		TrustLevel: uint32(entry.TrustLevel),
		Username:   entry.Username,
	}
}
