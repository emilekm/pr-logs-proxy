package players

import (
	"encoding/binary"
	"net"
	"sort"
	"strings"
	"sync"

	v1 "github.com/Alliance-Community/pr-logs-proxy/logsproxy/v1"
	"github.com/emilekm/go-prbf2/logs"
)

// PlayerDatabase holds all player records
type PlayerDatabase struct {
	profiles              map[string][]*logs.PlayerProfileEntry // key_hash -> profile
	joins                 map[string][]*logs.JoinEntry          // key_hash -> join
	actionsIssued         map[string][]*logs.AdminEntry         // username -> actions
	actionsTargeted       map[string][]*logs.AdminEntry         // username -> actions
	actionsTargetedByHash map[string][]*logs.AdminEntry         // key_hash -> actions

	hashToUsernames map[string]map[string]struct{} // key_hash -> set of connected usernames
	usernamesToHash map[string]string              // username -> key_hash
	ipToHashes      map[uint32]map[string]struct{} // ip -> set of key_hashes
	hashToIPs       map[string]map[uint32]struct{} // key_hash -> set of ips

	mu sync.RWMutex
}

func newPlayerDatabase() *PlayerDatabase {
	return &PlayerDatabase{
		profiles:              make(map[string][]*logs.PlayerProfileEntry),
		joins:                 make(map[string][]*logs.JoinEntry),
		actionsIssued:         make(map[string][]*logs.AdminEntry),
		actionsTargeted:       make(map[string][]*logs.AdminEntry),
		actionsTargetedByHash: make(map[string][]*logs.AdminEntry),
		hashToUsernames:       make(map[string]map[string]struct{}),
		usernamesToHash:       make(map[string]string),
		ipToHashes:            make(map[uint32]map[string]struct{}),
		hashToIPs:             make(map[string]map[uint32]struct{}),
	}
}

func (db *PlayerDatabase) AddProfileEntry(entry *logs.PlayerProfileEntry) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.profiles[entry.KeyHash]; !exists {
		db.profiles[entry.KeyHash] = make([]*logs.PlayerProfileEntry, 0)
	}
	db.profiles[entry.KeyHash] = append(db.profiles[entry.KeyHash], entry)

	db.usernamesToHash[entry.Username] = entry.KeyHash
	if _, exists := db.hashToUsernames[entry.KeyHash]; !exists {
		db.hashToUsernames[entry.KeyHash] = make(map[string]struct{})
	}
	db.hashToUsernames[entry.KeyHash][entry.Username] = struct{}{}
}

func (db *PlayerDatabase) AddJoinEntry(entry *logs.JoinEntry) {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.joins[entry.KeyHash] = append(db.joins[entry.KeyHash], entry)

	ip := ipFromNetIP(entry.IP)
	if _, exists := db.ipToHashes[ip]; !exists {
		db.ipToHashes[ip] = make(map[string]struct{})
	}
	db.ipToHashes[ip][entry.KeyHash] = struct{}{}

	if _, exists := db.hashToIPs[entry.KeyHash]; !exists {
		db.hashToIPs[entry.KeyHash] = make(map[uint32]struct{})
	}
	db.hashToIPs[entry.KeyHash][ip] = struct{}{}
}

func (db *PlayerDatabase) AddAdminEntry(entry *logs.AdminEntry) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Index by issuer username
	issuer := cleanName(entry.Issuer)
	db.actionsIssued[issuer] = append(db.actionsIssued[issuer], entry)

	keyHash := extractKeyHashFromTarget(entry.Target)
	if keyHash != "" {
		// Index by target key_hash
		db.actionsTargetedByHash[keyHash] = append(db.actionsTargetedByHash[keyHash], entry)
	} else if entry.Target != "" {
		// Index by target username
		target := cleanName(entry.Target)
		db.actionsTargeted[target] = append(db.actionsTargeted[target], entry)
	}
}

// GetKeyHashByName looks up a keyHash by exact name match
func (db *PlayerDatabase) GetKeyHashByName(name string) (string, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	cleanedName := cleanName(name)
	keyHash, exists := db.usernamesToHash[cleanedName]
	return keyHash, exists
}

