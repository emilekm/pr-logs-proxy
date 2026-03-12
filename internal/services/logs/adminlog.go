package logs

import (
	"context"
	"strings"

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
	return s.startTailing(stream, req.LineNumber)
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
		Action:    adminActionFromLog(entry.Action),
		ActionStr: entry.Action,
		Target:    entry.Target,
		Details:   entry.Details,
	}
}

func adminActionFromLog(cmd string) v1.AdminAction {
	upper := strings.ToUpper(strings.TrimPrefix(cmd, "!"))
	switch upper {
	case "AA":
		return v1.AdminAction_ADMIN_ACTION_AA
	case "ALLOWEDID":
		return v1.AdminAction_ADMIN_ACTION_ALLOWEDID
	case "BAN":
		return v1.AdminAction_ADMIN_ACTION_BAN
	case "BANID":
		return v1.AdminAction_ADMIN_ACTION_BANID
	case "FLY":
		return v1.AdminAction_ADMIN_ACTION_FLY
	case "HASH":
		return v1.AdminAction_ADMIN_ACTION_HASH
	case "INIT":
		return v1.AdminAction_ADMIN_ACTION_INIT
	case "K", "KICK":
		return v1.AdminAction_ADMIN_ACTION_KICK
	case "KILL":
		return v1.AdminAction_ADMIN_ACTION_KILL
	case "MAPVOTE":
		return v1.AdminAction_ADMIN_ACTION_MAPVOTE
	case "MAPVOTERESULT":
		return v1.AdminAction_ADMIN_ACTION_MAPVOTERESULT
	case "MESSAGE":
		return v1.AdminAction_ADMIN_ACTION_MESSAGE
	case "RELOAD":
		return v1.AdminAction_ADMIN_ACTION_RELOAD
	case "REPORT":
		return v1.AdminAction_ADMIN_ACTION_REPORT
	case "REPORTP":
		return v1.AdminAction_ADMIN_ACTION_REPORTP
	case "RESIGN":
		return v1.AdminAction_ADMIN_ACTION_RESIGN
	case "RESIGNALL":
		return v1.AdminAction_ADMIN_ACTION_RESIGNALL
	case "RUNNEXT":
		return v1.AdminAction_ADMIN_ACTION_RUNNEXT
	case "SAY":
		return v1.AdminAction_ADMIN_ACTION_SAY
	case "SAYTEAM":
		return v1.AdminAction_ADMIN_ACTION_SAYTEAM
	case "SCRAMBLE":
		return v1.AdminAction_ADMIN_ACTION_SCRAMBLE
	case "SETNEXT":
		return v1.AdminAction_ADMIN_ACTION_SETNEXT
	case "STOPSERVER":
		return v1.AdminAction_ADMIN_ACTION_STOPSERVER
	case "SWAPTEAMS":
		return v1.AdminAction_ADMIN_ACTION_SWAPTEAMS
	case "SWITCH":
		return v1.AdminAction_ADMIN_ACTION_SWITCH
	case "TEMPBAN":
		return v1.AdminAction_ADMIN_ACTION_TEMPBAN
	case "TICKETS":
		return v1.AdminAction_ADMIN_ACTION_TICKETS
	case "TIMEBAN":
		return v1.AdminAction_ADMIN_ACTION_TIMEBAN
	case "TIMEBANID":
		return v1.AdminAction_ADMIN_ACTION_TIMEBANID
	case "UNBAN":
		return v1.AdminAction_ADMIN_ACTION_UNBAN
	case "UNBANID":
		return v1.AdminAction_ADMIN_ACTION_UNBANID
	case "UNBANNAME":
		return v1.AdminAction_ADMIN_ACTION_UNBANNAME
	case "UNGRIEF":
		return v1.AdminAction_ADMIN_ACTION_UNGRIEF
	case "WARN":
		return v1.AdminAction_ADMIN_ACTION_WARN
	default:
		return v1.AdminAction_ADMIN_ACTION_UNSPECIFIED
	}
}
