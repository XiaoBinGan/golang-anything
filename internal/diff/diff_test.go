package diff

import (
	"math"
	"testing"

	"github.com/XiaoBinGan/golang-anything/internal/cases"
)

func TestSameIgnoresKeyOrderAndSmallFloatDiff(t *testing.T) {
	want := cases.Case{Fn: "f", Ok: map[string]any{
		"a": float64(1),
		"b": []any{float64(2), float64(3)},
	}}
	got := cases.Case{Fn: "f", Ok: map[string]any{
		"b": []any{float64(2), float64(3.0000000001)},
		"a": float64(1),
	}}
	if why := same(want, got, 1e-9); why != "" {
		t.Errorf("key order and a tiny float cost a pass: %s", why)
	}
	// Same but far apart: reported, and at the key that differs.
	got.Ok.(map[string]any)["b"] = []any{float64(2), float64(9)}
	if why := same(want, got, 1e-9); why == "" {
		t.Error("a real difference passed")
	}
}

func TestSameComparesErrorKinds(t *testing.T) {
	want := cases.Case{Fn: "f", Err: "RangeError: whole must not be zero"}
	if why := same(want, cases.Case{Fn: "f", Err: "RangeError: other text"}, 0); why != "" {
		t.Errorf("same kind, different text: %s", why)
	}
	if why := same(want, cases.Case{Fn: "f", Err: "TypeError: x"}, 0); why == "" {
		t.Error("a different error kind passed")
	}
	// A Go panic stands in for the original's exception, and must name the same kind.
	if why := same(want, cases.Case{Fn: "f", Panic: "RangeError: zero"}, 0); why != "" {
		t.Errorf("panic with the right kind: %s", why)
	}
	if why := same(want, cases.Case{Fn: "f", Panic: "index out of range"}, 0); why == "" {
		t.Error("a panic with the wrong kind passed")
	}
}

func TestSameTreatsNaNAsAgreement(t *testing.T) {
	want := cases.Case{Fn: "f", Ok: map[string]any{"$number": "NaN"}}
	got := cases.Case{Fn: "f", Ok: map[string]any{"$number": "NaN"}}
	if why := same(want, got, 1e-9); why != "" {
		t.Errorf("NaN should match itself: %s", why)
	}
	_ = math.NaN()
}
