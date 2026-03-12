package players

import (
	"encoding/binary"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/emilekm/go-prbf2/logs"
	v1 "github.com/emilekm/pr-logs-proxy/logsproxy/v1"
	syncmap "github.com/zolstein/sync-map"
)

type syncSlice[T any] struct {
	mu    sync.Mutex
	slice []T
}

func (s *syncSlice[T]) Append(item T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slice = append(s.slice, item)
}

func (s *syncSlice[T]) Range(f func(item T) bool) {
	// Iterate over the slice until length at the time of starting the iteration to avoid holding the lock for the entire duration
	s.mu.Lock()
	length := len(s.slice)
	s.mu.Unlock()

	for i := range length {
		if !f(s.slice[i]) {
			break
		}
	}

	s.mu.Lock()
	// Check if new items were added during iteration and iterate over them as well
	for i := length; i < len(s.slice); i++ {
		if !f(s.slice[i]) {
			break
		}
	}
	s.mu.Unlock()
}

func (s *syncSlice[T]) AppendTo(items []T) []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(items, s.slice...)
}

type simpleSyncMap[K comparable, V any] struct {
	sync.RWMutex
	M map[K]V
}

func newSimpleSyncMap[K comparable, V any]() *simpleSyncMap[K, V] {
	return &simpleSyncMap[K, V]{M: make(map[K]V)}
}

// PlayerDatabase holds all player records
type PlayerDatabase struct {
	// profiles              *syncmap.Map[string, []*logs.PlayerProfileEntry] // key_hash -> profile (concurrent)
	joins                 *syncmap.Map[string, *syncSlice[*logs.JoinEntry]]  // key_hash -> join (concurrent)
	actions               *syncmap.Map[string, *syncSlice[*logs.AdminEntry]] // username -> actions (concurrent)
	actionsIssued         *syncmap.Map[string, *syncSlice[*logs.AdminEntry]] // username -> actions (concurrent)
	actionsTargeted       *syncmap.Map[string, *syncSlice[*logs.AdminEntry]] // username -> actions (concurrent)
	actionsTargetedByHash *syncmap.Map[string, *syncSlice[*logs.AdminEntry]] // key_hash -> actions (concurrent)

	banByUsername   *syncmap.Map[string, *logs.AdminEntry] // username -> latest ban action (concurrent)
	unbanByUsername *syncmap.Map[string, *logs.AdminEntry] // username -> latest unban action (concurrent)

	usernamesToHash *syncmap.Map[string, string]                           // username -> key_hash (concurrent)
	hashToUsernames *syncmap.Map[string, *simpleSyncMap[string, struct{}]] // key_hash -> set of connected usernames (concurrent)
	ipToHashes      *syncmap.Map[uint32, *simpleSyncMap[string, struct{}]] // ip -> set of key_hashes (concurrent)
	hashToIPs       *syncmap.Map[string, *simpleSyncMap[uint32, struct{}]] // key_hash -> set of ips (concurrent)
}

func newPlayerDatabase() *PlayerDatabase {
	return &PlayerDatabase{
		// profiles: new(syncmap.Map[string, []*logs.PlayerProfileEntry]),
		joins: new(syncmap.Map[string, *syncSlice[*logs.JoinEntry]]),

		actionsIssued:         new(syncmap.Map[string, *syncSlice[*logs.AdminEntry]]),
		actionsTargeted:       new(syncmap.Map[string, *syncSlice[*logs.AdminEntry]]),
		actionsTargetedByHash: new(syncmap.Map[string, *syncSlice[*logs.AdminEntry]]),

		usernamesToHash: new(syncmap.Map[string, string]),
		hashToUsernames: new(syncmap.Map[string, *simpleSyncMap[string, struct{}]]),
		ipToHashes:      new(syncmap.Map[uint32, *simpleSyncMap[string, struct{}]]),
		hashToIPs:       new(syncmap.Map[string, *simpleSyncMap[uint32, struct{}]]),
	}
}

func (db *PlayerDatabase) AddProfileEntry(entry *logs.PlayerProfileEntry) {
	// entries, _ := db.profiles.LoadOrStore(entry.KeyHash, []*logs.PlayerProfileEntry{})
	// entries = append(entries, entry)

	db.usernamesToHash.Store(entry.Username, entry.KeyHash)

	names, _ := db.hashToUsernames.LoadOrStore(entry.KeyHash, newSimpleSyncMap[string, struct{}]())
	names.M[entry.Username] = struct{}{}
}

