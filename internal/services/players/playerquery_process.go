package players

import (
	"encoding/binary"
	"strings"

	"github.com/emilekm/go-prbf2/logs"
)

// processJoinLogEntry processes a join log entry and updates the database
func (s *PlayerQueryService) processJoinLogEntry(entry *logs.JoinEntry) {
	if entry.KeyHash == "" {
		return
	}

	player := s.db.AddOrUpdatePlayer(entry.KeyHash)

	// Add name
	if entry.Name != "" {
		player.AddName(entry.Name)
		s.db.IndexName(entry.Name, entry.KeyHash)
	}

	// Update IP
	ip := binary.BigEndian.Uint32(entry.IP)
	player.UpdateIP(ip, entry.Timestamp)
	s.db.IndexIP(ip, entry.KeyHash)

	// Update account info
	player.mu.Lock()
	player.AccountCreated = entry.CreatedAt
	player.AccountStatus = entry.Status
	player.TrustLevel = entry.TrustLevel
	player.mu.Unlock()
}

// processPlayerProfileEntry processes a player profile entry
func (s *PlayerQueryService) processPlayerProfileEntry(entry *logs.PlayerProfileEntry) {
	player := s.db.AddOrUpdatePlayer(entry.KeyHash)

	player.AddName(entry.Username)
	s.db.IndexName(entry.Username, entry.KeyHash)

	player.mu.Lock()
	player.CurrentName = entry.Username
	player.mu.Unlock()
}

// processAdminLogEntry processes an admin log entry
func (s *PlayerQueryService) processAdminLogEntry(entry *logs.AdminEntry) {
	if entry.Action == "REPORT" {
		// Ignore report actions
		return
	}

	// Try to extract key_hash from target
	targetKeyHash := extractKeyHashFromTarget(entry.Target)
	if targetKeyHash != "" {
		// Action was performed ON this player
		player := s.db.AddOrUpdatePlayer(targetKeyHash)
		player.mu.Lock()
		player.Actions = append(player.Actions, entry)
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
			player.Actions = append(player.Actions, entry)
			player.mu.Unlock()
		}
	}
}

func extractKeyHashFromTarget(target string) string {
	for part := range strings.SplitSeq(target, " ") {
		if len(part) == 32 { // Key hash length
			return part
		}
	}

	return ""
}
