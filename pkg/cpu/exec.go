package cpu

import "github.com/oisee/6502-optimizer/pkg/inst"

// Exec executes a single instruction on the given state.
// Returns the cycle cost. The state is modified in place.
// Assumes D=0 (binary mode) for ADC/SBC.
func Exec(s *State, op inst.OpCode, imm uint16) int {
	switch op {

	// === Register transfers ===
	case inst.TAX:
		s.X = s.A
		setNZ(s, s.X)
	case inst.TAY:
		s.Y = s.A
		setNZ(s, s.Y)
	case inst.TXA:
		s.A = s.X
		setNZ(s, s.A)
	case inst.TYA:
		s.A = s.Y
		setNZ(s, s.A)
	case inst.TSX:
		s.X = s.S
		setNZ(s, s.X)
	case inst.TXS:
		s.S = s.X
		// TXS does NOT affect flags

	// === Increment/decrement registers ===
	case inst.INX:
		s.X++
		setNZ(s, s.X)
	case inst.INY:
		s.Y++
		setNZ(s, s.Y)
	case inst.DEX:
		s.X--
		setNZ(s, s.X)
	case inst.DEY:
		s.Y--
		setNZ(s, s.Y)

	// === Shifts/rotates on accumulator ===
	case inst.ASL_A:
		execASL(s, &s.A)
	case inst.LSR_A:
		execLSR(s, &s.A)
	case inst.ROL_A:
		execROL(s, &s.A)
	case inst.ROR_A:
		execROR(s, &s.A)

	// === Flag operations ===
	case inst.CLC:
		s.P &^= FlagC
	case inst.SEC:
		s.P |= FlagC
	case inst.CLD:
		s.P &^= FlagD
	case inst.SED:
		s.P |= FlagD
	case inst.CLI:
		s.P &^= FlagI
	case inst.SEI:
		s.P |= FlagI
	case inst.CLV:
		s.P &^= FlagV

	// === Stack operations ===
	case inst.PHA:
		push(s, s.A)
	case inst.PLA:
		s.A = pull(s)
		setNZ(s, s.A)
	case inst.PHP:
		// PHP always pushes with B and U bits set
		push(s, s.P|FlagB|FlagU)
	case inst.PLP:
		s.P = pull(s)

	// === NOP ===
	case inst.NOP:
		// do nothing

	// === Immediate loads ===
	case inst.LDA_IMM:
		s.A = uint8(imm)
		setNZ(s, s.A)
	case inst.LDX_IMM:
		s.X = uint8(imm)
		setNZ(s, s.X)
	case inst.LDY_IMM:
		s.Y = uint8(imm)
		setNZ(s, s.Y)

	// === Immediate ALU ===
	case inst.ADC_IMM:
		execADC(s, uint8(imm))
	case inst.SBC_IMM:
		execSBC(s, uint8(imm))
	case inst.AND_IMM:
		s.A &= uint8(imm)
		setNZ(s, s.A)
	case inst.ORA_IMM:
		s.A |= uint8(imm)
		setNZ(s, s.A)
	case inst.EOR_IMM:
		s.A ^= uint8(imm)
		setNZ(s, s.A)

	// === Immediate compares ===
	case inst.CMP_IMM:
		execCMP(s, s.A, uint8(imm))
	case inst.CPX_IMM:
		execCMP(s, s.X, uint8(imm))
	case inst.CPY_IMM:
		execCMP(s, s.Y, uint8(imm))

	// === Memory loads (virtual M) ===
	case inst.LDA_M:
		s.A = s.M
		setNZ(s, s.A)
	case inst.LDX_M:
		s.X = s.M
		setNZ(s, s.X)
	case inst.LDY_M:
		s.Y = s.M
		setNZ(s, s.Y)

	// === Memory stores ===
	case inst.STA_M:
		s.M = s.A
	case inst.STX_M:
		s.M = s.X
	case inst.STY_M:
		s.M = s.Y

	// === Memory ALU ===
	case inst.ADC_M:
		execADC(s, s.M)
	case inst.SBC_M:
		execSBC(s, s.M)
	case inst.AND_M:
		s.A &= s.M
		setNZ(s, s.A)
	case inst.ORA_M:
		s.A |= s.M
		setNZ(s, s.A)
	case inst.EOR_M:
		s.A ^= s.M
		setNZ(s, s.A)

	// === Memory compares ===
	case inst.CMP_M:
		execCMP(s, s.A, s.M)
	case inst.CPX_M:
		execCMP(s, s.X, s.M)
	case inst.CPY_M:
		execCMP(s, s.Y, s.M)

	// === Memory inc/dec ===
	case inst.INC_M:
		s.M++
		setNZ(s, s.M)
	case inst.DEC_M:
		s.M--
		setNZ(s, s.M)

	// === Memory shifts ===
	case inst.ASL_M:
		execASL(s, &s.M)
	case inst.LSR_M:
		execLSR(s, &s.M)
	case inst.ROL_M:
		execROL(s, &s.M)
	case inst.ROR_M:
		execROR(s, &s.M)

	// === BIT test ===
	case inst.BIT_M:
		execBIT(s, s.M)

	default:
		panic("unhandled opcode in Exec")
	}
	return inst.Cycles(op)
}

