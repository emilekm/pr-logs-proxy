package players

import (
	"context"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchPlayers implements the SearchPlayers RPC
func (s *PlayerQueryService) SearchPlayers(ctx context.Context, req *v1.PlayerSearchRequest) (*v1.PlayerSearchResponse, error) {
	// Validate that name is provided
	if req.PlayerName == "" {
		return nil, status.Error(codes.InvalidArgument, "player_name must be provided")
	}

	// Search by name
	results := s.db.SearchByName(req.PlayerName)

	// Limit to 10 results
	if len(results) > 10 {
		results = results[:10]
	}

	// Convert to proto
	protoResults := make([]*v1.PlayerSearchResult, 0, len(results))
	for _, player := range results {
		protoResults = append(protoResults, &v1.PlayerSearchResult{
			KeyHash:        player.KeyHash,
			CurrentName:    player.CurrentName,
			AlternateNames: player.GetAllNames(),
		})
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

	_, exists := s.db.GetPlayer(req.KeyHash)
	if !exists {
		return nil, status.Error(codes.NotFound, "player not found")
	}

	// Get all connected accounts
	connections := s.db.GetConnectedAccounts(req.KeyHash)

	// Build hash info for the requested account first (no connection info)
	hashInfos := make([]*v1.PlayerHashInfo, 0, len(connections)+1)

	// Add the requested player first
	if requestedPlayer, exists := s.db.GetPlayer(req.KeyHash); exists {
		hashInfo := s.buildPlayerHashInfo(req.KeyHash, requestedPlayer, nil)
		hashInfos = append(hashInfos, hashInfo)
	}

	// Add all connected accounts with their connection info
	for _, connInfo := range connections {
		hashPlayer, hashExists := s.db.GetPlayer(connInfo.KeyHash)
		if !hashExists {
			continue
		}

		// Convert internal ConnectionInfo to proto ConnectionInfo
		protoConnInfo := &v1.ConnectionInfo{
			ViaKeyHash:   connInfo.ViaKeyHash,
			ViaIp:        connInfo.ViaIP,
			DistanceHops: int32(connInfo.DistanceHops),
		}

		hashInfo := s.buildPlayerHashInfo(connInfo.KeyHash, hashPlayer, protoConnInfo)
		hashInfos = append(hashInfos, hashInfo)
	}

	return &v1.PlayerInfoResponse{
		Hashes: hashInfos,
	}, nil
}

// buildPlayerHashInfo creates a PlayerHashInfo proto message from a PlayerRecord
func (s *PlayerQueryService) buildPlayerHashInfo(keyHash string, player *PlayerRecord, connInfo *v1.ConnectionInfo) *v1.PlayerHashInfo {
	player.mu.RLock()
	defer player.mu.RUnlock()

	// Collect all names for this hash
	names := make([]string, 0, len(player.AllNames))
	for name := range player.AllNames {
		names = append(names, name)
	}

	// Collect all IPs for this hash
	ips := make([]*v1.PlayerIP, 0, len(player.IPs))
	for _, ipInfo := range player.IPs {
		ips = append(ips, &v1.PlayerIP{
			Ip:        ipInfo.IP,
			FirstSeen: ipInfo.FirstSeen,
			LastSeen:  ipInfo.LastSeen,
		})
	}

	// Determine ban status for this hash
	banStatus := s.buildBanStatus(player.Actions)

	return &v1.PlayerHashInfo{
		KeyHash:    keyHash,
		Names:      names,
		Ips:        ips,
		BanStatus:  banStatus,
		Connection: connInfo,
	}
}

// buildBanStatus creates a BanStatus object from action records
func (s *PlayerQueryService) buildBanStatus(actions []*ActionRecord) *v1.BanStatus {
	var lastBan *ActionRecord
	var lastBanTimestamp int64
	var lastUnban *ActionRecord
	var lastUnbanTimestamp int64

	// Find the most recent ban and unban actions
	for _, action := range actions {
		if IsBanAction(action.Action) {
			if action.Timestamp > lastBanTimestamp {
				lastBanTimestamp = action.Timestamp
				lastBan = action
			}
		}
		if IsUnbanAction(action.Action) {
			if action.Timestamp > lastUnbanTimestamp {
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
			Timestamp: lastBan.Timestamp,
			Action:    lastBan.Action,
			Issuer:    lastBan.Issuer,
			Target:    lastBan.Target,
			Details:   lastBan.Details,
		},
	}

	// Include unban if it exists (regardless of timestamp order)
	if lastUnban != nil {
		banStatus.Unban = &v1.AdminLogEntry{
			Timestamp: lastUnban.Timestamp,
			Action:    lastUnban.Action,
			Issuer:    lastUnban.Issuer,
			Target:    lastUnban.Target,
			Details:   lastUnban.Details,
		}
	}

	return banStatus
}
