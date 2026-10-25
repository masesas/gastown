package assetsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"src/a.md":     {Data: []byte("alpha v1\n")},
		"src/b.md":     {Data: []byte("beta v1\n")},
		"src/skip.txt": {Data: []byte("ignored\n")},
		"src/sub/c.md": {Data: []byte("nested, ignored\n")},
	}
}

func testSet(dest string, preserveUntracked bool) Set {
	return Set{
		FS:                testFS(),
		SrcDir:            "src",
		DestDir:           dest,
		RecordKey:         "files",
		Match:             func(name string) bool { return filepath.Ext(name) == ".md" },
		PreserveUntracked: preserveUntracked,
	}
}

func readRecord(t *testing.T, dir, key string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, RecordFile))
	if err != nil {
		t.Fatalf("reading record: %v", err)
	}
	var raw map[string]map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing record: %v", err)
	}
	return raw[key]
}

func TestProvision_FreshWritesAndRecords(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	s := testSet(dest, false)

	n, err := Provision(s)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if n != 2 {
		t.Fatalf("provisioned %d, want 2", n)
	}
	for name, want := range map[string]string{"a.md": "alpha v1\n", "b.md": "beta v1\n"} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "skip.txt")); !os.IsNotExist(err) {
		t.Errorf("skip.txt must not be provisioned (Match filter)")
	}
	rec := readRecord(t, dest, "files")
	if rec["a.md"] != Hash([]byte("alpha v1\n")) || rec["b.md"] != Hash([]byte("beta v1\n")) {
		t.Errorf("record = %v, want hashes of a.md and b.md", rec)
	}
	if len(rec) != 2 {
		t.Errorf("record has %d entries, want 2", len(rec))
	}
}

func TestProvision_SkipsExistingFile(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("mine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := Provision(testSet(dest, true))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if n != 1 {
		t.Fatalf("provisioned %d, want 1", n)
	}
	got, _ := os.ReadFile(filepath.Join(dest, "a.md"))
	if string(got) != "mine\n" {
		t.Errorf("existing file overwritten: %q", got)
	}
	if _, ok := readRecord(t, dest, "files")["a.md"]; ok {
		t.Errorf("skipped file must not be recorded as installed")
	}
}

func TestProvision_CorruptRecordFails(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, RecordFile), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Provision(testSet(dest, false)); err == nil {
		t.Fatal("expected error for corrupt record")
	}
}

func TestProvision_RecordKeyIsolation(t *testing.T) {
	dest := t.TempDir()
	s := testSet(dest, false)
	s.RecordKey = "formulas"
	if _, err := Provision(s); err != nil {
		t.Fatal(err)
	}
	if len(readRecord(t, dest, "formulas")) != 2 {
		t.Errorf("record not stored under configured key")
	}
}

