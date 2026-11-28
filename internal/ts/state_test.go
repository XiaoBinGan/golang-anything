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

// Kotlin keeps the class name in a `type_identifier` child, not in a `name` field, so reading the field answered ""
// and every member function came out without an owner: `double` looked like a free function called on nobody.
func TestKotlinClassMembersKeepTheirOwner(t *testing.T) {
	src := []byte("class Timer(val offset: Int) {\n    fun double(n: Int): Int = n * 2\n}\n")
	file, err := ParseSource(src, Kotlin, "M.kt")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range file.Fns {
		if fn.Name != "double" {
			continue
		}
		if !fn.Method || fn.Receiver != "Timer" {
			t.Errorf("double: method=%v receiver=%q, want true/\"Timer\"", fn.Method, fn.Receiver)
		}
		return
	}
	t.Fatal("double was not read at all")
}

// Kotlin state lives in two places: `val x` in the body, and a constructor parameter marked `val`/`var`. Checking only
// the body said a class with `(private val offset: Int)` had none.
func TestKotlinInstanceMethodsAreCounted(t *testing.T) {
	src := []byte(`class Timer(private val offset: Int) {
    fun shifted(n: Int): Int = n + offset

    fun double(n: Int): Int = n * 2
}

class Maths {
    fun triple(n: Int): Int = n * 3
}
`)
	file, err := ParseSource(src, Kotlin, "M.kt")
	if err != nil {
		t.Fatal(err)
	}
	// Timer holds state, so neither of its members is free; Maths holds none, so its member is.
	if file.InstanceMethods != 3 || file.InstanceFree != 1 {
		t.Errorf("InstanceMethods=%d InstanceFree=%d, want 3/1", file.InstanceMethods, file.InstanceFree)
	}
}

// A Java class whose proxy could actually be written: no instance fields, a no-arg constructor, and a return type the
// shim can spell. `InstancePromotable` is what tells a person a Java proxy is even possible.
func TestJavaPromotableNeedsAFieldlessClassAndANoArgConstructor(t *testing.T) {
	withDefault := []byte(`public final class Maths {
    public int double(int n) { return n * 2; }

    public static int twice(int n) { return n * 2; }
}`)
	file, err := ParseSource(withDefault, Java, "Maths.java")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstancePromotable != 1 || len(file.Promotable) != 1 {
		t.Fatalf("InstancePromotable=%d promotable=%d, want 1/1", file.InstancePromotable, len(file.Promotable))
	}
	if p := file.Promotable[0]; p.Owner != "Maths" || p.Name != "double" || p.Returns != "int" {
		t.Errorf("promotable = %+v, want Maths.double returning int", p)
	}

	withCtorArgs := []byte(`public final class Needs {
    public Needs(int unit) { }

    public int double(int n) { return n * 2; }

    public static int twice(int n) { return n * 2; }
}`)
	file, err = ParseSource(withCtorArgs, Java, "Needs.java")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstancePromotable != 0 {
		t.Errorf("InstancePromotable=%d, want 0: new Needs() does not compile", file.InstancePromotable)
	}
}

// A Kotlin `object` is a singleton, and its members are reachable as `Object.name(...)` — which is the shape a proxy
// wants. The parser ignored the body, so a proxy written that way was invisible to `analyze`.
func TestKotlinObjectMembersKeepTheirOwner(t *testing.T) {
	src := []byte("object TimerPromoted {\n    fun doubled(n: Int): Int = n * 2\n}\n")
	file, err := ParseSource(src, Kotlin, "TimerPromoted.kt")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Fns) != 1 {
		t.Fatalf("read %d functions, want 1", len(file.Fns))
	}
	if fn := file.Fns[0]; fn.Name != "doubled" || fn.Receiver != "TimerPromoted" {
		t.Errorf("%s receiver=%q, want doubled/TimerPromoted", fn.Name, fn.Receiver)
	}
}

// C# is the same shape as Java: a class full of instance methods, and a proxy is only possible when the class holds no
// state and `new Owner()` compiles.
func TestCSharpInstanceMethodsAreCounted(t *testing.T) {
	src := []byte(`public class Maths
{
    public int Doubled(int n) { return n * 2; }

    public static int Twice(int n) { return n * 2; }
}
`)
	file, err := ParseSource(src, CSharp, "Maths.cs")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstanceMethods != 1 || file.InstanceFree != 1 || file.InstancePromotable != 1 {
		t.Fatalf("instances=%d free=%d promotable=%d, want 1/1/1",
			file.InstanceMethods, file.InstanceFree, file.InstancePromotable)
	}
	if p := file.Promotable[0]; p.Owner != "Maths" || p.Name != "Doubled" || p.Returns != "int" {
		t.Errorf("promotable = %+v, want Maths.Doubled returning int", p)
	}

	stateful := []byte(`public class Needs
{
    private int offset = 5;

    public int Doubled(int n) { return n * 2; }
}
`)
	file, err = ParseSource(stateful, CSharp, "Needs.cs")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstanceFree != 0 || file.InstancePromotable != 0 {
		t.Errorf("free=%d promotable=%d, want 0/0 for a class with a field",
			file.InstanceFree, file.InstancePromotable)
	}
}

// A Kotlin `object` is a singleton: its members are reachable as `Timer.doubled(...)` and there is no instance to
// create. Counting them as instance methods offered `promote` a target whose proxy would say `Timer()`, which does not
// compile.
func TestKotlinObjectMembersAreNotInstanceMethods(t *testing.T) {
	src := []byte("object Timer {\n    fun doubled(n: Int): Int = n * 2\n}\n")
	file, err := ParseSource(src, Kotlin, "Timer.kt")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Fns) != 1 || file.Fns[0].Receiver != "Timer" {
		t.Fatalf("the member is not an export of the object: %+v", file.Fns)
	}
	if file.InstanceMethods != 0 || file.InstancePromotable != 0 {
		t.Errorf("instances=%d promotable=%d, want 0/0: an object has no instance",
			file.InstanceMethods, file.InstancePromotable)
	}
}

