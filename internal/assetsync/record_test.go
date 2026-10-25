package assetsync

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedHashes(t *testing.T) {
	got, err := EmbeddedHashes(testSet("unused", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a.md"] != Hash([]byte("alpha v1\n")) {
		t.Errorf("EmbeddedHashes = %v", got)
	}
	if _, err := EmbeddedHashes(Set{}); err == nil {
		t.Error("EmbeddedHashes accepted an empty set")
	}
	bad := testSet("unused", false)
	bad.SrcDir = "nope"
	if _, err := EmbeddedHashes(bad); err == nil {
		t.Error("EmbeddedHashes accepted a missing SrcDir")
	}
}

func TestLoadSaveRecord_RoundTrip(t *testing.T) {
	s := testSet(t.TempDir(), false)
	rec, err := LoadRecord(s)
	if err != nil || len(rec) != 0 {
		t.Fatalf("LoadRecord on empty dir = %v, %v", rec, err)
	}
	if err := SaveRecord(s, map[string]string{"a.md": "h1"}); err != nil {
		t.Fatal(err)
	}
	rec, err = LoadRecord(s)
	if err != nil || rec["a.md"] != "h1" {
		t.Fatalf("LoadRecord = %v, %v", rec, err)
	}
	// A record with another key yields an empty map for this set.
	other := s
	other.RecordKey = "other"
	if rec, err := LoadRecord(other); err != nil || len(rec) != 0 {
		t.Errorf("LoadRecord(other key) = %v, %v", rec, err)
	}
	if _, err := LoadRecord(Set{}); err == nil {
		t.Error("LoadRecord accepted an empty set")
	}
	if err := SaveRecord(Set{}, nil); err == nil {
		t.Error("SaveRecord accepted an empty set")
	}
	// Saving into a missing directory fails with an error, not a panic.
	missing := testSet(filepath.Join(t.TempDir(), "absent"), false)
	if err := SaveRecord(missing, nil); err == nil {
		t.Error("SaveRecord into a missing directory succeeded")
	}
}

func TestCheck_Errors(t *testing.T) {
	if _, err := Check(Set{}); err == nil {
		t.Error("Check accepted an empty set")
	}
	bad := testSet(t.TempDir(), false)
	bad.SrcDir = "nope"
	if _, err := Check(bad); err == nil {
		t.Error("Check accepted a missing SrcDir")
	}
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, RecordFile), []byte("["), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(testSet(dest, false)); err == nil {
		t.Error("Check accepted a corrupt record")
	}
	if _, _, _, err := Update(testSet(dest, false)); err == nil {
		t.Error("Update accepted a corrupt record")
	}
}

func TestUnreadableFile_ReportedAsError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits not enforced")
	}
	dest := t.TempDir()
	p := filepath.Join(dest, "a.md")
	if err := os.WriteFile(p, []byte("x"), 0000); err != nil {
		t.Fatal(err)
	}
	r, err := Check(testSet(dest, false))
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != 1 {
		t.Errorf("check = %+v, want 1 error", r)
	}
	// Update leaves an unreadable file alone.
	if _, _, _, err := Update(testSet(dest, false)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "x" {
		t.Errorf("unreadable file was overwritten: %q", got)
	}
}

func TestProvision_StatAndWriteErrors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits not enforced")
	}
	dest := t.TempDir()
	if err := os.Chmod(dest, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dest, 0755) })
	if _, err := Provision(testSet(dest, false)); err == nil {
		t.Error("Provision into a read-only directory succeeded")
	}
	if _, _, _, err := Update(testSet(dest, false)); err == nil {
		t.Error("Update into a read-only directory succeeded")
	}
	// MkdirAll failure: DestDir below a regular file.
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Provision(testSet(filepath.Join(file, "sub"), false)); err == nil {
		t.Error("Provision below a regular file succeeded")
	}
}
