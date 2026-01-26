package players

import (
	"github.com/emilekm/go-prbf2/logs"
)

// processJoinLogEntry processes a join log entry and updates the database
func (s *PlayerQueryService) processJoinLogEntry(entry *logs.JoinEntry) {
	if entry.KeyHash == "" {
		return
	}

	s.db.AddJoinEntry(entry)
}

// processPlayerProfileEntry processes a player profile entry
func (s *PlayerQueryService) processPlayerProfileEntry(entry *logs.PlayerProfileEntry) {
	if entry.KeyHash == "" {
		return
	}
	s.db.AddProfileEntry(entry)
}

// processAdminLogEntry processes an admin log entry
func (s *PlayerQueryService) processAdminLogEntry(entry *logs.AdminEntry) {
	if entry.Action == "" {
		return
	}
	s.db.AddAdminEntry(entry)
}