// A secondary constructor lives in the class body and spells its parameters `function_value_parameters`. Reading only
// the class's own children found no constructor at all and answered "no arguments", so `promote` wrote `Timer()` on a
// class whose only constructor takes one — kotlinc: "no value passed for parameter 'unit'".
func TestKotlinSecondaryConstructorBlocksPromotion(t *testing.T) {
	src := []byte("class Timer {\n    constructor(unit: Int) {\n        println(unit)\n    }\n\n    fun doubled(n: Int): Int = n * 2\n}\n")
	file, err := ParseSource(src, Kotlin, "Timer.kt")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstanceFree != 1 {
		t.Errorf("InstanceFree=%d, want 1: doubled reads no state", file.InstanceFree)
	}
	if file.InstancePromotable != 0 {
		t.Errorf("InstancePromotable=%d, want 0: `Timer()` does not compile", file.InstancePromotable)
	}
	// A no-arg secondary constructor is still a way in.
	withNoArg := []byte("class Maths {\n    constructor() {\n        println(1)\n    }\n\n    fun triple(n: Int): Int = n * 3\n}\n")
	file, err = ParseSource(withNoArg, Kotlin, "Maths.kt")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstancePromotable != 1 {
		t.Errorf("InstancePromotable=%d, want 1: `Maths()` compiles", file.InstancePromotable)
	}
}

// The Kotlin bug (a constructor the parse did not see) is worth checking in the other two proxy languages: a class whose
// only constructor takes arguments must not be offered, and C# declares constructors directly in the class body.
func TestCSharpConstructorWithArgumentsBlocksPromotion(t *testing.T) {
	src := []byte(`public class Needs
{
    public Needs(int unit)
    {
        this.unit = unit;
    }

    private int unit;

    public int Doubled(int n) { return n * 2; }
}
`)
	file, err := ParseSource(src, CSharp, "Needs.cs")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstancePromotable != 0 {
		t.Errorf("InstancePromotable=%d, want 0: `new Needs()` does not compile", file.InstancePromotable)
	}

	// Without the field, the only thing standing in the way is the constructor — and with a no-arg one, nothing is.
	withNoArg := []byte(`public class Maths
{
    public Maths()
    {
        System.Console.WriteLine(1);
    }

    public int Tripled(int n) { return n * 3; }
}
`)
	file, err = ParseSource(withNoArg, CSharp, "Maths.cs")
	if err != nil {
		t.Fatal(err)
	}
	if file.InstancePromotable != 1 {
		t.Errorf("InstancePromotable=%d, want 1: `new Maths()` compiles", file.InstancePromotable)
	}
}

// A `native` method has no body in the crate: the JVM links it at run time, so recording one answers
// UnsatisfiedLinkError. It is a blocked export with that reason, not an export the bridge will try to call.
func TestJavaNativeMethodsAreBlocked(t *testing.T) {
	src := []byte(`public final class Native {
    public static native int square(int n);

    public static int twice(int n) { return n * 2; }
}
`)
	file, err := ParseSource(src, Java, "Native.java")
	if err != nil {
		t.Fatal(err)
	}
	var blocked, offerable int
	for _, fn := range file.Fns {
		if fn.Blocked != "" {
			blocked++
			if fn.Name != "square" {
				t.Errorf("%s is blocked, want square", fn.Name)
			}
		} else {
			offerable++
		}
	}
	if blocked != 1 || offerable != 1 {
		t.Errorf("blocked=%d offerable=%d, want 1/1", blocked, offerable)
	}
}

// `external fun` in Kotlin is implemented outside the crate, like Java's `native`: recording it answers
// UnsatisfiedLinkError, so it is a blocked export with that reason.
func TestKotlinExternalFunctionsAreBlocked(t *testing.T) {
	src := []byte("external fun square(n: Int): Int\n\nfun twice(n: Int): Int = n * 2\n")
	file, err := ParseSource(src, Kotlin, "M.kt")
	if err != nil {
		t.Fatal(err)
	}
	var blocked, offerable int
	for _, fn := range file.Fns {
		if fn.Blocked != "" {
			blocked++
			if fn.Name != "square" {
				t.Errorf("%s is blocked, want square", fn.Name)
			}
		} else {
			offerable++
		}
	}
	if blocked != 1 || offerable != 1 {
		t.Errorf("blocked=%d offerable=%d, want 1/1", blocked, offerable)
	}
}

// C# spells "implemented elsewhere" as `extern` plus a DllImport; recording one answers an EntryPointNotFoundException.
func TestCSharpExternMethodsAreBlocked(t *testing.T) {
	src := []byte(`public static class N
{
    public static extern int Square(int n);

    public static int Twice(int n) { return n * 2; }
}
`)
	file, err := ParseSource(src, CSharp, "N.cs")
	if err != nil {
		t.Fatal(err)
	}
	var blocked, offerable int
	for _, fn := range file.Fns {
		if fn.Blocked != "" {
			blocked++
			if fn.Name != "Square" {
				t.Errorf("%s is blocked, want Square", fn.Name)
			}
		} else {
			offerable++
		}
	}
	if blocked != 1 || offerable != 1 {
		t.Errorf("blocked=%d offerable=%d, want 1/1", blocked, offerable)
	}
}
