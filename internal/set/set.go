// Package set provides membership sets backed by Go maps.
package set

type Set[T comparable] map[T]struct{}

func New[T comparable](values ...T) Set[T] {
	out := make(Set[T], len(values))
	for _, value := range values {
		out.Add(value)
	}
	return out
}

func (s Set[T]) Has(value T) bool {
	_, ok := s[value]
	return ok
}

// Add inserts value and reports whether it was absent. Initialize s with New or make.
func (s Set[T]) Add(value T) bool {
	if s.Has(value) {
		return false
	}
	s[value] = struct{}{}
	return true
}
