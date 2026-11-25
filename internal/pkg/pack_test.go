package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The workflow builds an artifact per platform; pack.mjs is what turns it into the package the loader
// requires. Without it the artifacts are uploads nobody can install.
func TestPackTurnsAnArtifactIntoAPlatformPackage(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := crate(t)
	if _, err := Run(Options{CrateDir: dir, Name: "mk", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(dir, "package", "pack.mjs")
	if _, err := os.Stat(pack); err != nil {
		t.Fatalf("pack.mjs is missing: %v", err)
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"libport.dylib", "addon.node", "index.cjs"} {
		if err := os.WriteFile(filepath.Join(dist, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("node", "pack.mjs", "darwin-arm64", dist)
	cmd.Dir = filepath.Join(dir, "package")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pack: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "package", "mk-darwin-arm64", "package.json"))
	if err != nil {
		t.Fatalf("the platform package was not written: %v\n%s", err, out)
	}
	for _, want := range []string{`"name": "mk-darwin-arm64"`, `"version": "1.0.0"`, `"main": "index.cjs"`} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest is missing %s:\n%s", want, manifest)
		}
	}
}
