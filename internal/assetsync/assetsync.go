// Package assetsync provisions files embedded in the gt binary into a town
// and keeps them current across upgrades without overwriting user edits.
//
// A Set maps one directory of an embedded filesystem to one destination
// directory. The hashes of the files gt wrote are kept in
// <DestDir>/.installed.json under Set.RecordKey. That record is what lets an
// upgrade tell "unmodified since install" (safe to refresh) from "edited by
// the user" (kept as is).
//
// Formulas (internal/formula) and default role directives
// (internal/directives) both use this package.
package assetsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// RecordFile is the name of the hash record inside Set.DestDir.
const RecordFile = ".installed.json"

// File status values reported by Check.
const (
	StatusOK        = "ok"        // file on disk equals the embedded file
	StatusOutdated  = "outdated"  // unmodified since install, embedded file changed
	StatusModified  = "modified"  // edited by the user; never overwritten
	StatusMissing   = "missing"   // installed earlier, deleted since
	StatusNew       = "new"       // never installed, not on disk
	StatusUntracked = "untracked" // on disk, differs, no record entry (see Set.PreserveUntracked)
	StatusError     = "error"     // could not be read
)

// Set describes one embedded source directory and where it is installed.
type Set struct {
	FS        fs.FS  // embedded source (typically an embed.FS)
	SrcDir    string // directory inside FS; only its direct, regular files are used
	DestDir   string // absolute destination directory
	RecordKey string // top-level JSON key of the record in DestDir/.installed.json

	// Match filters the files of SrcDir by name. Nil means every file.
	Match func(name string) bool

	// PreserveUntracked treats a file that exists on disk, differs from the
	// embedded one and has no record entry as user-owned: it is reported as
	// modified and never overwritten. Use it for assets earlier gt versions
	// did not install, where such a file can only have been written by the
	// user. When false, such files are reported untracked and Update
	// overwrites them (formulas from gt versions without a record).
	PreserveUntracked bool
}

// Status is the state of one embedded file at its destination.
type Status struct {
	Name          string
	Status        string // one of the Status* constants
	EmbeddedHash  string // hash of the embedded content
	InstalledHash string // hash recorded at install time ("" if not recorded)
	CurrentHash   string // hash of the file on disk ("" if absent or unreadable)
}

// Report summarises Check over every embedded file of a Set.
type Report struct {
	Files     []Status // sorted by Name
	OK        int
	Outdated  int
	Modified  int
	Missing   int
	New       int
	Untracked int
	Error     int
}

// Hash returns the hex SHA-256 of data, the form stored in the record.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Set) validate() error {
	switch {
	case s.FS == nil:
		return errors.New("assetsync: Set.FS is nil")
	case s.SrcDir == "":
		return errors.New("assetsync: Set.SrcDir is empty")
	case s.DestDir == "":
		return errors.New("assetsync: Set.DestDir is empty")
	case s.RecordKey == "":
		return errors.New("assetsync: Set.RecordKey is empty")
	}
	return nil
}

// embedded returns name -> content for the files of the Set, sorted by name.
func (s Set) embedded() ([]string, map[string][]byte, error) {
	entries, err := fs.ReadDir(s.FS, s.SrcDir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading embedded %s: %w", s.SrcDir, err)
	}
	var names []string
	contents := make(map[string][]byte)
	for _, e := range entries {
		if e.IsDir() || (s.Match != nil && !s.Match(e.Name())) {
			continue
		}
		data, err := fs.ReadFile(s.FS, path.Join(s.SrcDir, e.Name()))
		if err != nil {
			return nil, nil, fmt.Errorf("reading embedded %s: %w", e.Name(), err)
		}
		names = append(names, e.Name())
		contents[e.Name()] = data
	}
	sort.Strings(names)
	return names, contents, nil
}

// EmbeddedHashes returns name -> hash of every embedded file of the Set.
// It does not touch DestDir.
func EmbeddedHashes(s Set) (map[string]string, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	names, contents, err := s.embedded()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = Hash(contents[name])
	}
	return out, nil
}

// LoadRecord returns the recorded install hashes of the Set (name -> hash).
// A missing record yields an empty map.
func LoadRecord(s Set) (map[string]string, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s.loadRecord()
}

// SaveRecord replaces the recorded install hashes of the Set.
func SaveRecord(s Set, rec map[string]string) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.saveRecord(rec)
}

func (s Set) loadRecord() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(s.DestDir, RecordFile))
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading installed record: %w", err)
	}
	var raw map[string]map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing installed record: %w", err)
	}
	rec := raw[s.RecordKey]
	if rec == nil {
		rec = make(map[string]string)
	}
	return rec, nil
}

