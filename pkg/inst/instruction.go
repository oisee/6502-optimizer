package inst

// OpCode is a compact identifier for a 6502 instruction (not the raw byte encoding).
type OpCode uint16

// Instruction is a compact representation of one 6502 instruction.
type Instruction struct {
	Op  OpCode
	Imm uint16 // Immediate value (8-bit only for 6502; uint16 for Z80 compat)
}

// HasImmediate returns true if this opcode uses an 8-bit immediate operand.
func HasImmediate(op OpCode) bool {
	switch op {
	case LDA_IMM, LDX_IMM, LDY_IMM,
		ADC_IMM, SBC_IMM, AND_IMM, ORA_IMM, EOR_IMM,
		CMP_IMM, CPX_IMM, CPY_IMM:
		return true
	}
	return false
}

// UsesMemory returns true if this opcode accesses the virtual memory byte (State.M).
func UsesMemory(op OpCode) bool {
	return op >= LDA_M && op <= BIT_M
}

// UsesStack returns true if this opcode accesses the virtual stack slots.
func UsesStack(op OpCode) bool {
	switch op {
	case PHA, PLA, PHP, PLP:
		return true
	}
	return false
}

// OpCode constants for the 6502 superoptimizer.
// 58 opcodes total:
//   - 26 implied (no operand)
//   - 11 immediate (#nn)
//   - 21 zero-page/memory (virtual M)
const (
	// === Implied instructions (26 opcodes, 1 byte each) ===

	// Register transfers
	TAX OpCode = iota
	TAY
	TXA
	TYA
	TSX
	TXS

	// Increment/decrement registers
	INX
	INY
	DEX
	DEY

	// Shifts/rotates on accumulator
	ASL_A
	LSR_A
	ROL_A
	ROR_A

	// Flag operations
	CLC
	SEC
	CLD
	SED
	CLI
	SEI
	CLV

	// Stack operations
	PHA
	PLA
	PHP
	PLP

	// No-op
	NOP

	// === Immediate instructions (11 opcodes, 2 bytes each) ===

	LDA_IMM
	LDX_IMM
	LDY_IMM
	ADC_IMM
	SBC_IMM
	AND_IMM
	ORA_IMM
	EOR_IMM
	CMP_IMM
	CPX_IMM
	CPY_IMM

	// === Zero-page memory / virtual M instructions (21 opcodes) ===

	LDA_M
	LDX_M
	LDY_M
	STA_M
	STX_M
	STY_M
	ADC_M
	SBC_M
	AND_M
	ORA_M
	EOR_M
	CMP_M
	CPX_M
	CPY_M
	INC_M
	DEC_M
	ASL_M
	LSR_M
	ROL_M
	ROR_M
	BIT_M

	OpCodeCount // sentinel
)
