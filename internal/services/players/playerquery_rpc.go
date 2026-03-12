package players

import (
	"context"
	"sort"
	"time"

	internal_logs "github.com/Alliance-Community/pr-logs-proxy/internal/services/logs"
	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchPlayers implements the SearchPlayers RPC
func (s *PlayerQueryService) SearchPlayers(ctx context.Context, req *v1.SearchPlayersRequest) (*v1.SearchPlayersResponse, error) {
	// Validate that name is provided
	if req.PlayerName == "" {
		return nil, status.Error(codes.InvalidArgument, "player_name must be provided")
	}

	// Search by name - returns keyHashes
	keyHashes := s.db.SearchKeyHashesByName(req.PlayerName)

	// Limit to 10 results
	if len(keyHashes) > 10 {
		keyHashes = keyHashes[:10]
	}

	// Convert to proto
	protoResults := make([]*v1.PlayerSearchResult, len(keyHashes))
	for i, keyHash := range keyHashes {
		names := s.db.GetNames(keyHash)
		currentName := ""
		alternateNames := []string{}

		if len(names) > 0 {
			currentName = names[0]
			if len(names) > 1 {
				alternateNames = names[1:]
			}
		}

		protoResults[i] = &v1.PlayerSearchResult{
			KeyHash:    keyHash,
			Name:       currentName,
			OtherNames: alternateNames,
		}
	}

	return &v1.SearchPlayersResponse{
		Results: protoResults,
	}, nil
}

// GetPlayerInfo implements the GetPlayerInfo RPC
func (s *PlayerQueryService) PlayerInfo(ctx context.Context, req *v1.PlayerInfoRequest) (*v1.PlayerInfoResponse, error) {
	if req.KeyHash == "" {
		return nil, status.Error(codes.InvalidArgument, "key_hash is required")
	}

	if _, ok := s.db.hashToUsernames.Load(req.KeyHash); !ok {
		return nil, status.Error(codes.NotFound, "player not found")
	}

	// Get all names for this hash
	names := s.db.GetNames(req.KeyHash)

	// Get all IPs for this hash
	ips := s.db.GetIPs(req.KeyHash)

	// Determine ban status for this hash
	banStatus := s.buildBanStatus(req.KeyHash)

	return &v1.PlayerInfoResponse{
		KeyHash:   req.KeyHash,
		Names:     names,
		Ips:       ips,
		BanStatus: banStatus,
	}, nil
}

// buildBanStatus creates a BanStatus object from action records
func (s *PlayerQueryService) buildBanStatus(keyHash string) *v1.BanStatus {
	var lastBan *logs.AdminEntry
	var lastBanTimestamp time.Time
	var lastUnban *logs.AdminEntry
	var lastUnbanTimestamp time.Time

	actions := make([]*logs.AdminEntry, 0)

	if partial, exists := s.db.actionsTargetedByHash.Load(keyHash); exists {
		partial.AppendTo(actions)
		partial.Range(func(entry *logs.AdminEntry) bool {
			if IsBanAction(entry.Action) || IsUnbanAction(entry.Action) {
				actions = append(actions, entry)
			}
			return true
		})
	}

	if names, exists := s.db.hashToUsernames.Load(keyHash); exists {
		names.RLock()
		for name := range names.M {
			if banAction, ok := s.db.banByUsername.Load(name); ok {
				actions = append(actions, banAction)
			}
			if unbanAction, ok := s.db.unbanByUsername.Load(name); ok {
				actions = append(actions, unbanAction)
			}
		}
		names.RUnlock()
	}

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Timestamp.Before(actions[j].Timestamp)
	})

	// Find the most recent ban and unban actions
	for _, action := range actions {
		if IsBanAction(action.Action) {
			if action.Timestamp.After(lastBanTimestamp) {
				lastBanTimestamp = action.Timestamp
				lastBan = action
			}
		}
		if IsUnbanAction(action.Action) {
			if action.Timestamp.After(lastUnbanTimestamp) {
				lastUnbanTimestamp = action.Timestamp
				lastUnban = action
			}
		}
	}

	// Only return ban status if there's at least a ban action
	if lastBan == nil {
		return nil
	}

	banStatus := &v1.BanStatus{
		Ban: &v1.AdminLogEntry{
			Timestamp: lastBan.Timestamp.Unix(),
			Action:    internal_logs.AdminActionFromLog(lastBan.Action),
			ActionStr: lastBan.Action,
			Issuer:    lastBan.Issuer,
			Target:    lastBan.Target,
			Details:   lastBan.Details,
		},
	}

	// Include unban if it exists (regardless of timestamp order)
	if lastUnban != nil && lastUnban.Timestamp.After(lastBan.Timestamp) {
		banStatus.Unban = &v1.AdminLogEntry{
			Timestamp: lastUnban.Timestamp.Unix(),
			Action:    internal_logs.AdminActionFromLog(lastUnban.Action),
			ActionStr: lastUnban.Action,
			Issuer:    lastUnban.Issuer,
			Target:    lastUnban.Target,
			Details:   lastUnban.Details,
		}
	}

	return banStatus
}

func (s *PlayerQueryService) ConnectedPlayers(ctx context.Context, req *v1.ConnectedPlayersRequest) (*v1.ConnectedPlayersResponse, error) {
	if req.KeyHash == "" {
		return nil, status.Error(codes.InvalidArgument, "key_hash is required")
	}

	connections := s.db.GetConnectedAccounts(req.KeyHash)

	protoConnections := make([]*v1.ConnectionInfo, len(connections))
	for i, conn := range connections {
		protoConnections[i] = &v1.ConnectionInfo{
			KeyHash:      conn.KeyHash,
			ViaKeyHash:   conn.ViaKeyHash,
			ViaIp:        conn.ViaIP,
			DistanceHops: int32(conn.DistanceHops),
		}
	}

	return &v1.ConnectedPlayersResponse{
		Connections: protoConnections,
	}, nil
}

func (s *PlayerQueryService) PlayerLogs(ctx context.Context, req *v1.PlayerLogsRequest) (*v1.PlayerLogsResponse, error) {
	if req.KeyHash == "" {
		return nil, status.Error(codes.InvalidArgument, "key_hash is required")
	}

	actions := s.db.GetActions(req.KeyHash)

	protoActions := make([]*v1.AdminLogEntry, len(actions))
	for i, action := range actions {
		protoActions[i] = internal_logs.AdminEntryToProto(action)
	}

	return &v1.PlayerLogsResponse{
		AdminLogs: protoActions,
	}, nil
}
