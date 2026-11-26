package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crate makes a crate folder with the manifest packaging reads.
func crate(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "port-rs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"unit":"src/text.ts","crate":"example.com/x/port-rs","exports":[{"name":"count"},{"name":"pad"},{"name":"generic","blocked":"is generic"}]}`
	if err := os.WriteFile(filepath.Join(dir, "golang-anything.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLibraryShapeWritesAPackageThatNamesEveryPlatform(t *testing.T) {
	dir := crate(t)
	res, err := Run(Options{CrateDir: dir, Name: "port-rs", Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Shape != "library" {
		t.Fatalf("%+v", res)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("package.json is not JSON: %v\n%s", err, raw)
	}
	optional, _ := manifest["optionalDependencies"].(map[string]any)
	if len(optional) != len(DefaultPlatforms) {
		t.Errorf("every platform needs its own package: %+v", optional)
	}
	if manifest["version"] != "1.2.3" {
		t.Errorf("version: %v", manifest["version"])
	}
	// The workflow is what builds the other platforms, and it must not publish for you.
	flow, err := os.ReadFile(filepath.Join(dir, "package", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(flow), "npm publish") {
		t.Error("the generated workflow must not publish")
	}
	if !strings.Contains(string(flow), "matrix") || !strings.Contains(string(flow), "CGO_ENABLED=1") {
		t.Errorf("workflow:\n%s", flow)
	}
}

func TestAShapeChangeRemovesWhatTheLastRunWrote(t *testing.T) {
	dir := crate(t)
	if _, err := Run(Options{CrateDir: dir, Name: "port-rs", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "package", "go.mod")); err != nil {
		t.Fatal("the library shape should write go.mod")
	}
	if _, err := Run(Options{CrateDir: dir, Name: "port-rs", Version: "1.0.0", Shape: "cli", Original: "orig@2.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "package", "go.mod")); !os.IsNotExist(err) {
		t.Error("a rerun with another shape left the old go.mod behind")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"orig": "2.0.0"`) {
		t.Errorf("the CLI shape should pin the original it falls back to:\n%s", raw)
	}
}

func TestAnUnknownShapeIsRefusedRatherThanGuessed(t *testing.T) {
	dir := crate(t)
	if _, err := Run(Options{CrateDir: dir, Name: "x", Shape: "wasm"}); err == nil {
		t.Error("an unknown shape must be an error")
	}
}