func TestCheck_Classification(t *testing.T) {
	dest := t.TempDir()
	s := testSet(dest, false)
	if _, err := Provision(s); err != nil {
		t.Fatal(err)
	}

	r, err := Check(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.OK != 2 || len(r.Files) != 2 {
		t.Fatalf("fresh check = %+v, want 2 ok", r)
	}
	if r.Files[0].Name != "a.md" || r.Files[1].Name != "b.md" {
		t.Errorf("files not sorted by name: %+v", r.Files)
	}

	// modified: tracked file changed by the user
	if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("edited\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// missing: tracked file deleted
	if err := os.Remove(filepath.Join(dest, "b.md")); err != nil {
		t.Fatal(err)
	}
	r, err = Check(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Modified != 1 || r.Missing != 1 || r.OK != 0 {
		t.Errorf("check = %+v, want 1 modified, 1 missing", r)
	}
}

func TestCheck_NewOutdatedUntracked(t *testing.T) {
	dest := t.TempDir()
	// a.md: tracked with an old hash and still holding the old content -> outdated
	if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("alpha v0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, dest, "files", map[string]string{"a.md": Hash([]byte("alpha v0\n"))})
	// b.md absent, not recorded -> new

	r, err := Check(testSet(dest, false))
	if err != nil {
		t.Fatal(err)
	}
	if r.Outdated != 1 || r.New != 1 {
		t.Errorf("check = %+v, want 1 outdated, 1 new", r)
	}

	// untracked: exists, differs, no record entry
	dest2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest2, "a.md"), []byte("hand written\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err = Check(testSet(dest2, false))
	if err != nil {
		t.Fatal(err)
	}
	if r.Untracked != 1 {
		t.Errorf("default policy: check = %+v, want 1 untracked", r)
	}
	r, err = Check(testSet(dest2, true))
	if err != nil {
		t.Fatal(err)
	}
	if r.Untracked != 0 || r.Modified != 1 || r.Files[0].Status != StatusModified {
		t.Errorf("PreserveUntracked: check = %+v, want untracked file reported as modified", r)
	}
}

func writeRecord(t *testing.T, dir, key string, entries map[string]string) {
	t.Helper()
	data, err := json.Marshal(map[string]map[string]string{key: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, RecordFile), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdate_RefreshesUnmodifiedSkipsModified(t *testing.T) {
	dest := t.TempDir()
	// a.md unmodified since an older install -> refreshed
	if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("alpha v0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// b.md modified by the user after an older install -> kept
	if err := os.WriteFile(filepath.Join(dest, "b.md"), []byte("user edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, dest, "files", map[string]string{
		"a.md": Hash([]byte("alpha v0\n")),
		"b.md": Hash([]byte("beta v0\n")),
	})

	updated, skipped, reinstalled, err := Update(testSet(dest, true))
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 || skipped != 1 || reinstalled != 0 {
		t.Errorf("Update = (%d,%d,%d), want (1,1,0)", updated, skipped, reinstalled)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.md")); string(got) != "alpha v1\n" {
		t.Errorf("a.md = %q, want refreshed", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "b.md")); string(got) != "user edit\n" {
		t.Errorf("b.md = %q, user edit overwritten", got)
	}
	rec := readRecord(t, dest, "files")
	if rec["a.md"] != Hash([]byte("alpha v1\n")) {
		t.Errorf("record for a.md not refreshed: %v", rec)
	}
	if rec["b.md"] != Hash([]byte("beta v0\n")) {
		t.Errorf("record for modified b.md must keep the installed hash: %v", rec)
	}
}

func TestUpdate_MissingAndNew(t *testing.T) {
	dest := t.TempDir()
	writeRecord(t, dest, "files", map[string]string{"a.md": Hash([]byte("alpha v1\n"))})

	updated, skipped, reinstalled, err := Update(testSet(dest, false))
	if err != nil {
		t.Fatal(err)
	}
	// a.md was installed and deleted -> reinstalled; b.md never installed -> updated (new)
	if updated != 1 || skipped != 0 || reinstalled != 1 {
		t.Errorf("Update = (%d,%d,%d), want (1,0,1)", updated, skipped, reinstalled)
	}
}

func TestUpdate_UntrackedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		preserve bool
		want     string
	}{
		{"default overwrites untracked", false, "alpha v1\n"},
		{"preserve keeps untracked", true, "hand written\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()
			if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("hand written\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := Update(testSet(dest, tc.preserve)); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(filepath.Join(dest, "a.md")); string(got) != tc.want {
				t.Errorf("a.md = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdate_AdoptsIdenticalUntrackedFile(t *testing.T) {
	dest := t.TempDir()
	// A file copied by hand that equals the shipped content becomes tracked,
	// so a later release can refresh it.
	if err := os.WriteFile(filepath.Join(dest, "a.md"), []byte("alpha v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Update(testSet(dest, true)); err != nil {
		t.Fatal(err)
	}
	if readRecord(t, dest, "files")["a.md"] != Hash([]byte("alpha v1\n")) {
		t.Errorf("identical untracked file not adopted into the record")
	}
}

func TestSet_Validate(t *testing.T) {
	for _, s := range []Set{
		{SrcDir: "src", DestDir: "d", RecordKey: "k"},
		{FS: testFS(), DestDir: "d", RecordKey: "k"},
		{FS: testFS(), SrcDir: "src", RecordKey: "k"},
		{FS: testFS(), SrcDir: "src", DestDir: "d"},
	} {
		if _, err := Provision(s); err == nil {
			t.Errorf("Provision(%+v) accepted an incomplete set", s)
		}
	}
}
