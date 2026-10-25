// Package assetsync provisions files embedded in the gt binary into a town
// and keeps them current across upgrades without overwriting user edits.
//
// A Set maps one directory of an embedded filesystem to one destination
// directory. The hashes of the files gt wrote are kept in
// <DestDir>/.installed.json under Set.RecordKey. That record is what lets an
// upgrade tell "unmodified since install" (safe to refresh) from "edited by
// the user" (kept as is). Other top-level keys in the record are preserved.
//
// Safety rules: every write goes to a temporary file in the same directory
// and is renamed into place; DestDir, the record and each target are never
// written through a symlink; a failure on one file does not stop the others,
// and the record is saved for every file that was written.
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
	StatusError     = "error"     // unreadable, or a symlink (never written through)
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

// writeFile writes data to dir/name atomically. A variable so tests can
// inject write failures.
var writeFile = writeFileAtomic

// refuseSymlink returns an error if p exists and is a symlink.
func refuseSymlink(p string) error {
	fi, err := os.Lstat(p)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink; refusing to write through it", p)
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("checking %s: %w", p, err)
	}
	return nil
}

// writeFileAtomic writes data to a temp file in dir and renames it to dir/name.
// It refuses to replace a symlink.
func writeFileAtomic(dir, name string, data []byte) (err error) {
	target := filepath.Join(dir, name)
	if err := refuseSymlink(target); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err = tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
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

// checkDestDir fails if DestDir exists as a symlink or as a non-directory.
// It reports whether DestDir exists.
func (s Set) checkDestDir() (bool, error) {
	fi, err := os.Lstat(s.DestDir)
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("checking %s: %w", s.DestDir, err)
	case fi.Mode()&os.ModeSymlink != 0:
		return false, fmt.Errorf("%s is a symlink; refusing to use it", s.DestDir)
	case !fi.IsDir():
		return false, fmt.Errorf("%s is not a directory", s.DestDir)
	}
	return true, nil
}

// embedded returns the names (sorted) and contents of the files of the Set.
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
// A missing record or a record without the Set's key yields an empty map.
func LoadRecord(s Set) (map[string]string, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s.loadRecord()
}

// SaveRecord replaces the recorded install hashes of the Set, keeping the
// entries of other record keys.
func SaveRecord(s Set, rec map[string]string) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.saveRecord(rec)
}

// loadRaw reads the whole record as top-level key -> raw JSON.
func (s Set) loadRaw() (map[string]json.RawMessage, error) {
	p := filepath.Join(s.DestDir, RecordFile)
	if err := refuseSymlink(p); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading installed record: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing installed record: %w", err)
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	return raw, nil
}

func (s Set) loadRecord() (map[string]string, error) {
	raw, err := s.loadRaw()
	if err != nil {
		return nil, err
	}
	rec := make(map[string]string)
	if msg, ok := raw[s.RecordKey]; ok {
		var entries map[string]string
		if err := json.Unmarshal(msg, &entries); err != nil {
			return nil, fmt.Errorf("parsing installed record key %q: %w", s.RecordKey, err)
		}
		for k, v := range entries {
			rec[k] = v
		}
	}
	return rec, nil
}

func (s Set) saveRecord(rec map[string]string) error {
	raw, err := s.loadRaw()
	if err != nil {
		return fmt.Errorf("saving installed record: %w", err)
	}
	if rec == nil {
		rec = map[string]string{}
	}
	entries, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encoding installed record: %w", err)
	}
	merged := make(map[string]json.RawMessage, len(raw)+1)
	for k, v := range raw {
		merged[k] = v
	}
	merged[s.RecordKey] = entries
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding installed record: %w", err)
	}
	if err := writeFile(s.DestDir, RecordFile, data); err != nil {
		return fmt.Errorf("saving installed record: %w", err)
	}
	return nil
}