// --- ALU helpers ---

// setNZ sets N and Z flags based on value, preserving other flags.
func setNZ(s *State, val uint8) {
	s.P = (s.P &^ (FlagN | FlagZ)) | NZTable[val]
}

// execADC implements ADC (binary mode only, D=0 assumed).
// result = A + operand + C
// V = (A^result) & (operand^result) & 0x80
func execADC(s *State, operand uint8) {
	a := uint16(s.A)
	m := uint16(operand)
	c := uint16(s.P & FlagC)
	result := a + m + c

	// Overflow: sign of result differs from both inputs
	v := (uint8(a) ^ uint8(result)) & (operand ^ uint8(result)) & 0x80

	s.A = uint8(result)
	s.P &^= FlagC | FlagZ | FlagN | FlagV
	s.P |= NZTable[s.A]
	if result > 0xFF {
		s.P |= FlagC
	}
	if v != 0 {
		s.P |= FlagV
	}
}

// execSBC implements SBC (binary mode only, D=0 assumed).
// result = A + ~operand + C (borrow = inverted carry)
func execSBC(s *State, operand uint8) {
	a := uint16(s.A)
	m := uint16(operand)
	c := uint16(s.P & FlagC)
	result := a - m - (1 - c)

	// Overflow: same formula but with complemented operand
	v := (s.A ^ uint8(result)) & (s.A ^ operand) & 0x80

	s.A = uint8(result)
	s.P &^= FlagC | FlagZ | FlagN | FlagV
	s.P |= NZTable[s.A]
	if result <= 0xFF { // no borrow = carry set
		s.P |= FlagC
	}
	if v != 0 {
		s.P |= FlagV
	}
}

// execCMP implements CMP/CPX/CPY: set N,Z,C without modifying the register.
// C is set if reg >= operand.
func execCMP(s *State, reg, operand uint8) {
	result := uint16(reg) - uint16(operand)
	s.P &^= FlagC | FlagZ | FlagN
	s.P |= NZTable[uint8(result)]
	if reg >= operand {
		s.P |= FlagC
	}
}

// execASL: Arithmetic Shift Left. C = old bit 7, then shift left.
func execASL(s *State, val *uint8) {
	s.P &^= FlagC
	if *val&0x80 != 0 {
		s.P |= FlagC
	}
	*val <<= 1
	setNZ(s, *val)
}

// execLSR: Logical Shift Right. C = old bit 0, then shift right.
func execLSR(s *State, val *uint8) {
	s.P &^= FlagC
	if *val&0x01 != 0 {
		s.P |= FlagC
	}
	*val >>= 1
	setNZ(s, *val)
}

// execROL: Rotate Left through carry.
func execROL(s *State, val *uint8) {
	oldC := s.P & FlagC
	s.P &^= FlagC
	if *val&0x80 != 0 {
		s.P |= FlagC
	}
	*val = (*val << 1) | oldC
	setNZ(s, *val)
}

// execROR: Rotate Right through carry.
func execROR(s *State, val *uint8) {
	oldC := s.P & FlagC
	s.P &^= FlagC
	if *val&0x01 != 0 {
		s.P |= FlagC
	}
	*val = (*val >> 1) | (oldC << 7)
	setNZ(s, *val)
}

// execBIT: BIT test. N=M[7], V=M[6], Z=(A&M)==0.
// Unique: N and V come from the operand, not the result.
func execBIT(s *State, operand uint8) {
	s.P &^= FlagN | FlagV | FlagZ
	if operand&0x80 != 0 {
		s.P |= FlagN
	}
	if operand&0x40 != 0 {
		s.P |= FlagV
	}
	if s.A&operand == 0 {
		s.P |= FlagZ
	}
}

// push pushes a byte onto the virtual stack.
// Uses S0/S1 as a 2-deep stack. S is decremented.
func push(s *State, val uint8) {
	s.S1 = s.S0
	s.S0 = val
	s.S--
}

// pull pulls a byte from the virtual stack.
// Uses S0/S1 as a 2-deep stack. S is incremented.
func pull(s *State) uint8 {
	val := s.S0
	s.S0 = s.S1
	s.S1 = 0
	s.S++
	return val
}
