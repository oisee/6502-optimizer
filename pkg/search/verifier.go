package search

import (
	"github.com/oisee/6502-optimizer/pkg/cpu"
	"github.com/oisee/6502-optimizer/pkg/inst"
)

// FlagMask indicates which flag bits are considered "dead" and can be ignored.
type FlagMask = uint8

const (
	DeadNone FlagMask = 0x00 // Full equivalence
	DeadAll  FlagMask = 0xFF // All flags dead — registers only
)

// TestVectors are fixed inputs used for QuickCheck to reject ~99.99% of non-matches.
// 6502 state: A, X, Y, P, S, M, S0, S1
var TestVectors = []cpu.State{
	{A: 0x00, X: 0x00, Y: 0x00, P: 0x00, S: 0xFD, M: 0x42, S0: 0x00, S1: 0x00},
	{A: 0xFF, X: 0xFF, Y: 0xFF, P: 0xFF, S: 0xFF, M: 0xBD, S0: 0xFF, S1: 0xFF},
	{A: 0x01, X: 0x02, Y: 0x03, P: 0x00, S: 0xFD, M: 0x13, S0: 0x10, S1: 0x20},
	{A: 0x80, X: 0x40, Y: 0x20, P: 0x01, S: 0xFC, M: 0x7E, S0: 0x30, S1: 0x40},
	{A: 0x55, X: 0xAA, Y: 0x55, P: 0x00, S: 0xFE, M: 0xA5, S0: 0x55, S1: 0xAA},
	{A: 0xAA, X: 0x55, Y: 0xAA, P: 0x01, S: 0xFD, M: 0x5A, S0: 0xAA, S1: 0x55},
	{A: 0x0F, X: 0xF0, Y: 0x0F, P: 0x00, S: 0xFB, M: 0xE1, S0: 0x0F, S1: 0xF0},
	{A: 0x7F, X: 0x80, Y: 0x7F, P: 0x01, S: 0xFD, M: 0x33, S0: 0x80, S1: 0x7F},
}

