package services

import (
	"bufio"
	"context"
	"os"

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
			func(entry *parsers.AdminEntry) *v1.AdminLogUpdatesResponse {
				return &v1.AdminLogUpdatesResponse{
					Entry: adminEntryToProto(entry),
				}
			},
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

func (s *AdminLogService) AdminsLogs(ctx context.Context, req *v1.AdminsLogsRequest) (*v1.AdminsLogsResponse, error) {
	file, err := os.Open(s.logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]*v1.AdminLogEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, err := parsers.ParseAdminEntry(line, parsers.DefaultDateFormat)
		if err != nil {
			continue
		}

		entries = append(entries, adminEntryToProto(entry))
	}

	return &v1.AdminsLogsResponse{
		Entries: entries,
	}, nil
}

func adminEntryToProto(entry *parsers.AdminEntry) *v1.AdminLogEntry {
	return &v1.AdminLogEntry{
		Timestamp: entry.Timestamp.Unix(),
		Issuer:    entry.Issuer,
		Action:    entry.Action,
		Target:    entry.Target,
		Details:   entry.Details,
	}
}
