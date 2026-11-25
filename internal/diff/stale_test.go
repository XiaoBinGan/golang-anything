package diff

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A prebuilt driver from an earlier build answers the port as it was, so a crate that no longer compiles
// used to come back "every case matches". The driver is rebuilt when a source file is newer.
func TestAStaleDriverIsRebuiltRatherThanTrusted(t *testing.T) {
	dir := t.TempDir()
	crate := filepath.Join(dir, "crate")
	if err := os.MkdirAll(filepath.Join(crate, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join(crate, "work", "godriver")
	if err := os.WriteFile(driver, []byte("not really a program"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(driver, old, old); err != nil {
		t.Fatal(err)
	}
	// A source file touched after the driver was built makes it stale.
	src := filepath.Join(crate, "port.go")
	if err := os.WriteFile(src, []byte("package port\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := sourcesNewerThan(crate, old)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatal("a source newer than the driver must make it stale")
	}
	// With nothing newer, the prebuilt binary is what runs.
	fresh := time.Now().Add(time.Hour)
	stale, err = sourcesNewerThan(crate, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Error("a driver newer than every source must not be rebuilt")
	}
}

// The work folder holds the driver and the answers; walking it would compare the driver against itself
// and rebuild on every call.
func TestTheWorkFolderIsNotASource(t *testing.T) {
	dir := t.TempDir()
	crate := filepath.Join(dir, "crate")
	if err := os.MkdirAll(filepath.Join(crate, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(crate, "work", "generated.go")
	if err := os.WriteFile(inside, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := sourcesNewerThan(crate, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Error("a file under work/ is not a source")
	}
}