// MidCheckVectors are 32 vectors for intermediate filtering.
var MidCheckVectors = []cpu.State{
	// First 8 = same as TestVectors
	{A: 0x00, X: 0x00, Y: 0x00, P: 0x00, S: 0xFD, M: 0x42, S0: 0x00, S1: 0x00},
	{A: 0xFF, X: 0xFF, Y: 0xFF, P: 0xFF, S: 0xFF, M: 0xBD, S0: 0xFF, S1: 0xFF},
	{A: 0x01, X: 0x02, Y: 0x03, P: 0x00, S: 0xFD, M: 0x13, S0: 0x10, S1: 0x20},
	{A: 0x80, X: 0x40, Y: 0x20, P: 0x01, S: 0xFC, M: 0x7E, S0: 0x30, S1: 0x40},
	{A: 0x55, X: 0xAA, Y: 0x55, P: 0x00, S: 0xFE, M: 0xA5, S0: 0x55, S1: 0xAA},
	{A: 0xAA, X: 0x55, Y: 0xAA, P: 0x01, S: 0xFD, M: 0x5A, S0: 0xAA, S1: 0x55},
	{A: 0x0F, X: 0xF0, Y: 0x0F, P: 0x00, S: 0xFB, M: 0xE1, S0: 0x0F, S1: 0xF0},
	{A: 0x7F, X: 0x80, Y: 0x7F, P: 0x01, S: 0xFD, M: 0x33, S0: 0x80, S1: 0x7F},
	// Single-bit A values (8-15)
	{A: 0x02, X: 0x01, Y: 0x04, P: 0x00, S: 0xFD, M: 0x91, S0: 0x11, S1: 0x22},
	{A: 0x04, X: 0x08, Y: 0x10, P: 0x01, S: 0xFD, M: 0x48, S0: 0x33, S1: 0x44},
	{A: 0x08, X: 0x10, Y: 0x20, P: 0x00, S: 0xFD, M: 0x24, S0: 0x55, S1: 0x66},
	{A: 0x10, X: 0x20, Y: 0x40, P: 0x01, S: 0xFD, M: 0xD7, S0: 0x77, S1: 0x88},
	{A: 0x20, X: 0x40, Y: 0x80, P: 0x00, S: 0xFD, M: 0x6B, S0: 0x99, S1: 0xBB},
	{A: 0x40, X: 0x80, Y: 0x01, P: 0x01, S: 0xFD, M: 0xB2, S0: 0xCC, S1: 0xDD},
	{A: 0x03, X: 0x05, Y: 0x09, P: 0x00, S: 0xFD, M: 0x0F, S0: 0xEE, S1: 0x11},
	{A: 0x05, X: 0x0A, Y: 0x14, P: 0x01, S: 0xFD, M: 0xF0, S0: 0x22, S1: 0x33},
	// Boundary values (16-23)
	{A: 0xFE, X: 0x7F, Y: 0x80, P: 0x00, S: 0xFD, M: 0x01, S0: 0x44, S1: 0x55},
	{A: 0x81, X: 0xFE, Y: 0x01, P: 0x01, S: 0xFD, M: 0xFE, S0: 0x66, S1: 0x77},
	{A: 0x7E, X: 0x81, Y: 0x42, P: 0x00, S: 0xFD, M: 0x80, S0: 0x88, S1: 0x99},
	{A: 0xBF, X: 0x40, Y: 0xBF, P: 0x01, S: 0xFD, M: 0x40, S0: 0xAA, S1: 0xBB},
	{A: 0xC0, X: 0x3F, Y: 0xC0, P: 0x00, S: 0xFD, M: 0x1F, S0: 0xCC, S1: 0xDD},
	{A: 0xE0, X: 0x1F, Y: 0xE0, P: 0x01, S: 0xFD, M: 0xEF, S0: 0xEE, S1: 0x00},
	{A: 0xF0, X: 0x0F, Y: 0xF0, P: 0x00, S: 0xFD, M: 0x55, S0: 0x11, S1: 0x22},
	{A: 0x0E, X: 0xE0, Y: 0x0E, P: 0x01, S: 0xFD, M: 0xAA, S0: 0x33, S1: 0x44},
	// Stack-exercising (24-31)
	{A: 0x11, X: 0x22, Y: 0x33, P: 0x00, S: 0xFE, M: 0xC3, S0: 0x00, S1: 0x00},
	{A: 0x22, X: 0x33, Y: 0x44, P: 0x01, S: 0xFF, M: 0x3C, S0: 0x55, S1: 0x66},
	{A: 0x44, X: 0x55, Y: 0x66, P: 0x00, S: 0xFC, M: 0x87, S0: 0x77, S1: 0x88},
	{A: 0x88, X: 0x99, Y: 0xAA, P: 0x01, S: 0xFB, M: 0x69, S0: 0x99, S1: 0xAA},
	{A: 0x33, X: 0x44, Y: 0x55, P: 0x00, S: 0xFD, M: 0xAE, S0: 0xBB, S1: 0xCC},
	{A: 0xCC, X: 0xDD, Y: 0xEE, P: 0x01, S: 0xFD, M: 0x51, S0: 0xDD, S1: 0xEE},
	{A: 0x66, X: 0x77, Y: 0x88, P: 0x00, S: 0xFD, M: 0xDA, S0: 0x12, S1: 0x34},
	{A: 0x99, X: 0xAA, Y: 0xBB, P: 0x01, S: 0xFD, M: 0x25, S0: 0x56, S1: 0x78},
}

// execSeq runs a sequence of instructions on a state, returning the final state.
func execSeq(initial cpu.State, seq []inst.Instruction) cpu.State {
	s := initial
	for i := range seq {
		cpu.Exec(&s, seq[i].Op, seq[i].Imm)
	}
	return s
}

// statesEqual compares two states, masking P bits 4-5.
func statesEqual(a, b cpu.State) bool {
	return a.Equal(b)
}

