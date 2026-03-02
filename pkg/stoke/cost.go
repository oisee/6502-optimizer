package stoke

import (
	"github.com/oisee/6502-optimizer/pkg/cpu"
	"github.com/oisee/6502-optimizer/pkg/inst"
)

// testVectors are fixed inputs for quick equivalence checking.
// Same as search.TestVectors — duplicated here to avoid import cycle.
var testVectors = []cpu.State{
	{A: 0x00, X: 0x00, Y: 0x00, P: 0x00, S: 0xFD, M: 0x42, S0: 0x00, S1: 0x00},
	{A: 0xFF, X: 0xFF, Y: 0xFF, P: 0xFF, S: 0xFF, M: 0xBD, S0: 0xFF, S1: 0xFF},
	{A: 0x01, X: 0x02, Y: 0x03, P: 0x00, S: 0xFD, M: 0x13, S0: 0x10, S1: 0x20},
	{A: 0x80, X: 0x40, Y: 0x20, P: 0x01, S: 0xFC, M: 0x7E, S0: 0x30, S1: 0x40},
	{A: 0x55, X: 0xAA, Y: 0x55, P: 0x00, S: 0xFE, M: 0xA5, S0: 0x55, S1: 0xAA},
	{A: 0xAA, X: 0x55, Y: 0xAA, P: 0x01, S: 0xFD, M: 0x5A, S0: 0xAA, S1: 0x55},
	{A: 0x0F, X: 0xF0, Y: 0x0F, P: 0x00, S: 0xFB, M: 0xE1, S0: 0x0F, S1: 0xF0},
	{A: 0x7F, X: 0x80, Y: 0x7F, P: 0x01, S: 0xFD, M: 0x33, S0: 0x80, S1: 0x7F},
}

// execSeq runs a sequence of instructions on a state.
func execSeq(initial cpu.State, seq []inst.Instruction) cpu.State {
	s := initial
	for i := range seq {
		cpu.Exec(&s, seq[i].Op, seq[i].Imm)
	}
	return s
}

// Cost evaluates how far a candidate is from matching the target.
// Returns: 1000 * mismatches + byteSize(candidate) + cycles(candidate)/100
func Cost(target, candidate []inst.Instruction) int {
	mismatches := 0
	for i := range testVectors {
		tOut := execSeq(testVectors[i], target)
		cOut := execSeq(testVectors[i], candidate)
		if !tOut.Equal(cOut) {
			mismatches++
		}
	}
	return 1000*mismatches + inst.SeqByteSize(candidate) + inst.SeqCycles(candidate)/100
}

// Mismatches returns only the mismatch count on test vectors.
func Mismatches(target, candidate []inst.Instruction) int {
	mismatches := 0
	for i := range testVectors {
		tOut := execSeq(testVectors[i], target)
		cOut := execSeq(testVectors[i], candidate)
		if !tOut.Equal(cOut) {
			mismatches++
		}
	}
	return mismatches
}

// CostMasked evaluates cost, ignoring dead flag bits in comparisons.
func CostMasked(target, candidate []inst.Instruction, deadFlags uint8) int {
	if deadFlags == 0 {
		return Cost(target, candidate)
	}
	mismatches := MismatchesMasked(target, candidate, deadFlags)
	return 1000*mismatches + inst.SeqByteSize(candidate) + inst.SeqCycles(candidate)/100
}

// MismatchesMasked returns the mismatch count, ignoring dead flag bits.
func MismatchesMasked(target, candidate []inst.Instruction, deadFlags uint8) int {
	mismatches := 0
	pMask := cpu.PMask &^ deadFlags
	for i := range testVectors {
		tOut := execSeq(testVectors[i], target)
		cOut := execSeq(testVectors[i], candidate)
		if tOut.A != cOut.A || tOut.X != cOut.X || tOut.Y != cOut.Y ||
			(tOut.P&pMask) != (cOut.P&pMask) ||
			tOut.S != cOut.S || tOut.M != cOut.M ||
			tOut.S0 != cOut.S0 || tOut.S1 != cOut.S1 {
			mismatches++
		}
	}
	return mismatches
}
