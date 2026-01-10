package parsers

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

type PlayerProfileEntry struct {
	Timestamp time.Time
	KeyHash   string
	Level     int
	Username  string
}

func ParsePlayerProfileEntry(line string) (*PlayerProfileEntry, error) {
	parts := strings.Split(line, "\t")
	if len(parts) < 4 {
		return nil, errors.New("invalid format: expected at least 4 fields")
	}

	timestamp, err := time.Parse("[2006-01-02 15:04:05]", parts[0])
	if err != nil {
		return nil, err
	}

	level, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, err
	}

	return &PlayerProfileEntry{
		Timestamp: timestamp,
		KeyHash:   parts[1],
		Level:     level,
		Username:  parts[3],
	}, nil
}
