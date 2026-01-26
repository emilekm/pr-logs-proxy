package players

import (
	"log/slog"
)

func (s *PlayerQueryService) streamAdminLogs() {
	ch := s.adminLogService.Updates()

	for {
		entry, ok := <-ch
		if !ok {
			slog.Error("Admin log stream closed")
			return
		}

		s.processAdminLogEntry(entry)
	}
}

func (s *PlayerQueryService) streamJoinLogs() {
	ch := s.joinLogService.Updates()

	for {
		entry, ok := <-ch
		if !ok {
			slog.Error("Join log stream closed")
			return
		}

		s.processJoinLogEntry(entry)
	}
}

func (s *PlayerQueryService) streamProfileUpdates() {
	ch := s.playerProfilesService.Updates()

	for {
		entry, ok := <-ch
		if !ok {
			slog.Error("Player profile stream closed")
			return
		}

		s.processPlayerProfileEntry(entry)
	}
}