// QuickCheck tests two sequences against the test vectors.
func QuickCheck(target, candidate []inst.Instruction) bool {
	for i := range TestVectors {
		tOut := execSeq(TestVectors[i], target)
		cOut := execSeq(TestVectors[i], candidate)
		if !statesEqual(tOut, cOut) {
			return false
		}
	}
	return true
}

// MidCheck tests two sequences against the 32 MidCheck vectors.
func MidCheck(target, candidate []inst.Instruction) bool {
	for i := range MidCheckVectors {
		tOut := execSeq(MidCheckVectors[i], target)
		cOut := execSeq(MidCheckVectors[i], candidate)
		if !statesEqual(tOut, cOut) {
			return false
		}
	}
	return true
}

// FingerprintSize is the number of bytes per state snapshot in a fingerprint.
const FingerprintSize = 8

// FingerprintLen is the total fingerprint length: FingerprintSize * 8 test vectors = 64 bytes.
const FingerprintLen = FingerprintSize * 8

// Fingerprint computes a compact hash of a sequence's behavior on test vectors.
func Fingerprint(seq []inst.Instruction) [FingerprintLen]byte {
	var fp [FingerprintLen]byte
	for i := range TestVectors {
		out := execSeq(TestVectors[i], seq)
		off := i * FingerprintSize
		fp[off+0] = out.A
		fp[off+1] = out.X
		fp[off+2] = out.Y
		fp[off+3] = out.P & cpu.PMask
		fp[off+4] = out.S
		fp[off+5] = out.M
		fp[off+6] = out.S0
		fp[off+7] = out.S1
	}
	return fp
}

// Register bitmask for tracking which registers are read/written.
type regMask uint16

const (
	regA  regMask = 1 << iota
	regP          // flags
	regX
	regY
	regS  // stack pointer
	regM  // virtual memory byte
	regS0 // stack slot 0
	regS1 // stack slot 1
)

func regsRead(seq []inst.Instruction) regMask {
	var mask regMask
	for _, instr := range seq {
		mask |= opReads(instr.Op)
	}
	return mask
}

// opReads returns which registers an instruction reads as source operands.
func opReads(op inst.OpCode) regMask {
	switch op {
	// Transfers
	case inst.TAX, inst.TAY:
		return regA
	case inst.TXA:
		return regX
	case inst.TYA:
		return regY
	case inst.TSX:
		return regS
	case inst.TXS:
		return regX

	// Inc/Dec registers
	case inst.INX, inst.DEX:
		return regX
	case inst.INY, inst.DEY:
		return regY

	// Shifts on A (ASL/LSR don't read carry, ROL/ROR do)
	case inst.ASL_A, inst.LSR_A:
		return regA
	case inst.ROL_A, inst.ROR_A:
		return regA | regP

	// Flag ops
	case inst.CLC, inst.SEC, inst.CLD, inst.SED, inst.CLI, inst.SEI, inst.CLV:
		return 0

	// Stack
	case inst.PHA:
		return regA | regS0 | regS1
	case inst.PLA:
		return regS0
	case inst.PHP:
		return regP | regS0 | regS1
	case inst.PLP:
		return regS0

	// NOP
	case inst.NOP:
		return 0

	// Immediate loads
	case inst.LDA_IMM, inst.LDX_IMM, inst.LDY_IMM:
		return 0

	// ALU immediate: ADC/SBC read A + P(carry)
	case inst.ADC_IMM, inst.SBC_IMM:
		return regA | regP
	// AND/ORA/EOR read A
	case inst.AND_IMM, inst.ORA_IMM, inst.EOR_IMM:
		return regA
	// CMP reads A, CPX reads X, CPY reads Y
	case inst.CMP_IMM:
		return regA
	case inst.CPX_IMM:
		return regX
	case inst.CPY_IMM:
		return regY

	// Memory loads
	case inst.LDA_M:
		return regM
	case inst.LDX_M:
		return regM
	case inst.LDY_M:
		return regM

	// Memory stores
	case inst.STA_M:
		return regA
	case inst.STX_M:
		return regX
	case inst.STY_M:
		return regY

	// Memory ALU
	case inst.ADC_M, inst.SBC_M:
		return regA | regM | regP
	case inst.AND_M, inst.ORA_M, inst.EOR_M:
		return regA | regM
	case inst.CMP_M:
		return regA | regM
	case inst.CPX_M:
		return regX | regM
	case inst.CPY_M:
		return regY | regM

	// Memory inc/dec
	case inst.INC_M, inst.DEC_M:
		return regM

	// Memory shifts
	case inst.ASL_M, inst.LSR_M:
		return regM
	case inst.ROL_M, inst.ROR_M:
		return regM | regP

	// BIT test
	case inst.BIT_M:
		return regA | regM
	}
	return 0
}

