package players

import (
	"slices"
	"sync"
	"time"
)

// PlayerRecord represents aggregated data for a single player
type PlayerRecord struct {
	KeyHash        string
	CurrentName    string
	AllNames       map[string]struct{} // Deduplicated names
	IPs            map[uint32]*IPInfo
	AccountStatus  int32
	AccountCreated int64
	TrustLevel     uint32

	// Action history
	Actions        []*ActionRecord
	JoinEvents     []*JoinEvent
	ProfileUpdates []*ProfileUpdate

	mu sync.RWMutex
}

// IPInfo tracks information about an IP address
type IPInfo struct {
	IP        uint32
	FirstSeen int64
	LastSeen  int64
}

// ActionRecord represents an admin action
type ActionRecord struct {
	Timestamp int64
	Action    string
	Issuer    string
	Target    string
	Details   string
}

// JoinEvent represents a player join event
type JoinEvent struct {
	Timestamp  int64
	Name       string
	IP         uint32
	TrustLevel uint32
	Status     int32
}

// ProfileUpdate represents a profile change
type ProfileUpdate struct {
	Timestamp  int64
	Username   string
	TrustLevel uint32
}

// PlayerDatabase holds all player records
type PlayerDatabase struct {
	players   map[string]*PlayerRecord // key_hash -> record
	nameIndex map[string][]string      // lowercase name -> []key_hash
	ipIndex   map[uint32][]string      // ip -> []key_hash
	mu        sync.RWMutex
}

func newPlayerDatabase() *PlayerDatabase {
	return &PlayerDatabase{
		players:   make(map[string]*PlayerRecord),
		nameIndex: make(map[string][]string),
		ipIndex:   make(map[uint32][]string),
	}
}

// AddOrUpdatePlayer adds or updates a player record
func (db *PlayerDatabase) AddOrUpdatePlayer(keyHash string) *PlayerRecord {
	db.mu.Lock()
	defer db.mu.Unlock()

	if player, exists := db.players[keyHash]; exists {
		return player
	}

	player := &PlayerRecord{
		KeyHash:  keyHash,
		AllNames: make(map[string]struct{}),
		IPs:      make(map[uint32]*IPInfo),
		Actions:  make([]*ActionRecord, 0),
	}
	db.players[keyHash] = player
	return player
}

// GetPlayer retrieves a player by key_hash
func (db *PlayerDatabase) GetPlayer(keyHash string) (*PlayerRecord, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	player, exists := db.players[keyHash]
	return player, exists
}

// SearchByName searches for players by name (case-insensitive substring match)
func (db *PlayerDatabase) SearchByName(name string) []*PlayerRecord {
	db.mu.RLock()
	defer db.mu.RUnlock()

	results := make(map[string]*PlayerRecord)
	lowerName := toLower(name)

	// Search through name index
	for indexedName, keyHashes := range db.nameIndex {
		if contains(indexedName, lowerName) {
			for _, keyHash := range keyHashes {
				if player, exists := db.players[keyHash]; exists {
					results[keyHash] = player
				}
			}
		}
	}

	// Convert map to slice
	list := make([]*PlayerRecord, 0, len(results))
	for _, player := range results {
		list = append(list, player)
	}

	return list
}

// ConnectionInfo describes how a keyHash was discovered through the connection graph
type ConnectionInfo struct {
	KeyHash      string // The connected account
	ViaKeyHash   string // The keyHash that led to this discovery
	ViaIP        uint32 // The IP address that connected them
	DistanceHops int    // Number of hops from the original keyHash
}

// GetConnectedAccounts finds all key_hashes transitively connected through shared IPs.
// It performs a breadth-first search to find all accounts that share IPs, either directly
// or through a chain of IP-sharing relationships.
// Returns a slice of ConnectionInfo containing details about each connection.
func (db *PlayerDatabase) GetConnectedAccounts(keyHash string) []*ConnectionInfo {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.players[keyHash]
	if !exists {
		return nil
	}

	// Track connection information
	connections := make(map[string]*ConnectionInfo)
	visited := make(map[string]bool)
	visited[keyHash] = true

	// Queue for BFS - each item includes the keyHash and current distance
	type queueItem struct {
		keyHash  string
		distance int
	}
	queue := []queueItem{{keyHash: keyHash, distance: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		currentPlayer, exists := db.players[current.keyHash]
		if !exists {
			continue
		}

		// For each IP this player has used
		for ip := range currentPlayer.IPs {
			// Find all other players who used this IP
			if keyHashes, exists := db.ipIndex[ip]; exists {
				for _, otherKeyHash := range keyHashes {
					// Skip if it's the original keyHash or already visited
					if otherKeyHash == keyHash || visited[otherKeyHash] {
						continue
					}

					// Mark as visited and record connection info
					visited[otherKeyHash] = true
					connections[otherKeyHash] = &ConnectionInfo{
						KeyHash:      otherKeyHash,
						ViaKeyHash:   current.keyHash,
						ViaIP:        ip,
						DistanceHops: current.distance + 1,
					}

					// Add to queue to explore its IPs
					queue = append(queue, queueItem{
						keyHash:  otherKeyHash,
						distance: current.distance + 1,
					})
				}
			}
		}
	}

	// Convert map to slice
	result := make([]*ConnectionInfo, 0, len(connections))
	for _, info := range connections {
		result = append(result, info)
	}

	return result
}

// IndexName adds a name to the search index
func (db *PlayerDatabase) IndexName(name, keyHash string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	lowerName := toLower(name)
	keyHashes := db.nameIndex[lowerName]

	// Check if already indexed
	if slices.Contains(keyHashes, keyHash) {
		return
	}

	db.nameIndex[lowerName] = append(keyHashes, keyHash)
}

// IndexIP adds an IP to the search index
func (db *PlayerDatabase) IndexIP(ip uint32, keyHash string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	keyHashes := db.ipIndex[ip]

	// Check if already indexed
	if slices.Contains(keyHashes, keyHash) {
		return
	}

	db.ipIndex[ip] = append(keyHashes, keyHash)
}

// Helper to update or add IP info
func (p *PlayerRecord) UpdateIP(ip uint32, timestamp int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if info, exists := p.IPs[ip]; exists {
		if timestamp < info.FirstSeen {
			info.FirstSeen = timestamp
		}
		if timestamp > info.LastSeen {
			info.LastSeen = timestamp
		}
	} else {
		p.IPs[ip] = &IPInfo{
			IP:        ip,
			FirstSeen: timestamp,
			LastSeen:  timestamp,
		}
	}
}

// Helper to add a name
func (p *PlayerRecord) AddName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.AllNames[name] = struct{}{}
	if p.CurrentName == "" || len(name) > 0 {
		p.CurrentName = name
	}
}

// Helper to get all names as slice
func (p *PlayerRecord) GetAllNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.AllNames))
	for name := range p.AllNames {
		if name != p.CurrentName {
			names = append(names, name)
		}
	}
	return names
}

// Simple string helpers
func toLower(s string) string {
	// Simple ASCII lowercase conversion
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ParseTimeBanDuration extracts duration from TIMEBAN details
// TODO: Implement duration parsing logic
func ParseTimeBanDuration(details string) time.Duration {
	// TODO: Parse ban duration from details field
	// Expected formats: "7 days", "24 hours", "1 week", etc.
	// This is left as a placeholder for the user to implement
	return 0
}