func (s Set) saveRecord(rec map[string]string) error {
	data, err := json.MarshalIndent(map[string]map[string]string{s.RecordKey: rec}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding installed record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.DestDir, RecordFile), data, 0644); err != nil {
		return fmt.Errorf("saving installed record: %w", err)
	}
	return nil
}

// prepare validates the Set, creates DestDir and loads the embedded files and the record.
func (s Set) prepare() ([]string, map[string][]byte, map[string]string, error) {
	if err := s.validate(); err != nil {
		return nil, nil, nil, err
	}
	names, contents, err := s.embedded()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := os.MkdirAll(s.DestDir, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("creating %s: %w", s.DestDir, err)
	}
	rec, err := s.loadRecord()
	if err != nil {
		return nil, nil, nil, err
	}
	return names, contents, rec, nil
}

// Provision writes every embedded file that does not exist yet in DestDir
// and records its hash. Existing files are left untouched and unrecorded.
// Used on first install. Returns the number of files written.
func Provision(s Set) (int, error) {
	names, contents, rec, err := s.prepare()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, name := range names {
		dest := filepath.Join(s.DestDir, name)
		if _, err := os.Stat(dest); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return count, fmt.Errorf("checking %s: %w", name, err)
		}
		if err := os.WriteFile(dest, contents[name], 0644); err != nil {
			return count, fmt.Errorf("writing %s: %w", name, err)
		}
		rec[name] = Hash(contents[name])
		count++
	}
	if err := s.saveRecord(rec); err != nil {
		return count, err
	}
	return count, nil
}

// classify determines the status of one embedded file at its destination.
func (s Set) classify(name string, content []byte, rec map[string]string) Status {
	st := Status{Name: name, EmbeddedHash: Hash(content)}
	installedHash, wasInstalled := rec[name]
	st.InstalledHash = installedHash

	data, err := os.ReadFile(filepath.Join(s.DestDir, name))
	switch {
	case os.IsNotExist(err):
		if wasInstalled {
			st.Status = StatusMissing
		} else {
			st.Status = StatusNew
		}
		return st
	case err != nil:
		st.Status = StatusError
		return st
	}

	st.CurrentHash = Hash(data)
	switch {
	case st.CurrentHash == st.EmbeddedHash:
		st.Status = StatusOK
	case wasInstalled && st.CurrentHash == installedHash:
		st.Status = StatusOutdated
	case wasInstalled, s.PreserveUntracked:
		st.Status = StatusModified
	default:
		st.Status = StatusUntracked
	}
	return st
}

// Check reports the status of every embedded file. It does not write.
func Check(s Set) (*Report, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	names, contents, err := s.embedded()
	if err != nil {
		return nil, err
	}
	rec, err := s.loadRecord()
	if err != nil {
		return nil, err
	}
	report := &Report{}
	for _, name := range names {
		st := s.classify(name, contents[name], rec)
		switch st.Status {
		case StatusOK:
			report.OK++
		case StatusOutdated:
			report.Outdated++
		case StatusModified:
			report.Modified++
		case StatusMissing:
			report.Missing++
		case StatusNew:
			report.New++
		case StatusUntracked:
			report.Untracked++
		case StatusError:
			report.Error++
		}
		report.Files = append(report.Files, st)
	}
	return report, nil
}

// Update writes the embedded version of every file that is outdated,
// missing, new or untracked (unless PreserveUntracked), and records it.
// Modified files are skipped. A file on disk that already equals the
// embedded one but has no record entry is adopted into the record, so a
// later release can refresh it.
// Returns counts of files updated (incl. new), skipped (modified) and
// reinstalled (missing).
func Update(s Set) (updated, skipped, reinstalled int, err error) {
	names, contents, rec, err := s.prepare()
	if err != nil {
		return 0, 0, 0, err
	}
	for _, name := range names {
		st := s.classify(name, contents[name], rec)
		switch st.Status {
		case StatusOK:
			rec[name] = st.EmbeddedHash
			continue
		case StatusModified:
			skipped++
			continue
		case StatusError:
			continue
		}
		if err := os.WriteFile(filepath.Join(s.DestDir, name), contents[name], 0644); err != nil {
			return updated, skipped, reinstalled, fmt.Errorf("writing %s: %w", name, err)
		}
		rec[name] = st.EmbeddedHash
		if st.Status == StatusMissing {
			reinstalled++
		} else {
			updated++
		}
	}
	if err := s.saveRecord(rec); err != nil {
		return updated, skipped, reinstalled, err
	}
	return updated, skipped, reinstalled, nil
}
