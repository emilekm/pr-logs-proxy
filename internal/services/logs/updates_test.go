package logs

import (
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

func TestDualCounterTracking(t *testing.T) {
	// Create a temporary log file with mixed valid and invalid entries
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test_admin.log")

	// Create test data: 5 lines total, 3 parseable
	testData := `[2024_01_01_12_00_00] !kick performed by 'Admin' on 'Player1': test
this is an invalid line that won't parse
[2024_01_01_12_00_01] !warn performed by 'Admin' on 'Player2': test2
another invalid line
[2024_01_01_12_00_02] !ban performed by 'Admin' on 'Player3': test3
`

	if err := os.WriteFile(logFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create the update service
	svc, err := newUpdateService(
		logFile,
		func(line string) (*logs.AdminEntry, error) {
			return logs.ParseAdminEntry(line, logs.DefaultAdminEntryDateFormat)
		},
		func(entry *logs.AdminEntry, lineNum uint64) *v1.AdminLogUpdatesResponse {
			return &v1.AdminLogUpdatesResponse{
				Entry: &v1.AdminLogEntry{
					Timestamp: entry.Timestamp.Unix(),
					Issuer:    entry.Issuer,
					Action:    entry.Action,
					Target:    entry.Target,
					Details:   entry.Details,
				},
				LineNumber: lineNum,
			}
		},
	)
	if err != nil {
		t.Fatalf("Failed to create update service: %v", err)
	}

	// Verify counters
	if svc.totalLinesRead != 5 {
		t.Errorf("Expected totalLinesRead = 5, got %d", svc.totalLinesRead)
	}

	if svc.parsedEntriesCount != 3 {
		t.Errorf("Expected parsedEntriesCount = 3, got %d", svc.parsedEntriesCount)
	}

	if len(svc.entries) != 3 {
		t.Errorf("Expected 3 entries in memory, got %d", len(svc.entries))
	}

	t.Logf("✓ Dual counter tracking works correctly: %d total lines, %d parsed entries",
		svc.totalLinesRead, svc.parsedEntriesCount)
}

func TestGetAllEntries(t *testing.T) {
	// Create a temporary log file
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test_admin.log")

	testData := `[2024_01_01_12_00_00] !kick performed by 'Admin' on 'Player1': test1
[2024_01_01_12_00_01] !warn performed by 'Admin' on 'Player2': test2
`

	if err := os.WriteFile(logFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	svc, err := newUpdateService(
		logFile,
		func(line string) (*logs.AdminEntry, error) {
			return logs.ParseAdminEntry(line, logs.DefaultAdminEntryDateFormat)
		},
		func(entry *logs.AdminEntry, lineNum uint64) *v1.AdminLogUpdatesResponse {
			return &v1.AdminLogUpdatesResponse{
				Entry: &v1.AdminLogEntry{
					Timestamp: entry.Timestamp.Unix(),
					Issuer:    entry.Issuer,
					Action:    entry.Action,
					Target:    entry.Target,
					Details:   entry.Details,
				},
				LineNumber: lineNum,
			}
		},
	)
	if err != nil {
		t.Fatalf("Failed to create update service: %v", err)
	}

	// Get all entries
	entries := svc.GetAllEntries()

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	// Verify it's a copy (modifying shouldn't affect internal storage)
	if len(entries) > 0 {
		entries[0] = nil
		if svc.entries[0] == nil {
			t.Error("GetAllEntries should return a copy, not the original slice")
		}
	}

	t.Logf("✓ GetAllEntries returns correct number of entries: %d", len(entries))
}
