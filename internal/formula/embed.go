package formula

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/assetsync"
)

// Formulas live in internal/formula/formulas/ (source of truth).
// They are embedded into the binary and provisioned to .beads/formulas/ at install time.
// Provisioning, health check and update are implemented by internal/assetsync,
// which internal/directives uses for the default role directives as well.

//go:embed formulas/*.formula.toml
var formulasFS embed.FS

// Install hashes are recorded in .beads/formulas/.installed.json under the key
// "formulas" ({"formulas": {"<file>": "<sha256>"}}); see assetsync.

// FormulaStatus represents the status of a single formula during health check.
type FormulaStatus struct {
	Name          string
	Status        string // "ok", "outdated", "modified", "missing", "new", "untracked"
	EmbeddedHash  string // hash computed from embedded content
	InstalledHash string // hash we installed (from .installed.json)
	CurrentHash   string // hash of current file on disk
}

// HealthReport contains the results of checking formula health.
type HealthReport struct {
	Formulas []FormulaStatus
	// Counts
	OK        int
	Outdated  int // embedded changed, user hasn't modified
	Modified  int // user modified the file (tracked in .installed.json)
	Missing   int // file was deleted
	New       int // new formula not yet installed
	Untracked int // file exists but not in .installed.json (safe to update)
	Error     int // file could not be read (e.g. permission denied)
}

// ResolveFormulaContent resolves formula content using the three-tier precedence
// defined in docs/design/formula-resolution.md: rig > town > embedded.
//
// Tier 1 (rig): townRoot/rigName/.beads/formulas/<name>.formula.toml
// Tier 2 (town): townRoot/.beads/formulas/<name>.formula.toml
// Tier 3 (embedded): compiled into the binary
//
// Either townRoot or rigName may be empty; those tiers are skipped.
func ResolveFormulaContent(name, townRoot, rigName string) ([]byte, error) {
	filename := name
	if !hasFormulaSuffix(filename) {
		filename = filename + ".formula.toml"
	}

	// Tier 1: rig-level (most specific)
	if townRoot != "" && rigName != "" {
		path := filepath.Join(townRoot, rigName, ".beads", "formulas", filename)
		if content, err := os.ReadFile(path); err == nil {
			return content, nil
		}
	}

	// Tier 2: town-level
	if townRoot != "" {
		path := filepath.Join(townRoot, ".beads", "formulas", filename)
		if content, err := os.ReadFile(path); err == nil {
			return content, nil
		}
	}

	// Tier 3: embedded (system fallback)
	return GetEmbeddedFormulaContent(name)
}

// GetEmbeddedFormulaContent returns the raw content of an embedded formula by name.
// The name can be with or without the .formula.toml suffix.
// Returns the content bytes, or an error if the formula is not found.
func GetEmbeddedFormulaContent(name string) ([]byte, error) {
	// Normalize: ensure the filename has the correct suffix
	filename := name
	if !hasFormulaSuffix(filename) {
		filename = filename + ".formula.toml"
	}
	content, err := formulasFS.ReadFile("formulas/" + filename)
	if err != nil {
		return nil, fmt.Errorf("embedded formula %q not found: %w", name, err)
	}
	return content, nil
}

// hasFormulaSuffix checks if a name already has a formula file suffix.
func hasFormulaSuffix(name string) bool {
	return len(name) > len(".formula.toml") &&
		name[len(name)-len(".formula.toml"):] == ".formula.toml"
}

// formulaSetFor returns the assetsync set that installs the embedded formulas into formulasDir.
func formulaSetFor(formulasDir string) assetsync.Set {
	return assetsync.Set{
		FS:        formulasFS,
		SrcDir:    "formulas",
		DestDir:   formulasDir,
		RecordKey: "formulas",
	}
}

// formulaSet returns the set for <beadsPath>/.beads/formulas/.
func formulaSet(beadsPath string) assetsync.Set {
	return formulaSetFor(filepath.Join(beadsPath, ".beads", "formulas"))
}

// ProvisionFormulas creates the .beads/formulas/ directory with embedded formulas.
// This is called during gt install for fresh installations.
// If a formula already exists, it is skipped (no overwrite).
// Returns the number of formulas provisioned.
func ProvisionFormulas(beadsPath string) (int, error) {
	return assetsync.Provision(formulaSet(beadsPath))
}

// CheckFormulaHealth checks the status of all formulas.
// Returns a report of which formulas are ok, outdated, modified, or missing.
func CheckFormulaHealth(beadsPath string) (*HealthReport, error) {
	r, err := assetsync.Check(formulaSet(beadsPath))
	if err != nil {
		return nil, err
	}
	report := &HealthReport{
		OK:        r.OK,
		Outdated:  r.Outdated,
		Modified:  r.Modified,
		Missing:   r.Missing,
		New:       r.New,
		Untracked: r.Untracked,
		Error:     r.Error,
	}
	for _, f := range r.Files {
		report.Formulas = append(report.Formulas, FormulaStatus(f))
	}
	return report, nil
}

// UpdateFormulas updates formulas that are safe to update (outdated, missing, or untracked).
// Skips user-modified formulas (tracked files that user changed).
// Returns counts of updated, skipped (modified), and reinstalled (missing).
func UpdateFormulas(beadsPath string) (updated, skipped, reinstalled int, err error) {
	return assetsync.Update(formulaSet(beadsPath))
}
