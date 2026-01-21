package players

import (
	"strings"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
)

// processJoinLogEntry processes a join log entry and updates the database
func (s *PlayerQueryService) processJoinLogEntry(entry *v1.JoinLogEntry) {
	if entry == nil || entry.KeyHash == "" {
		return
	}

	player := s.db.AddOrUpdatePlayer(entry.KeyHash)

	// Add name
	if entry.Name != "" {
		player.AddName(entry.Name)
		s.db.IndexName(entry.Name, entry.KeyHash)
	}

	// Update IP
	if entry.Ipv4 != 0 {
		player.UpdateIP(entry.Ipv4, entry.Timestamp)
		s.db.IndexIP(entry.Ipv4, entry.KeyHash)
	}

	// Update account info
	player.mu.Lock()
	if entry.CreatedAt > 0 {
		player.AccountCreated = entry.CreatedAt
	}
	player.AccountStatus = int32(entry.Status)
	player.TrustLevel = entry.TrustLevel

	// Add join event
	player.JoinEvents = append(player.JoinEvents, &JoinEvent{
		Timestamp:  entry.Timestamp,
		Name:       entry.Name,
		IP:         entry.Ipv4,
		TrustLevel: entry.TrustLevel,
		Status:     int32(entry.Status),
	})
	player.mu.Unlock()
}

// processPlayerProfileEntry processes a player profile entry
func (s *PlayerQueryService) processPlayerProfileEntry(entry *v1.PlayerProfileEntry) {
	if entry == nil || entry.KeyHash == "" {
		return
	}

	player := s.db.AddOrUpdatePlayer(entry.KeyHash)

	// Update current name
	if entry.Username != "" {
		player.AddName(entry.Username)
		s.db.IndexName(entry.Username, entry.KeyHash)

		player.mu.Lock()
		player.CurrentName = entry.Username
		player.TrustLevel = entry.TrustLevel
		player.mu.Unlock()
	}

	// Add profile update
	player.mu.Lock()
	player.ProfileUpdates = append(player.ProfileUpdates, &ProfileUpdate{
		Timestamp:  entry.Timestamp,
		Username:   entry.Username,
		TrustLevel: entry.TrustLevel,
	})
	player.mu.Unlock()
}

// processAdminLogEntry processes an admin log entry
func (s *PlayerQueryService) processAdminLogEntry(entry *v1.AdminLogEntry) {
	if entry == nil {
		return
	}

	action := &ActionRecord{
		Timestamp: entry.Timestamp,
		Action:    entry.Action,
		Issuer:    entry.Issuer,
		Target:    entry.Target,
		Details:   entry.Details,
	}

	// Try to extract key_hash from target
	targetKeyHash := ExtractKeyHashFromTarget(entry.Target)
	if targetKeyHash != "" {
		// Action was performed ON this player
		player := s.db.AddOrUpdatePlayer(targetKeyHash)
		player.mu.Lock()
		player.Actions = append(player.Actions, action)
		player.mu.Unlock()
	}

	// Check if issuer is a player (not an admin command)
	// Issuers that contain "PRISM user" or "Server" are admin actions
	if !strings.Contains(entry.Issuer, "PRISM") && !strings.Contains(entry.Issuer, "Server") {
		// Issuer might be a player performing an action
		// Try to find player by name match
		players := s.db.SearchByName(entry.Issuer)
		if len(players) == 1 {
			// Exact match found
			player := players[0]
			player.mu.Lock()
			player.Actions = append(player.Actions, action)
			player.mu.Unlock()
		}
	}
}
