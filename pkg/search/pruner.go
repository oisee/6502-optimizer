package search

import "github.com/oisee/6502-optimizer/pkg/inst"

// ShouldPrune returns true if the sequence can be skipped.
func ShouldPrune(seq []inst.Instruction) bool {
	for i := 0; i < len(seq); i++ {
		// NOP elimination
		if seq[i].Op == inst.NOP {
			return true
		}

		// Dead write: instruction at i writes a register that is
		// immediately overwritten at i+1 without being read
		if i+1 < len(seq) && isDeadWrite(seq[i], seq[i+1]) {
			return true
		}
	}

	// Stack depth check: reject sequences with underflow or overflow
	if hasStackViolation(seq) {
		return true
	}

	// Canonical ordering: for independent adjacent instructions,
	// force opcode order to eliminate permutation duplicates
	for i := 0; i+1 < len(seq); i++ {
		if areIndependent(seq[i], seq[i+1]) && instKey(seq[i]) > instKey(seq[i+1]) {
			return true
		}
	}

	return false
}

// isDeadWrite returns true if 'first' writes a register that 'second'
// overwrites without reading first.
func isDeadWrite(first, second inst.Instruction) bool {
	written := opWrites(first.Op)
	if written == 0 {
		return false
	}
	read := opReads(second.Op)
	written2 := opWrites(second.Op)

	// For each register written by first:
	// if second writes it too AND doesn't read it, it's dead
	// Exclude flags and stack-related registers (S, S0, S1) from dead write
	// detection — stack ops have implicit dependencies through push/pull ordering.
	dead := written & written2 & ^regP & ^regS & ^regS0 & ^regS1 & ^(read)
	return dead != 0
}

// hasStackViolation returns true if the sequence has stack underflow
// (PLA on empty) or overflow (3+ pushes without matching pulls).
func hasStackViolation(seq []inst.Instruction) bool {
	depth := 0
	for _, instr := range seq {
		switch instr.Op {
		case inst.PHA, inst.PHP:
			depth++
			if depth > 2 {
				return true // overflow: more than 2 deep
			}
		case inst.PLA, inst.PLP:
			if depth <= 0 {
				return true // underflow
			}
			depth--
		}
	}
	return false
}

// opWrites returns which registers an instruction modifies.
func opWrites(op inst.OpCode) regMask {
	switch op {
	// Transfers
	case inst.TAX:
		return regX | regP
	case inst.TAY:
		return regY | regP
	case inst.TXA:
		return regA | regP
	case inst.TYA:
		return regA | regP
	case inst.TSX:
		return regX | regP
	case inst.TXS:
		return regS // no flags!

	// Inc/Dec
	case inst.INX, inst.DEX:
		return regX | regP
	case inst.INY, inst.DEY:
		return regY | regP

	// Shifts on A
	case inst.ASL_A, inst.LSR_A, inst.ROL_A, inst.ROR_A:
		return regA | regP

	// Flag ops
	case inst.CLC, inst.SEC, inst.CLD, inst.SED, inst.CLI, inst.SEI, inst.CLV:
		return regP

	// Stack
	case inst.PHA, inst.PHP:
		return regS | regS0 | regS1
	case inst.PLA:
		return regA | regP | regS | regS0
	case inst.PLP:
		return regP | regS | regS0

	// NOP
	case inst.NOP:
		return 0

	// Immediate loads
	case inst.LDA_IMM:
		return regA | regP
	case inst.LDX_IMM:
		return regX | regP
	case inst.LDY_IMM:
		return regY | regP

	// ALU immediate
	case inst.ADC_IMM, inst.SBC_IMM, inst.AND_IMM, inst.ORA_IMM, inst.EOR_IMM:
		return regA | regP
	case inst.CMP_IMM, inst.CPX_IMM, inst.CPY_IMM:
		return regP

	// Memory loads
	case inst.LDA_M:
		return regA | regP
	case inst.LDX_M:
		return regX | regP
	case inst.LDY_M:
		return regY | regP

	// Memory stores (no flags)
	case inst.STA_M, inst.STX_M, inst.STY_M:
		return regM

	// Memory ALU
	case inst.ADC_M, inst.SBC_M, inst.AND_M, inst.ORA_M, inst.EOR_M:
		return regA | regP
	case inst.CMP_M, inst.CPX_M, inst.CPY_M:
		return regP

	// Memory inc/dec
	case inst.INC_M, inst.DEC_M:
		return regM | regP

	// Memory shifts
	case inst.ASL_M, inst.LSR_M, inst.ROL_M, inst.ROR_M:
		return regM | regP

	// BIT
	case inst.BIT_M:
		return regP
	}
	return 0
}

// areIndependent returns true if swapping two instructions produces the same result.
func areIndependent(a, b inst.Instruction) bool {
	aR := opReads(a.Op)
	aW := opWrites(a.Op)
	bR := opReads(b.Op)
	bW := opWrites(b.Op)

	if aW&bR != 0 || aR&bW != 0 || aW&bW != 0 {
		return false
	}
	return true
}

// instKey returns a sortable key for canonical ordering.
func instKey(i inst.Instruction) uint32 {
	return uint32(i.Op)<<16 | uint32(i.Imm)
}
