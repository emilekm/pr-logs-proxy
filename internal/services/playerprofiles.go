package services

import (
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/Alliance-Community/pr-logs-proxy/pkg/parsers"
)

var _ v1.PlayerProfileServiceServer = (*PlayerProfilesService)(nil)

type PlayerProfilesService struct {
	v1.UnimplementedPlayerProfileServiceServer
	*updateService[parsers.PlayerProfileEntry, v1.PlayerProfileUpdatesResponse]
	logPath string
}

func NewPlayerProfilesService(logPath string) *PlayerProfilesService {
	return &PlayerProfilesService{
		updateService: newUpdateService(
			logPath, parsers.ParsePlayerProfileEntry, playerProfileEntryToProto,
		),
		logPath: logPath,
	}
}

func (s *PlayerProfilesService) PlayerProfileUpdates(req *v1.PlayerProfileUpdatesRequest, stream v1.PlayerProfileService_PlayerProfileUpdatesServer) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updateStreams = append(s.updateStreams, stream)
	err := s.startTailing()
	return err
}

func playerProfileEntryToProto(entry *parsers.PlayerProfileEntry) *v1.PlayerProfileUpdatesResponse {
	return &v1.PlayerProfileUpdatesResponse{
		Entry: &v1.PlayerProfileEntry{
			Timestamp:  entry.Timestamp.Unix(),
			KeyHash:    entry.KeyHash,
			TrustLevel: uint32(entry.TrustLevel),
			Username:   entry.Username,
		},
	}
}