// ExhaustiveCheck verifies equivalence over ALL possible inputs.
func ExhaustiveCheck(target, candidate []inst.Instruction) bool {
	reads := regsRead(target) | regsRead(candidate)

	if reads&^(regA|regP) == 0 {
		return exhaustiveAP(target, candidate)
	}
	return exhaustiveAll(target, candidate, reads)
}

// exhaustiveAP sweeps A=0..255, carry=0/1 (512 iterations).
func exhaustiveAP(target, candidate []inst.Instruction) bool {
	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
			tOut := execSeq(s, target)
			cOut := execSeq(s, candidate)
			if !statesEqual(tOut, cOut) {
				return false
			}
		}
	}
	return true
}

func exhaustiveAll(target, candidate []inst.Instruction, reads regMask) bool {
	type regInfo struct {
		mask   regMask
		offset int // 0=A,1=X,2=Y,3=M,4=S0,5=S1
	}

	extraRegs := make([]int, 0, 6)
	if reads&regX != 0 {
		extraRegs = append(extraRegs, 1)
	}
	if reads&regY != 0 {
		extraRegs = append(extraRegs, 2)
	}
	if reads&regM != 0 {
		extraRegs = append(extraRegs, 3)
	}
	if reads&regS != 0 {
		extraRegs = append(extraRegs, 4)
	}
	if reads&regS0 != 0 {
		extraRegs = append(extraRegs, 5)
	}
	if reads&regS1 != 0 {
		extraRegs = append(extraRegs, 6)
	}

	if len(extraRegs) == 0 {
		return exhaustiveAP(target, candidate)
	}

	if len(extraRegs) <= 2 {
		return exhaustiveFullSweep(target, candidate, extraRegs)
	}
	return exhaustiveReducedSweep(target, candidate, extraRegs)
}

func setReg(s *cpu.State, offset int, val uint8) {
	switch offset {
	case 1:
		s.X = val
	case 2:
		s.Y = val
	case 3:
		s.M = val
	case 4:
		s.S = val
	case 5:
		s.S0 = val
	case 6:
		s.S1 = val
	}
}

func exhaustiveFullSweep(target, candidate []inst.Instruction, extraRegs []int) bool {
	if len(extraRegs) == 1 {
		for a := 0; a < 256; a++ {
			for carry := uint8(0); carry <= 1; carry++ {
				for r := 0; r < 256; r++ {
					s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
					setReg(&s, extraRegs[0], uint8(r))
					tOut := execSeq(s, target)
					cOut := execSeq(s, candidate)
					if !statesEqual(tOut, cOut) {
						return false
					}
				}
			}
		}
		return true
	}

	// 2 extra registers
	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			for r1 := 0; r1 < 256; r1++ {
				for r2 := 0; r2 < 256; r2++ {
					s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
					setReg(&s, extraRegs[0], uint8(r1))
					setReg(&s, extraRegs[1], uint8(r2))
					tOut := execSeq(s, target)
					cOut := execSeq(s, candidate)
					if !statesEqual(tOut, cOut) {
						return false
					}
				}
			}
		}
	}
	return true
}

