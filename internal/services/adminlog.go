package services

import (
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/Alliance-Community/pr-logs-proxy/pkg/parsers"
)

var _ v1.AdminLogServiceServer = (*AdminLogService)(nil)

type AdminLogService struct {
	v1.UnimplementedAdminLogServiceServer
	*updateService[parsers.AdminEntry, v1.AdminLogUpdatesResponse]
	logPath string
}

func NewAdminLogService(logPath string) *AdminLogService {
	return &AdminLogService{
		updateService: newUpdateService(
			logPath,
			func(line string) (*parsers.AdminEntry, error) {
				return parsers.ParseAdminEntry(line, parsers.DefaultDateFormat)
			},
			adminEntryToProto,
		),
		logPath: logPath,
	}
}

func (s *AdminLogService) AdminLogUpdates(req *v1.AdminLogUpdatesRequest, stream v1.AdminLogService_AdminLogUpdatesServer) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.updateStreams = append(s.updateStreams, stream)
	err := s.startTailing()
	return err
}

func adminEntryToProto(entry *parsers.AdminEntry) *v1.AdminLogUpdatesResponse {
	return &v1.AdminLogUpdatesResponse{
		Entry: &v1.AdminLogEntry{
			Timestamp: entry.Timestamp.Unix(),
			Issuer:    entry.Issuer,
			Action:    entry.Action,
			Target:    entry.Target,
			Details:   entry.Details,
		},
	}
}
