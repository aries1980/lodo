package set

import (
	"maps"
	"testing"
)

func TestSet(t *testing.T) {
	s := New("first", "second", "first")
	if len(s) != 2 || !s.Has("first") || !s.Has("second") || s.Has("missing") {
		t.Fatalf("unexpected membership: %v", s)
	}
	if s.Add("first") || !s.Add("third") || !s.Has("third") {
		t.Fatalf("unexpected insertion: %v", s)
	}
	delete(s, "first")
	if s.Has("first") {
		t.Fatal("deleted value still present")
	}
	clone := maps.Clone(s)
	clone.Add("fourth")
	if s.Has("fourth") || !clone.Has("fourth") {
		t.Fatal("clone must have independent membership")
	}
}

func TestNilSet(t *testing.T) {
	var s Set[int]
	if s.Has(0) || len(s) != 0 {
		t.Fatal("nil set must be empty")
	}
}

func TestEmptySet(t *testing.T) {
	s := New[int]()
	if !s.Add(0) || !s.Has(0) || s.Add(0) {
		t.Fatalf("unexpected zero-value membership: %v", s)
	}
}