// prepare validates the Set, ensures DestDir is a real directory (creating
// it if absent) and loads the embedded files and the record.
func (s Set) prepare() ([]string, map[string][]byte, map[string]string, error) {
	if err := s.validate(); err != nil {
		return nil, nil, nil, err
	}
	names, contents, err := s.embedded()
	if err != nil {
		return nil, nil, nil, err
	}
	exists, err := s.checkDestDir()
	if err != nil {
		return nil, nil, nil, err
	}
	if !exists {
		if err := os.MkdirAll(s.DestDir, 0755); err != nil {
			return nil, nil, nil, fmt.Errorf("creating %s: %w", s.DestDir, err)
		}
	}
	rec, err := s.loadRecord()
	if err != nil {
		return nil, nil, nil, err
	}
	return names, contents, rec, nil
}

// Provision writes every embedded file that does not exist yet in DestDir
// and records its hash. Existing files are left untouched and unrecorded;
// a symlink at a target is refused with an error. A failure on one file does
// not stop the others; the record is saved for the files written and all
// errors are returned joined. Used on first install. Returns the number of
// files written.
func Provision(s Set) (int, error) {
	names, contents, rec, err := s.prepare()
	if err != nil {
		return 0, err
	}
	count := 0
	var errs []error
	for _, name := range names {
		dest := filepath.Join(s.DestDir, name)
		fi, err := os.Lstat(dest)
		switch {
		case err == nil && fi.Mode()&os.ModeSymlink != 0:
			errs = append(errs, fmt.Errorf("%s is a symlink; refusing to write through it", dest))
			continue
		case err == nil:
			continue // exists: never overwrite on provision
		case !os.IsNotExist(err):
			errs = append(errs, fmt.Errorf("checking %s: %w", name, err))
			continue
		}
		if err := writeFile(s.DestDir, name, contents[name]); err != nil {
			errs = append(errs, err)
			continue
		}
		rec[name] = Hash(contents[name])
		count++
	}
	if err := s.saveRecord(rec); err != nil {
		errs = append(errs, err)
	}
	return count, errors.Join(errs...)
}

// classify determines the status of one embedded file at its destination.
// The returned error is non-nil only when the target is a symlink.
func (s Set) classify(name string, content []byte, rec map[string]string) (Status, error) {
	st := Status{Name: name, EmbeddedHash: Hash(content)}
	installedHash, wasInstalled := rec[name]
	st.InstalledHash = installedHash

	dest := filepath.Join(s.DestDir, name)
	if err := refuseSymlink(dest); err != nil {
		st.Status = StatusError
		return st, err
	}
	data, err := os.ReadFile(dest)
	switch {
	case os.IsNotExist(err):
		if wasInstalled {
			st.Status = StatusMissing
		} else {
			st.Status = StatusNew
		}
		return st, nil
	case err != nil:
		st.Status = StatusError
		return st, nil
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
	return st, nil
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
	if _, err := s.checkDestDir(); err != nil {
		return nil, err
	}
	rec, err := s.loadRecord()
	if err != nil {
		return nil, err
	}
	report := &Report{}
	for _, name := range names {
		st, _ := s.classify(name, contents[name], rec) // a symlink is reported as StatusError
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
// Modified and unreadable files are skipped; a symlink at a target is
// refused with an error. A file on disk that already equals the embedded
// one but has no record entry is adopted into the record, so a later release
// can refresh it. A failure on one file does not stop the others; the record
// is saved for the files written and all errors are returned joined.
// Returns counts of files updated (incl. new), skipped (modified) and
// reinstalled (missing).
func Update(s Set) (updated, skipped, reinstalled int, err error) {
	names, contents, rec, err := s.prepare()
	if err != nil {
		return 0, 0, 0, err
	}
	var errs []error
	for _, name := range names {
		st, symErr := s.classify(name, contents[name], rec)
		switch st.Status {
		case StatusOK:
			rec[name] = st.EmbeddedHash
			continue
		case StatusModified:
			skipped++
			continue
		case StatusError:
			if symErr != nil {
				errs = append(errs, symErr)
			}
			continue
		}
		if err := writeFile(s.DestDir, name, contents[name]); err != nil {
			errs = append(errs, err)
			continue
		}
		rec[name] = st.EmbeddedHash
		if st.Status == StatusMissing {
			reinstalled++
		} else {
			updated++
		}
	}
	if err := s.saveRecord(rec); err != nil {
		errs = append(errs, err)
	}
	return updated, skipped, reinstalled, errors.Join(errs...)
}
