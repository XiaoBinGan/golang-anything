package promote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shim has to give the file back byte for byte, or the takeover is one nobody can undo.
func TestInsertThenRevertRestoresTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timer.rb")
	src := "class Timer\n  def double(n)\n    n * 2\n  end\nend\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := Insert(path, "Timer", []string{"double"})
	if err != nil || !added {
		t.Fatalf("Insert = %v, %v", added, err)
	}
	shimmed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shimmed), "def self.double(*args)") {
		t.Errorf("no shim in the file:\n%s", shimmed)
	}
	removed, err := Revert(path, "Timer")
	if err != nil || !removed {
		t.Fatalf("Revert = %v, %v", removed, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != src {
		t.Errorf("revert did not restore the original:\nwant %q\ngot  %q", src, back)
	}
}

// A hand-edited block is refused rather than half-removed.
func TestRevertRefusesATruncatedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timer.rb")
	src := "class Timer\nend\n# >>> go-anything promote: Timer\nclass Timer\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Revert(path, "Timer"); err == nil {
		t.Fatal("a truncated block must be reported")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != src {
		t.Errorf("the refused revert changed the file:\n%s", after)
	}
}

// PHP cannot reopen a class, so the shim is a proxy class in its own file: `class Timer` twice is a fatal error.
// The proxy has to parse, and a revert that leaves an empty `<?php` behind is a revert nobody can see.
func TestThePHPSentinelIsAProxynItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "promoted.php")
	added, err := InsertPHP(path, "Timer", []Method{{
		Name:    "double",
		Params:  []Param{{Name: "$n", Type: "int"}},
		Returns: "int",
	}}, "timer.php")
	if err != nil || !added {
		t.Fatalf("InsertPHP = %v, %v", added, err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.HasPrefix(text, "<?php") || !strings.Contains(text, "class TimerPromoted") {
		t.Errorf("the proxy is not a PHP file with a proxy class:\n%s", text)
	}
	// Typed and required: a variadic proxy is unbridgeable and `new \\Timer` needs the original loaded.
	if !strings.Contains(text, "public static function double(int $n): int") {
		t.Errorf("the proxy does not declare the method's own types:\n%s", text)
	}
	if !strings.Contains(text, "require_once __DIR__ . '/timer.php';") {
		t.Errorf("the proxy does not load the class it instantiates:\n%s", text)
	}
	if !strings.Contains(text, "return (new \\Timer)->double($n);") {
		t.Errorf("the proxy does not reach the instance method:\n%s", text)
	}
	removed, err := Revert(path, "Timer")
	if err != nil || !removed {
		t.Fatalf("Revert = %v, %v", removed, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(back)) != "<?php" {
		t.Errorf("the revert left more than the header:\n%s", back)
	}
}

// Java proxies are one file per class, named after it, with Java comments — and `new Owner()` is what the method
// call needs, which is why analyze only offers a class with a no-arg constructor.
func TestTheJavaProxyCompilesAsJava(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MathsPromoted.java")
	added, err := InsertJava(path, "Maths", []Method{{
		Name:    "doubled",
		Params:  []Param{{Name: "n", Type: "int"}},
		Returns: "int",
	}})
	if err != nil || !added {
		t.Fatalf("InsertJava = %v, %v", added, err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if strings.Contains(text, "#") {
		t.Errorf("the file uses # where Java wants //:\n%s", text)
	}
	if !strings.Contains(text, "public static int doubled(int n)") ||
		!strings.Contains(text, "return new Maths().doubled(n);") {
		t.Errorf("the proxy does not forward with a no-arg constructor:\n%s", text)
	}
	removed, err := RevertJava(path, "Maths")
	if err != nil || !removed {
		t.Fatalf("RevertJava = %v, %v", removed, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(back)) != "" {
		t.Errorf("the revert left content behind:\n%s", back)
	}
}

// Kotlin proxies are an `object`: its members are reachable as `TimerPromoted.name(...)`, which is the shape analyze
// reads, and `Owner()` needs no arguments — the condition that produced the entry.
func TestTheKotlinProxyIsASingletonObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TimerPromoted.kt")
	added, err := InsertKotlin(path, "Timer", []Method{{
		Name:    "doubled",
		Params:  []Param{{Name: "n", Type: "int"}},
		Returns: "int",
	}})
	if err != nil || !added {
		t.Fatalf("InsertKotlin = %v, %v", added, err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "object TimerPromoted") || !strings.Contains(text, "fun doubled(n: Int): Int = Timer().doubled(n)") {
		t.Errorf("the proxy is not a singleton forwarding with a no-arg constructor:\n%s", text)
	}
	if strings.Contains(text, "#") {
		t.Errorf("the file uses # where Kotlin wants //:\n%s", text)
	}
}

// A C# proxy is a static class forwarding to `new Owner()` — C# cannot add a static member to a class from outside
// either. `dotnet build` on the same shape reports 0 errors.
func TestTheCSharpProxyIsAStaticClass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MathsPromoted.cs")
	added, err := InsertCSharp(path, "Maths", []Method{{
		Name:    "Doubled",
		Params:  []Param{{Name: "n", Type: "int"}},
		Returns: "int",
	}})
	if err != nil || !added {
		t.Fatalf("InsertCSharp = %v, %v", added, err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "public static class MathsPromoted") ||
		!strings.Contains(text, "return new Maths().Doubled(n);") {
		t.Errorf("the proxy does not forward with a no-arg constructor:\n%s", text)
	}
	if strings.Contains(text, "#") {
		t.Errorf("the file uses # where C# wants //:\n%s", text)
	}
}
