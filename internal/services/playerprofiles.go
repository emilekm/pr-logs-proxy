package services

import (
	"bufio"
	"context"
	"os"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

var _ v1.PlayerProfilesServiceServer = (*PlayerProfilesService)(nil)

type PlayerProfilesService struct {
	v1.UnimplementedPlayerProfilesServiceServer
	*updateService[logs.PlayerProfileEntry, v1.PlayerProfilesUpdatesResponse]
	logPath string
}

func NewPlayerProfilesService(logPath string) *PlayerProfilesService {
	return &PlayerProfilesService{
		updateService: newUpdateService(
			logPath, logs.ParsePlayerProfileEntry,
			func(entry *logs.PlayerProfileEntry) *v1.PlayerProfilesUpdatesResponse {
				return &v1.PlayerProfilesUpdatesResponse{
					Entry: playerProfileEntryToProto(entry),
				}
			},
		),
		logPath: logPath,
	}
}

func (s *PlayerProfilesService) PlayerProfileUpdates(req *v1.PlayerProfilesUpdatesRequest, stream v1.PlayerProfilesService_PlayerProfilesUpdatesServer) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updateStreams = append(s.updateStreams, stream)
	err := s.startTailing()
	return err
}

func (s *PlayerProfilesService) PlayerProfiles(ctx context.Context, req *v1.PlayerProfilesLogsRequest) (*v1.PlayerProfilesLogsResponse, error) {
	file, err := os.Open(s.logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]*v1.PlayerProfileEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, err := logs.ParsePlayerProfileEntry(line)
		if err != nil {
			continue
		}

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
