package players

import (
	"context"
	"sort"
	"time"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchPlayers implements the SearchPlayers RPC
func (s *PlayerQueryService) SearchPlayers(ctx context.Context, req *v1.PlayerSearchRequest) (*v1.PlayerSearchResponse, error) {
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
			KeyHash:        keyHash,
			CurrentName:    currentName,
			AlternateNames: alternateNames,
		}
	}

	return &v1.PlayerSearchResponse{
		Results: protoResults,
	}, nil
}

// GetPlayerInfo implements the GetPlayerInfo RPC
func (s *PlayerQueryService) GetPlayerInfo(ctx context.Context, req *v1.PlayerInfoRequest) (*v1.PlayerInfoResponse, error) {
	if req.KeyHash == "" {
		return nil, status.Error(codes.InvalidArgument, "key_hash is required")
	}

	if _, ok := s.db.hashToUsernames[req.KeyHash]; !ok {
		return nil, status.Error(codes.NotFound, "player not found")
	}

	// Get all connected accounts
	connections := s.db.GetConnectedAccounts(req.KeyHash)

	// Build hash info for the requested account first (no connection info)
	hashInfos := make([]*v1.PlayerHashInfo, 0, len(connections)+1)

	// Add the requested player first
	hashInfo := s.buildPlayerHashInfo(req.KeyHash, nil)
	hashInfos = append(hashInfos, hashInfo)

	// Add all connected accounts with their connection info
	for _, connInfo := range connections {
		// Convert internal ConnectionInfo to proto ConnectionInfo
		protoConnInfo := &v1.ConnectionInfo{
			ViaKeyHash:   connInfo.ViaKeyHash,
			ViaIp:        connInfo.ViaIP,
			DistanceHops: int32(connInfo.DistanceHops),
		}

		hashInfo := s.buildPlayerHashInfo(connInfo.KeyHash, protoConnInfo)
		hashInfos = append(hashInfos, hashInfo)
	}

	return &v1.PlayerInfoResponse{
		Hashes: hashInfos,
	}, nil
}

// buildPlayerHashInfo creates a PlayerHashInfo proto message from database queries
func (s *PlayerQueryService) buildPlayerHashInfo(keyHash string, connInfo *v1.ConnectionInfo) *v1.PlayerHashInfo {
	// Get all names for this hash
	names := s.db.GetNames(keyHash)

	// Get all IPs for this hash
	ips := s.db.GetIPs(keyHash)

	// Determine ban status for this hash
	banStatus := s.buildBanStatus(keyHash)

	return &v1.PlayerHashInfo{
		KeyHash:    keyHash,
		Names:      names,
		Ips:        ips,
		BanStatus:  banStatus,
		Connection: connInfo,
	}
}

// buildBanStatus creates a BanStatus object from action records
func (s *PlayerQueryService) buildBanStatus(keyHash string) *v1.BanStatus {
	var lastBan *logs.AdminEntry
	var lastBanTimestamp time.Time
	var lastUnban *logs.AdminEntry
	var lastUnbanTimestamp time.Time

	actions := make([]*logs.AdminEntry, 0)

	if actions, exists := s.db.actionsTargetedByHash[keyHash]; exists {
		actions = append(actions, actions...)
	}

	if names, exists := s.db.hashToUsernames[keyHash]; exists {
		for name := range names {
			if userActions, found := s.db.actionsTargeted[name]; found {
				actions = append(actions, userActions...)
			}
		}
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
			Action:    lastBan.Action,
			Issuer:    lastBan.Issuer,
			Target:    lastBan.Target,
			Details:   lastBan.Details,
		},
	}

	// Include unban if it exists (regardless of timestamp order)
	if lastUnban != nil && lastUnban.Timestamp.After(lastBan.Timestamp) {
		banStatus.Unban = &v1.AdminLogEntry{
			Timestamp: lastUnban.Timestamp.Unix(),
			Action:    lastUnban.Action,
			Issuer:    lastUnban.Issuer,
			Target:    lastUnban.Target,
			Details:   lastUnban.Details,
		}
	}

	return banStatus
}
