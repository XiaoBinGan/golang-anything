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