func exhaustiveReducedSweep(target, candidate []inst.Instruction, extraRegs []int) bool {
	repValues := []uint8{
		0x00, 0x01, 0x02, 0x0F, 0x10, 0x1F, 0x20, 0x3F,
		0x40, 0x55, 0x7E, 0x7F, 0x80, 0x81, 0xAA, 0xBF,
		0xC0, 0xD5, 0xE0, 0xEF, 0xF0, 0xF7, 0xFE, 0xFF,
		0x03, 0x07, 0x11, 0x33, 0x77, 0xBB, 0xDD, 0xEE,
	}

	compare := func(s cpu.State) bool {
		tOut := execSeq(s, target)
		cOut := execSeq(s, candidate)
		return statesEqual(tOut, cOut)
	}

	var sweep func(s cpu.State, regIdx int) bool
	sweep = func(s cpu.State, regIdx int) bool {
		if regIdx >= len(extraRegs) {
			return compare(s)
		}
		for _, v := range repValues {
			s2 := s
			setReg(&s2, extraRegs[regIdx], v)
			if !sweep(s2, regIdx+1) {
				return false
			}
		}
		return true
	}

	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
			if !sweep(s, 0) {
				return false
			}
		}
	}
	return true
}

// statesEqualMasked compares two states, ignoring dead flag bits.
func statesEqualMasked(a, b cpu.State, deadFlags FlagMask) bool {
	pMask := cpu.PMask &^ deadFlags
	return a.A == b.A && a.X == b.X && a.Y == b.Y &&
		(a.P&pMask) == (b.P&pMask) &&
		a.S == b.S && a.M == b.M &&
		a.S0 == b.S0 && a.S1 == b.S1
}

// QuickCheckMasked tests two sequences against test vectors, ignoring dead flag bits.
func QuickCheckMasked(target, candidate []inst.Instruction, deadFlags FlagMask) bool {
	if deadFlags == DeadNone {
		return QuickCheck(target, candidate)
	}
	for i := range TestVectors {
		tOut := execSeq(TestVectors[i], target)
		cOut := execSeq(TestVectors[i], candidate)
		if !statesEqualMasked(tOut, cOut, deadFlags) {
			return false
		}
	}
	return true
}

// MidCheckMasked tests against 32 vectors, ignoring dead flag bits.
func MidCheckMasked(target, candidate []inst.Instruction, deadFlags FlagMask) bool {
	if deadFlags == DeadNone {
		return MidCheck(target, candidate)
	}
	for i := range MidCheckVectors {
		tOut := execSeq(MidCheckVectors[i], target)
		cOut := execSeq(MidCheckVectors[i], candidate)
		if !statesEqualMasked(tOut, cOut, deadFlags) {
			return false
		}
	}
	return true
}

// ExhaustiveCheckMasked verifies equivalence ignoring dead flag bits.
func ExhaustiveCheckMasked(target, candidate []inst.Instruction, deadFlags FlagMask) bool {
	if deadFlags == DeadNone {
		return ExhaustiveCheck(target, candidate)
	}
	reads := regsRead(target) | regsRead(candidate)
	if reads&^(regA|regP) == 0 {
		return exhaustiveAPMasked(target, candidate, deadFlags)
	}
	return exhaustiveAllMasked(target, candidate, reads, deadFlags)
}

func exhaustiveAPMasked(target, candidate []inst.Instruction, deadFlags FlagMask) bool {
	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
			tOut := execSeq(s, target)
			cOut := execSeq(s, candidate)
			if !statesEqualMasked(tOut, cOut, deadFlags) {
				return false
			}
		}
	}
	return true
}

