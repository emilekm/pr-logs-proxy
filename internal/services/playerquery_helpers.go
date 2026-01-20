package services

import (
	"strings"
)

// ExtractKeyHashFromTarget attempts to extract key_hash from admin log target field
// Expected format: "id <key_hash>" or just "<key_hash>"
func ExtractKeyHashFromTarget(target string) string {
	// Check if target contains "id " prefix
	if strings.HasPrefix(target, "id ") {
		parts := strings.Fields(target)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	// Check if target looks like a key_hash (alphanumeric, reasonable length)
	if len(target) > 6 && len(target) < 50 && isAlphanumeric(target) {
		return target
	}

	return ""
}

// isAlphanumeric checks if string contains only alphanumeric characters
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// IsBanAction checks if an action is a ban-related action
func IsBanAction(action string) bool {
	upper := strings.ToUpper(action)
	return upper == "!BAN" || upper == "!BANID" || upper == "!TIMEBAN"
}

// IsUnbanAction checks if an action is an unban action
func IsUnbanAction(action string) bool {
	return strings.ToUpper(action) == "!UNBAN"
}

// IsTimeBan checks if an action is specifically a TIMEBAN
func IsTimeBan(action string) bool {
	return strings.ToUpper(action) == "!TIMEBAN"
}

// DetermineBanStatus determines if a player is currently banned
func DetermineBanStatus(actions []*ActionRecord) (lastBan *ActionRecord, isBanned bool) {
	var lastBanAction *ActionRecord
	var lastBanTimestamp int64

	// Find the most recent ban action
	for _, action := range actions {
		if IsBanAction(action.Action) {
			if action.Timestamp > lastBanTimestamp {
				lastBanTimestamp = action.Timestamp
				lastBanAction = action
			}
		}
	}

	if lastBanAction == nil {
		return nil, false
	}

	// Check if there's an UNBAN after the last ban
	for _, action := range actions {
		if IsUnbanAction(action.Action) && action.Timestamp > lastBanTimestamp {
			// Found an unban after the last ban
			return lastBanAction, false
		}
	}

	// Check if it's a TIMEBAN and if it has expired
	if IsTimeBan(lastBanAction.Action) {
		duration := ParseTimeBanDuration(lastBanAction.Details)
		if duration > 0 {
			expirationTime := lastBanAction.Timestamp + int64(duration.Seconds())
			currentTime := getCurrentTimestamp()
			if currentTime > expirationTime {
				// TIMEBAN has expired
				return lastBanAction, false
			}
		}
		// If duration parsing fails (returns 0), treat like permanent ban
	}

	return lastBanAction, true
}

// getCurrentTimestamp returns current Unix timestamp
func getCurrentTimestamp() int64 {
	return nowFunc().Unix()
}

// SortActionsByTimestamp sorts actions by timestamp (newest first)
func SortActionsByTimestamp(actions []*ActionRecord) {
	// Simple bubble sort (fine for reasonable action counts)
	n := len(actions)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if actions[j].Timestamp < actions[j+1].Timestamp {
				actions[j], actions[j+1] = actions[j+1], actions[j]
			}
		}
	}
}
