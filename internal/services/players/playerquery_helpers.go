package players

import (
	"strings"
)

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
