package grid

import (
	"math/bits"
	"slices"
)

// Mask is a fixed-size bitmask over grid slots: bit i is slot i.
type Mask []uint64

// NewMask returns a zeroed mask over slots slots.
func NewMask(slots int) Mask {
	return make(Mask, (slots+63)/64)
}

// Set turns bit i on.
func (m Mask) Set(i int) { m[i/64] |= 1 << (i % 64) }

// Has reports whether bit i is on.
func (m Mask) Has(i int) bool {
	if i < 0 || i/64 >= len(m) {
		return false
	}
	return m[i/64]&(1<<(i%64)) != 0
}

// Clone returns an independent copy of m.
func (m Mask) Clone() Mask { return slices.Clone(m) }

// Count returns the number of set bits.
func (m Mask) Count() int {
	n := 0
	for _, w := range m {
		n += bits.OnesCount64(w)
	}
	return n
}

// Or returns the union of m and other.
func (m Mask) Or(other Mask) Mask {
	out := make(Mask, len(m))
	for i := range m {
		out[i] = m[i] | other[i]
	}
	return out
}

// And returns the intersection of m and other.
func (m Mask) And(other Mask) Mask {
	out := make(Mask, len(m))
	for i := range m {
		out[i] = m[i] & other[i]
	}
	return out
}

// AndNot returns m with other's set bits cleared.
func (m Mask) AndNot(other Mask) Mask {
	out := make(Mask, len(m))
	for i := range m {
		out[i] = m[i] &^ other[i]
	}
	return out
}

// OrInto sets every bit of other in m. Both masks must be of equal length.
func (m Mask) OrInto(other Mask) {
	for i := range m {
		m[i] |= other[i]
	}
}

// Run is a run of consecutive set slots.
type Run struct {
	Start int // grid slot index of the first slot
	Slots int // length in slots
}

// Runs returns the runs of consecutive set slots in m, bounded by slots.
func (m Mask) Runs(slots int) []Run {
	var runs []Run
	start := -1
	for i := 0; i <= slots; i++ {
		switch set := i < slots && m.Has(i); {
		case set && start < 0:
			start = i
		case !set && start >= 0:
			runs = append(runs, Run{Start: start, Slots: i - start})
			start = -1
		}
	}
	return runs
}