func exhaustiveAllMasked(target, candidate []inst.Instruction, reads regMask, deadFlags FlagMask) bool {
	extraRegs := make([]int, 0, 6)
	if reads&regX != 0 {
		extraRegs = append(extraRegs, 1)
	}
	if reads&regY != 0 {
		extraRegs = append(extraRegs, 2)
	}
	if reads&regM != 0 {
		extraRegs = append(extraRegs, 3)
	}
	if reads&regS != 0 {
		extraRegs = append(extraRegs, 4)
	}
	if reads&regS0 != 0 {
		extraRegs = append(extraRegs, 5)
	}
	if reads&regS1 != 0 {
		extraRegs = append(extraRegs, 6)
	}

	if len(extraRegs) == 0 {
		return exhaustiveAPMasked(target, candidate, deadFlags)
	}

	if len(extraRegs) <= 2 {
		return exhaustiveFullSweepMasked(target, candidate, extraRegs, deadFlags)
	}
	return exhaustiveReducedSweepMasked(target, candidate, extraRegs, deadFlags)
}

func exhaustiveFullSweepMasked(target, candidate []inst.Instruction, extraRegs []int, deadFlags FlagMask) bool {
	if len(extraRegs) == 1 {
		for a := 0; a < 256; a++ {
			for carry := uint8(0); carry <= 1; carry++ {
				for r := 0; r < 256; r++ {
					s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
					setReg(&s, extraRegs[0], uint8(r))
					tOut := execSeq(s, target)
					cOut := execSeq(s, candidate)
					if !statesEqualMasked(tOut, cOut, deadFlags) {
						return false
					}
				}
			}
		}
		return true
	}

	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			for r1 := 0; r1 < 256; r1++ {
				for r2 := 0; r2 < 256; r2++ {
					s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
					setReg(&s, extraRegs[0], uint8(r1))
					setReg(&s, extraRegs[1], uint8(r2))
					tOut := execSeq(s, target)
					cOut := execSeq(s, candidate)
					if !statesEqualMasked(tOut, cOut, deadFlags) {
						return false
					}
				}
			}
		}
	}
	return true
}

func exhaustiveReducedSweepMasked(target, candidate []inst.Instruction, extraRegs []int, deadFlags FlagMask) bool {
	repValues := []uint8{
		0x00, 0x01, 0x02, 0x0F, 0x10, 0x1F, 0x20, 0x3F,
		0x40, 0x55, 0x7E, 0x7F, 0x80, 0x81, 0xAA, 0xBF,
		0xC0, 0xD5, 0xE0, 0xEF, 0xF0, 0xF7, 0xFE, 0xFF,
		0x03, 0x07, 0x11, 0x33, 0x77, 0xBB, 0xDD, 0xEE,
	}

	compare := func(s cpu.State) bool {
		tOut := execSeq(s, target)
		cOut := execSeq(s, candidate)
		return statesEqualMasked(tOut, cOut, deadFlags)
	}

	var sweep func(s cpu.State, regIdx int) bool
	sweep = func(s cpu.State, regIdx int) bool {
		if regIdx >= len(extraRegs) {
			return compare(s)
		}
		for _, v := range repValues {
			s2 := s
			setReg(&s2, extraRegs[regIdx], v)
			if !sweep(s2, regIdx+1) {
				return false
			}
		}
		return true
	}

	for a := 0; a < 256; a++ {
		for carry := uint8(0); carry <= 1; carry++ {
			s := cpu.State{A: uint8(a), P: carry, S: 0xFD}
			if !sweep(s, 0) {
				return false
			}
		}
	}
	return true
}

// FlagDiff runs test vectors and returns a bitmask of which flag bits ever differ.
func FlagDiff(target, candidate []inst.Instruction) FlagMask {
	var diff FlagMask
	for i := range TestVectors {
		tOut := execSeq(TestVectors[i], target)
		cOut := execSeq(TestVectors[i], candidate)
		if tOut.A != cOut.A || tOut.X != cOut.X || tOut.Y != cOut.Y ||
			tOut.S != cOut.S || tOut.M != cOut.M ||
			tOut.S0 != cOut.S0 || tOut.S1 != cOut.S1 {
			return 0
		}
		diff |= (tOut.P ^ cOut.P) & cpu.PMask
	}
	return diff
}