func (db *PlayerDatabase) AddJoinEntry(entry *logs.JoinEntry) {
	joins, _ := db.joins.LoadOrStore(entry.KeyHash, &syncSlice[*logs.JoinEntry]{})
	joins.Append(entry)

	ip := ipFromNetIP(entry.IP)

	hashes, _ := db.ipToHashes.LoadOrStore(ip, newSimpleSyncMap[string, struct{}]())
	hashes.Lock()
	hashes.M[entry.KeyHash] = struct{}{}
	hashes.Unlock()

	ips, _ := db.hashToIPs.LoadOrStore(entry.KeyHash, newSimpleSyncMap[uint32, struct{}]())
	ips.Lock()
	ips.M[ip] = struct{}{}
	ips.Unlock()
}

func (db *PlayerDatabase) AddAdminEntry(entry *logs.AdminEntry) {
	// Index by issuer username
	issuer := cleanName(entry.Issuer)
	issued, _ := db.actionsIssued.LoadOrStore(issuer, &syncSlice[*logs.AdminEntry]{})
	issued.Append(entry)

	actions, _ := db.actions.LoadOrStore(issuer, &syncSlice[*logs.AdminEntry]{})
	actions.Append(entry)

	keyHash := extractKeyHashFromTarget(entry.Target)
	if keyHash != "" {
		// Index by target key_hash
		targetedByHash, _ := db.actionsTargetedByHash.LoadOrStore(keyHash, &syncSlice[*logs.AdminEntry]{})
		targetedByHash.Append(entry)
	} else if entry.Target != "" {
		// Index by target username
		target := cleanName(entry.Target)
		targeted, _ := db.actionsTargeted.LoadOrStore(target, &syncSlice[*logs.AdminEntry]{})
		targeted.Append(entry)

		actions, _ := db.actions.LoadOrStore(target, &syncSlice[*logs.AdminEntry]{})
		actions.Append(entry)

		if IsBanAction(entry.Action) {
			db.banByUsername.Store(target, entry)
		} else if IsUnbanAction(entry.Action) {
			db.unbanByUsername.Store(target, entry)
		}
	}
}

// GetKeyHashByName looks up a keyHash by exact name match
func (db *PlayerDatabase) GetKeyHashByName(name string) (string, bool) {
	return db.usernamesToHash.Load(cleanName(name))
}

// SearchKeyHashesByName returns keyHashes matching substring search
func (db *PlayerDatabase) SearchKeyHashesByName(name string) []string {
	results := make(map[string]struct{})
	cleanedName := cleanName(name)

	db.usernamesToHash.Range(func(indexedName string, keyHash string) bool {
		if strings.Contains(indexedName, cleanedName) {
			results[keyHash] = struct{}{}
		}
		if strings.Contains(strings.ToLower(indexedName), strings.ToLower(cleanedName)) {
			results[keyHash] = struct{}{}
		}
		return true
	})

	// Convert map to slice
	list := make([]string, 0, len(results))
	for keyHash := range results {
		list = append(list, keyHash)
	}

	return list
}

// GetNames aggregates unique names from profile entries
func (db *PlayerDatabase) GetNames(keyHash string) []string {
	nameMap := make(map[string]struct{})

	joins, _ := db.joins.Load(keyHash)

	joins.Range(func(entry *logs.JoinEntry) bool {
		nameMap[entry.Name] = struct{}{}
		return true
	})

	// Convert to slice
	names := make([]string, 0, len(nameMap))
	for name := range nameMap {
		names = append(names, name)
	}

	return names
}

// GetIPs aggregates IPs from join entries with FirstSeen/LastSeen
func (db *PlayerDatabase) GetIPs(keyHash string) []*v1.PlayerIP {
	ipMap := make(map[uint32]*v1.PlayerIP)

	// Aggregate from joins
	if joins, exists := db.joins.Load(keyHash); exists {
		joins.Range(func(entry *logs.JoinEntry) bool {
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
			return true
		})
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
	actions := make([]*logs.AdminEntry, 0)

	if targetedByHash, exists := db.actionsTargetedByHash.Load(keyHash); exists {
		targetedByHash.AppendTo(actions)
	}

	if names, exists := db.hashToUsernames.Load(keyHash); exists {
		names.RLock()
		for name := range names.M {
			if actionsAll, ok := db.actions.Load(name); ok {
				actionsAll.AppendTo(actions)
			}
		}
		names.RUnlock()
	}

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
	// Check if keyHash exists
	if _, ok := db.hashToUsernames.Load(keyHash); !ok {
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
		ips, exists := db.hashToIPs.Load(current.keyHash)
		if !exists {
			continue
		}
		ips.RLock()
		for ip := range ips.M {
			// Find all other players who used this IP
			hashes, exists := db.ipToHashes.Load(ip)
			if !exists {
				continue
			}
			hashes.RLock()
			for otherKeyHash := range hashes.M {
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
			hashes.RUnlock()
		}
		ips.RUnlock()
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
