//go:build cgo

package ts

import "testing"

// A static method that reads or writes class state is not a function: the port has no class variables, so a
// recorded answer would depend on a call order. Carbon's static factories are exactly this shape.
func TestAStatefulMethodIsMarked(t *testing.T) {
	src := []byte(`<?php
class Counter {
    private static int $count = 0;
    public static function bump(int $n): int { self::$count += $n; return self::$count; }
    public static function pure(int $n): int { return $n * 2; }
}`)
	file, err := ParseSource(src, Php, "counter.php")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, fn := range file.Fns {
		got[fn.Name] = fn.State
	}
	if !got["bump"] {
		t.Error("a method touching self::$count was not marked as stateful")
	}
	if got["pure"] {
		t.Error("a pure static method was marked stateful")
	}
}

// A plain def inside a Ruby module is an instance method, not a module function: reading it as a free
// function made the recorder call it on Object.
func TestRubyInstanceMethodsAreNotExports(t *testing.T) {
	src := []byte(`module Util
  def helper(n)
    n * 2
  end

  def self.double(n)
    n * 2
  end
end
`)
	file, err := ParseSource(src, Ruby, "util.rb")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, fn := range file.Fns {
		names[fn.Name] = true
	}
	if names["helper"] {
		t.Error("an instance method of a module was offered as an export")
	}
	if !names["double"] {
		t.Error("a module function was not offered")
	}
	// Dropped is not the same as unseen: a library whose surface is instance methods has to report them, or
	// "0 exports" reads like an empty file.
	if file.InstanceMethods != 1 {
		t.Errorf("the instance method was dropped instead of counted: InstanceMethods = %d, want 1",
			file.InstanceMethods)
	}
	if file.InstanceFree != 1 {
		t.Errorf("InstanceFree = %d, want 1: `helper` reads nothing but its argument", file.InstanceFree)
	}
}

// `$this` is a different token from `self::`, and it is the one that decides whether an instance method
// could ever be promoted to an instance-free function. A detector that only knew class variables passes
// every test above and still marks `$this->offset` as pure.
func TestAThisMethodIsMarked(t *testing.T) {
	src := []byte(`<?php
class Clock {
    private int $offset = 0;
    public static function shifted(int $now): int { return $now + $this->offset; }
    public static function plain(int $n): int { return $n * 2; }
}`)
	file, err := ParseSource(src, Php, "clock.php")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, fn := range file.Fns {
		got[fn.Name] = fn.State
	}
	if !got["shifted"] {
		t.Error("a method reading $this->offset was not marked as stateful")
	}
	if got["plain"] {
		t.Error("a pure static method was marked stateful")
	}
}

// PHP has the same shape: a public method that is not static needs an object, and the count is what tells a
// Carbon-sized library apart from an empty one.
func TestPhpInstanceMethodsAreCountedNotOffered(t *testing.T) {
	src := []byte(`<?php
class Timer {
    public function elapsed(int $now): int { return $now; }
    public function shifted(int $now): int { return $now + $this->offset; }
    public static function twice(int $n): int { return $n * 2; }
}`)
	file, err := ParseSource(src, Php, "timer.php")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range file.Fns {
		if fn.Name != "twice" {
			t.Errorf("%s was offered as an export; only the static method is callable without an object", fn.Name)
		}
	}
	if file.InstanceMethods != 2 {
		t.Errorf("InstanceMethods = %d, want 2", file.InstanceMethods)
	}
	// `elapsed` only reads its argument; `shifted` reads $this->offset. One of the two could be promoted.
	if file.InstanceFree != 1 {
		t.Errorf("InstanceFree = %d, want 1: only `elapsed` is free of state", file.InstanceFree)
	}
}

// Java is the language where a library is classes full of instance methods, and the plan said nothing about them:
// no count, no sentence, only the static methods as exports.
func TestJavaInstanceMethodsAreCounted(t *testing.T) {
	src := []byte(`public final class Timer {
    private int offset = 5;

    public int shifted(int n) { return n + this.offset; }

    public int double(int n) { return n * 2; }

    public static int twice(int n) { return n * 2; }
}`)
	file, err := ParseSource(src, Java, "Timer.java")
	if err != nil {
		t.Fatal(err)
	}
	// The class declares `private int offset`, so no method can be called state-free: `return n + offset;` reads the
	// same state as `this.offset`, and looking for "this." alone called that free.
	if file.InstanceMethods != 2 || file.InstanceFree != 0 {
		t.Errorf("InstanceMethods=%d InstanceFree=%d, want 2/0", file.InstanceMethods, file.InstanceFree)
	}
	for _, fn := range file.Fns {
		if fn.Name != "twice" {
			t.Errorf("%s was offered as an export; only the static method needs no object", fn.Name)
		}
	}
}

// A class with no instance fields is the one case a proxy could reach safely, and it has to be counted.
func TestJavaFreeMethodsNeedAFieldlessClass(t *testing.T) {
	src := []byte(`public final class Maths {
    public int double(int n) { return n * 2; }

    public int shifted(int n) { return n + 1; }
}`)
	file, err := ParseSource(src, Java, "Maths.java")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstanceMethods != 2 || file.InstanceFree != 2 {
		t.Errorf("InstanceMethods=%d InstanceFree=%d, want 2/2", file.InstanceMethods, file.InstanceFree)
	}
}
