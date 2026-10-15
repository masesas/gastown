package witness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/workspace"
)

// respawnMu serializes in-process access to the respawn state file.
// Cross-process serialization is handled by lock.FlockAcquire on a
// sibling .flock file (see RecordBeadRespawn, ShouldBlockRespawn, etc.).
var respawnMu sync.Mutex

// beadRespawnRecord tracks how many times a single bead has been reset for re-dispatch.
type beadRespawnRecord struct {
	BeadID      string    `json:"bead_id"`
	Count       int       `json:"count"`
	LastRespawn time.Time `json:"last_respawn"`
	// InfraFailures counts consecutive dispatches that failed before the agent
	// started working (spawn, hook, session start). Separate from Count so
	// infrastructure trouble never consumes the bead's work-failure budget
	// (gt-pd1c.53). Reset on the next successful session start.
	InfraFailures int `json:"infra_failures,omitempty"`
}

// MaxBeadInfraFailures is the number of consecutive infra failures after which
// dispatch of a bead is blocked until success or `gt sling respawn-reset`.
const MaxBeadInfraFailures = 5

// beadRespawnState holds respawn counts for all tracked beads.
type beadRespawnState struct {
	Beads       map[string]*beadRespawnRecord `json:"beads"`
	LastUpdated time.Time                     `json:"last_updated"`
}

func beadRespawnStateFile(townRoot string) string {
	return filepath.Join(townRoot, "witness", "bead-respawn-counts.json")
}

func loadBeadRespawnState(townRoot string) *beadRespawnState {
	data, err := os.ReadFile(beadRespawnStateFile(townRoot)) //nolint:gosec // G304: path from trusted townRoot
	if err != nil {
		return &beadRespawnState{Beads: make(map[string]*beadRespawnRecord)}
	}
	var state beadRespawnState
	if err := json.Unmarshal(data, &state); err != nil {
		return &beadRespawnState{Beads: make(map[string]*beadRespawnRecord)}
	}
	if state.Beads == nil {
		state.Beads = make(map[string]*beadRespawnRecord)
	}
	return &state
}

func saveBeadRespawnState(townRoot string, state *beadRespawnState) error {
	stateFile := beadRespawnStateFile(townRoot)
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return fmt.Errorf("creating witness dir: %w", err)
	}
	state.LastUpdated = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling respawn state: %w", err)
	}
	return os.WriteFile(stateFile, data, 0600)
}

// ShouldBlockRespawn returns true if the bead has already been respawned
// MaxBeadRespawns times (from operational config). When true, the caller
// should escalate to mayor instead of sending RECOVERED_BEAD to deacon
// for re-dispatch. This is the primary circuit breaker for spawn storms
// (clown show #22).
func ShouldBlockRespawn(workDir, beadID string) bool {
	respawnMu.Lock()
	defer respawnMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}
	maxRespawns := config.LoadOperationalConfig(townRoot).GetWitnessConfig().MaxBeadRespawnsV()

	// Cross-process flock to serialize with other witness instances.
	unlock, flockErr := lock.FlockAcquire(beadRespawnStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadRespawnState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		return false
	}
	return rec.Count >= maxRespawns
}

// BeadRespawnCount returns the recorded respawn count for beadID (0 if none).
func BeadRespawnCount(workDir, beadID string) int {
	return withBeadRecord(workDir, false, beadID, func(rec *beadRespawnRecord) int { return rec.Count })
}

// ShouldBlockInfraFailures reports whether beadID has hit MaxBeadInfraFailures
// consecutive spawn/start failures, and the current count.
func ShouldBlockInfraFailures(workDir, beadID string) (bool, int) {
	n := withBeadRecord(workDir, false, beadID, func(rec *beadRespawnRecord) int { return rec.InfraFailures })
	return n >= MaxBeadInfraFailures, n
}

// RecordBeadInfraFailure increments the infra-failure counter (separate from the
// respawn counter) and returns the new value.
func RecordBeadInfraFailure(workDir, beadID string) int {
	return withBeadRecord(workDir, true, beadID, func(rec *beadRespawnRecord) int {
		rec.InfraFailures++
		return rec.InfraFailures
	})
}

// ResetBeadInfraFailures clears the infra-failure counter after a dispatch succeeded.
func ResetBeadInfraFailures(workDir, beadID string) {
	withBeadRecord(workDir, false, beadID, func(rec *beadRespawnRecord) int {
		rec.InfraFailures = 0
		return 0
	})
}

// withBeadRecord runs fn on the (optionally created) record for beadID under
// the in-process mutex and cross-process flock, persisting changes. Missing
// record with create=false yields 0 without writing.
func withBeadRecord(workDir string, create bool, beadID string, fn func(*beadRespawnRecord) int) int {
	respawnMu.Lock()
	defer respawnMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}
	unlock, flockErr := lock.FlockAcquire(beadRespawnStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadRespawnState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		if !create {
			return 0
		}
		rec = &beadRespawnRecord{BeadID: beadID}
		state.Beads[beadID] = rec
	}
	before := *rec
	out := fn(rec)
	if *rec != before || create {
		_ = saveBeadRespawnState(townRoot, state) // Non-fatal: tracking must not block dispatch
	}
	return out
}

// RecordBeadRespawn increments the respawn count for beadID and returns the new count.
// workDir is the rig path; townRoot is resolved internally via workspace.Find.
// On state file errors the count is still incremented in memory and returned, so the
// caller can log/warn without blocking the respawn itself.
//
// Serialized via respawnMu (in-process) and flock (cross-process) to prevent
// concurrent patrol cycles from racing on the load-modify-save cycle.
func RecordBeadRespawn(workDir, beadID string) int {
	respawnMu.Lock()
	defer respawnMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	// Cross-process flock to serialize with other witness instances.
	unlock, flockErr := lock.FlockAcquire(beadRespawnStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadRespawnState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		rec = &beadRespawnRecord{BeadID: beadID}
		state.Beads[beadID] = rec
	}
	rec.Count++
	rec.LastRespawn = time.Now().UTC()
	_ = saveBeadRespawnState(townRoot, state) // Non-fatal: tracking failure must not block respawn
	return rec.Count
}

// ResetBeadRespawnCount resets the respawn counter for beadID to zero.
// Used by `gt sling respawn-reset` to allow re-dispatch after investigation.
// Deleting the record clears both the respawn and the infra-failure counters.
func ResetBeadRespawnCount(workDir, beadID string) error {
	respawnMu.Lock()
	defer respawnMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	// Cross-process flock to serialize with other witness instances.
	unlock, flockErr := lock.FlockAcquire(beadRespawnStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadRespawnState(townRoot)
	delete(state.Beads, beadID)
	return saveBeadRespawnState(townRoot, state)
}
