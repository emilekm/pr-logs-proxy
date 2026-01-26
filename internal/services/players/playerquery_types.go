package players

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emilekm/go-prbf2/logs"
	"github.com/hashicorp/go-memdb"
)

var schema = &memdb.DBSchema{
	Tables: map[string]*memdb.TableSchema{
		"players": {
			Name: "players",
			Indexes: map[string]*memdb.IndexSchema{
				"key_hash": {
					Name:    "key_hash",
					Unique:  true,
					Indexer: &memdb.StringFieldIndex{Field: "KeyHash"},
				},
			},
		},
	},
}

// PlayerRecord represents aggregated data for a single player
type PlayerRecord struct {
	KeyHash        string
	CurrentName    string
	AllNames       map[string]struct{} // Deduplicated names
	IPs            map[uint32]*IPInfo
	AccountStatus  logs.AccountStatus
	AccountCreated time.Time
	TrustLevel     int

	// Action history
	Actions []*logs.AdminEntry

	mu sync.RWMutex
}

// IPInfo tracks information about an IP address
type IPInfo struct {
	IP        uint32
	FirstSeen time.Time
	LastSeen  time.Time
}

// PlayerDatabase holds all player records
type PlayerDatabase struct {
	players   map[string]*PlayerRecord // key_hash -> record
	nameIndex map[string]string        // cleaned_name -> key_hash
	ipIndex   map[uint32][]string      // ip -> []key_hash
	mu        sync.RWMutex
}

func newPlayerDatabase() *PlayerDatabase {
	return &PlayerDatabase{
		players:   make(map[string]*PlayerRecord),
		nameIndex: make(map[string]string),
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
		Actions:  make([]*logs.AdminEntry, 0),
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
	cleanedName := cleanName(name)

	if match, ok := db.nameIndex[cleanedName]; ok {
		if player, exists := db.players[match]; exists {
			results[match] = player
		}
	} else {
		// Search through name index
		for indexedName, keyHash := range db.nameIndex {
			if strings.Contains(indexedName, cleanedName) {
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

	cleanedName := cleanName(name)

	db.nameIndex[cleanedName] = keyHash
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
func (p *PlayerRecord) UpdateIP(ip uint32, timestamp time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if info, exists := p.IPs[ip]; exists {
		if timestamp.Before(info.FirstSeen) {
			info.FirstSeen = timestamp
		}
		if timestamp.After(info.LastSeen) {
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
	if p.CurrentName == "" {
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
	return slices.Clone(names)
}

func cleanName(name string) string {
	split := strings.Split(name, " ")
	username := name
	if len(split) == 2 {
		username = split[1]
	}

	if len(split) == 3 {
		username = split[2]
	}

	return strings.TrimSpace(username)
}
