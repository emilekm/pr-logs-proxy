package logs

import (
	"context"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

var _ v1.AdminLogServiceServer = (*AdminLogService)(nil)

type AdminLogService struct {
	v1.UnimplementedAdminLogServiceServer
	*updateService[logs.AdminEntry, v1.AdminLogUpdatesResponse]
	logPath string
}

func NewAdminLogService(logPath string) (*AdminLogService, error) {
	updateSvc, err := newUpdateService(
		logPath,
		func(line string) (*logs.AdminEntry, error) {
			return logs.ParseAdminEntry(line, logs.DefaultAdminEntryDateFormat)
		},
		func(entry *logs.AdminEntry, lineNum uint64) *v1.AdminLogUpdatesResponse {
			return &v1.AdminLogUpdatesResponse{
				Entry:      adminEntryToProto(entry),
				LineNumber: lineNum,
			}
		},
	)
	if err != nil {
		return nil, err
	}

	return &AdminLogService{
		updateService: updateSvc,
		logPath:       logPath,
	}, nil
}

func (s *AdminLogService) AdminLogUpdates(req *v1.AdminLogUpdatesRequest, stream v1.AdminLogService_AdminLogUpdatesServer) error {
	// Extract line number from request (defaults to 0 if not provided)
	fromLine := req.GetLineNumber()
	return s.startTailing(stream, fromLine)
}

func (s *AdminLogService) AdminsLogs(ctx context.Context, req *v1.AdminsLogsRequest) (*v1.AdminsLogsResponse, error) {
	// Get all entries from in-memory storage
	allEntries := s.GetAllEntries()

	// Convert to protobuf format
	entries := make([]*v1.AdminLogEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		entries = append(entries, adminEntryToProto(entry))
	}

	return &v1.AdminsLogsResponse{
		Entries: entries,
	}, nil
}

func adminEntryToProto(entry *logs.AdminEntry) *v1.AdminLogEntry {
	return &v1.AdminLogEntry{
		Timestamp: entry.Timestamp.Unix(),
		Issuer:    entry.Issuer,
		Action:    entry.Action,
		Target:    entry.Target,
		Details:   entry.Details,
	}
}
