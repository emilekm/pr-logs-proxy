package services

import (
	"bufio"
	"context"
	"encoding/binary"
	"os"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

var _ v1.JoinLogServiceServer = (*JoinLogService)(nil)

type JoinLogService struct {
	v1.UnimplementedJoinLogServiceServer
	*updateService[logs.JoinEntry, v1.JoinLogUpdatesResponse]
	logPath string
}

func NewJoinLogService(logPath string) *JoinLogService {
	return &JoinLogService{
		updateService: newUpdateService(
			logPath, logs.ParseJoinEntry,
			func(entry *logs.JoinEntry) *v1.JoinLogUpdatesResponse {
				return &v1.JoinLogUpdatesResponse{
					Entry: joinEntryToProto(entry),
				}
			},
		),
		logPath: logPath,
	}
}

func (s *JoinLogService) JoinLogsUpdates(req *v1.JoinLogUpdatesRequest, stream v1.JoinLogService_JoinLogUpdatesServer) error {
	return s.startTailing(stream)
}

func (s *JoinLogService) JoinLogs(ctx context.Context, req *v1.JoinLogsRequest) (*v1.JoinLogsResponse, error) {
	file, err := os.Open(s.logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]*v1.JoinLogEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, err := logs.ParseJoinEntry(line)
		if err != nil {
			continue
		}

		entries = append(entries, joinEntryToProto(entry))
	}

	return &v1.JoinLogsResponse{
		Entries: entries,
	}, nil
}

func joinEntryToProto(entry *logs.JoinEntry) *v1.JoinLogEntry {
	ipv4 := uint32(0)
	if ip := entry.IP.To4(); ip != nil {
		ipv4 = binary.BigEndian.Uint32(ip)
	}

	status := v1.AccountStatus_ACCOUNT_STATUS_UNSPECIFIED
	switch entry.Status {
	case logs.StatusLegacy:
		status = v1.AccountStatus_ACCOUNT_STATUS_LEGACY
	case logs.StatusWhitelisted:
		status = v1.AccountStatus_ACCOUNT_STATUS_WHITELISTED
	case logs.StatusVacBanned:
		status = v1.AccountStatus_ACCOUNT_STATUS_VAC_BANNED
	}

	return &v1.JoinLogEntry{
		Timestamp:  entry.Timestamp.Unix(),
		KeyHash:    entry.KeyHash,
		TrustLevel: uint32(entry.TrustLevel),
		Name:       entry.Name,
		CreatedAt:  entry.CreatedAt.Unix(),
		Ipv4:       ipv4,
		Status:     status,
	}
}