// SearchKeyHashesByName returns keyHashes matching substring search
func (db *PlayerDatabase) SearchKeyHashesByName(name string) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	results := make(map[string]struct{})
	cleanedName := cleanName(name)

	// Search through name index
	for indexedName, keyHash := range db.usernamesToHash {
		if strings.Contains(indexedName, cleanedName) {
			results[keyHash] = struct{}{}
		}
	}

	// Convert map to slice
	list := make([]string, 0, len(results))
	for keyHash := range results {
		list = append(list, keyHash)
	}

	return list
}

// GetNames aggregates unique names from profile entries
func (db *PlayerDatabase) GetNames(keyHash string) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	nameMap := make(map[string]struct{})

	for _, entry := range db.joins[keyHash] {
		nameMap[entry.Name] = struct{}{}
	}

	// Convert to slice
	names := make([]string, 0, len(nameMap))
	for name := range nameMap {
		names = append(names, name)
	}

	return names
}

// GetIPs aggregates IPs from join entries with FirstSeen/LastSeen
func (db *PlayerDatabase) GetIPs(keyHash string) []*v1.PlayerIP {
	db.mu.RLock()
	defer db.mu.RUnlock()

	ipMap := make(map[uint32]*v1.PlayerIP)

	// Aggregate from joins
	if joins, exists := db.joins[keyHash]; exists {
		for _, entry := range joins {
			ip := ipFromNetIP(entry.IP)
			if existing, found := ipMap[ip]; found {
				// Update FirstSeen and LastSeen
				if entry.Timestamp.Unix() < existing.FirstSeen {
					existing.FirstSeen = entry.Timestamp.Unix()
				}
				if entry.Timestamp.Unix() > existing.LastSeen {
					existing.LastSeen = entry.Timestamp.Unix()
				}
			} else {
				ipMap[ip] = &v1.PlayerIP{
					Ip:        ip,
					FirstSeen: entry.Timestamp.Unix(),
					LastSeen:  entry.Timestamp.Unix(),
				}
			}
		}
	}

	// Convert to slice
	ips := make([]*v1.PlayerIP, 0, len(ipMap))
	for _, ipInfo := range ipMap {
		ips = append(ips, ipInfo)
	}

	return ips
}

// GetActions returns admin actions targeted at this keyHash
func (db *PlayerDatabase) GetActions(keyHash string) []*logs.AdminEntry {
	db.mu.RLock()

	actions := make([]*logs.AdminEntry, 0)

	if actions, exists := db.actionsTargetedByHash[keyHash]; exists {
		actions = append(actions, actions...)
	}

	if names, exists := db.hashToUsernames[keyHash]; exists {
		for name := range names {
			if userActions, found := db.actionsTargeted[name]; found {
				actions = append(actions, userActions...)
			}
			if issuerActions, found := db.actionsIssued[name]; found {
				actions = append(actions, issuerActions...)
			}
		}
	}

	db.mu.RUnlock()

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Timestamp.Before(actions[j].Timestamp)
	})

	return actions
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

	// Check if keyHash exists
	if _, ok := db.hashToUsernames[keyHash]; !ok {
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

		// For each IP this player has used
		for ip := range db.hashToIPs[current.keyHash] {
			// Find all other players who used this IP
			if keyHashes, exists := db.ipToHashes[ip]; exists {
				for otherKeyHash := range keyHashes {
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

func cleanName(name string) string {
	split := strings.Split(name, " ")
	username := name
	if len(split) == 2 {
		username = split[1]
	}

	// PRISM user <username>
	if len(split) == 3 {
		username = split[2]
	}

	return strings.TrimSpace(username)
}

func ipFromNetIP(ipAddr net.IP) uint32 {
	ipAddr = ipAddr.To4()
	return binary.BigEndian.Uint32(ipAddr)
}

func extractKeyHashFromTarget(target string) string {
	for part := range strings.SplitSeq(target, " ") {
		if len(part) == 32 { // Key hash length
			return part
		}
	}

	return ""
}
