package cpu

// State represents the 6502 register state relevant to the superoptimizer.
// 8 bytes, fits in a single cache line, cheap to copy by value.
//
//   - A, X, Y: accumulator and index registers
//   - P: processor status (NV-BDIZC), bits 4(B) and 5(unused) masked on compare
//   - S: stack pointer (points into page $01)
//   - M: virtual zero-page memory byte (all zp ops share this)
//   - S0, S1: virtual stack slots (for PHA/PLA/PHP/PLP)
type State struct {
	A, X, Y, P uint8
	S          uint8 // stack pointer
	M          uint8 // virtual zero-page memory byte
	S0, S1     uint8 // virtual stack slots
}

// PMask masks out bit 4 (B flag) and bit 5 (unused) for comparison.
// On the real 6502 these bits are artifacts of how P is pushed to stack.
const PMask = uint8(0xCF) // 11001111 — keep N,V,D,I,Z,C

// Equal returns true if two states are identical (masking P bits 4-5).
func (s State) Equal(o State) bool {
	return s.A == o.A && s.X == o.X && s.Y == o.Y &&
		(s.P&PMask) == (o.P&PMask) &&
		s.S == o.S && s.M == o.M &&
		s.S0 == o.S0 && s.S1 == o.S1
}
