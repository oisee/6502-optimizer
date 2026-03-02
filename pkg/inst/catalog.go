package inst

// Info holds static metadata for an instruction opcode.
type Info struct {
	Mnemonic string  // Assembly mnemonic (e.g., "LDA #n")
	Bytes    []uint8 // Raw encoding (without immediate)
	Cycles   int     // Clock cycles
}

// Catalog maps each OpCode to its Info.
var Catalog [OpCodeCount]Info

// AllOps returns all valid OpCode values.
func AllOps() []OpCode {
	ops := make([]OpCode, 0, OpCodeCount)
	for i := OpCode(0); i < OpCodeCount; i++ {
		ops = append(ops, i)
	}
	return ops
}

// NonImmediateOps returns all OpCodes that don't take an immediate.
func NonImmediateOps() []OpCode {
	ops := make([]OpCode, 0)
	for i := OpCode(0); i < OpCodeCount; i++ {
		if !HasImmediate(i) {
			ops = append(ops, i)
		}
	}
	return ops
}

// ImmediateOps returns all OpCodes that take an 8-bit immediate byte.
func ImmediateOps() []OpCode {
	ops := make([]OpCode, 0)
	for i := OpCode(0); i < OpCodeCount; i++ {
		if HasImmediate(i) {
			ops = append(ops, i)
		}
	}
	return ops
}

// Cycles returns the cycle cost of an instruction.
func Cycles(op OpCode) int {
	return Catalog[op].Cycles
}

// ByteSize returns the total byte size of an instruction (encoding + immediate).
func ByteSize(op OpCode) int {
	n := len(Catalog[op].Bytes)
	if HasImmediate(op) {
		n++
	}
	return n
}

// Disassemble returns assembly text for an instruction.
func Disassemble(instr Instruction) string {
	info := &Catalog[instr.Op]
	if HasImmediate(instr.Op) {
		return disasmImm8(info.Mnemonic, uint8(instr.Imm))
	}
	return info.Mnemonic
}

func disasmImm8(mnemonic string, imm uint8) string {
	// Replace "n" placeholder with hex value
	buf := make([]byte, 0, len(mnemonic)+4)
	for i := 0; i < len(mnemonic); i++ {
		if mnemonic[i] == 'n' && (i == 0 || mnemonic[i-1] == '#' || mnemonic[i-1] == ' ') {
			buf = appendHex8(buf, imm)
		} else {
			buf = append(buf, mnemonic[i])
		}
	}
	return string(buf)
}

func appendHex8(buf []byte, v uint8) []byte {
	const hex = "0123456789ABCDEF"
	buf = append(buf, '$')
	buf = append(buf, hex[v>>4], hex[v&0x0F])
	return buf
}

// SeqByteSize returns total byte size for a sequence of instructions.
func SeqByteSize(seq []Instruction) int {
	n := 0
	for i := range seq {
		n += ByteSize(seq[i].Op)
	}
	return n
}

// SeqCycles returns total cycles for a sequence of instructions.
func SeqCycles(seq []Instruction) int {
	t := 0
	for i := range seq {
		t += Cycles(seq[i].Op)
	}
	return t
}

func init() {
	// === Implied instructions (1 byte each) ===

	// Register transfers: 2 cycles
	Catalog[TAX] = Info{"TAX", []uint8{0xAA}, 2}
	Catalog[TAY] = Info{"TAY", []uint8{0xA8}, 2}
	Catalog[TXA] = Info{"TXA", []uint8{0x8A}, 2}
	Catalog[TYA] = Info{"TYA", []uint8{0x98}, 2}
	Catalog[TSX] = Info{"TSX", []uint8{0xBA}, 2}
	Catalog[TXS] = Info{"TXS", []uint8{0x9A}, 2}

	// Increment/decrement: 2 cycles
	Catalog[INX] = Info{"INX", []uint8{0xE8}, 2}
	Catalog[INY] = Info{"INY", []uint8{0xC8}, 2}
	Catalog[DEX] = Info{"DEX", []uint8{0xCA}, 2}
	Catalog[DEY] = Info{"DEY", []uint8{0x88}, 2}

	// Shifts on accumulator: 2 cycles
	Catalog[ASL_A] = Info{"ASL A", []uint8{0x0A}, 2}
	Catalog[LSR_A] = Info{"LSR A", []uint8{0x4A}, 2}
	Catalog[ROL_A] = Info{"ROL A", []uint8{0x2A}, 2}
	Catalog[ROR_A] = Info{"ROR A", []uint8{0x6A}, 2}

	// Flag operations: 2 cycles
	Catalog[CLC] = Info{"CLC", []uint8{0x18}, 2}
	Catalog[SEC] = Info{"SEC", []uint8{0x38}, 2}
	Catalog[CLD] = Info{"CLD", []uint8{0xD8}, 2}
	Catalog[SED] = Info{"SED", []uint8{0xF8}, 2}
	Catalog[CLI] = Info{"CLI", []uint8{0x58}, 2}
	Catalog[SEI] = Info{"SEI", []uint8{0x78}, 2}
	Catalog[CLV] = Info{"CLV", []uint8{0xB8}, 2}

	// Stack: PHA/PHP = 3 cycles, PLA/PLP = 4 cycles
	Catalog[PHA] = Info{"PHA", []uint8{0x48}, 3}
	Catalog[PLA] = Info{"PLA", []uint8{0x68}, 4}
	Catalog[PHP] = Info{"PHP", []uint8{0x08}, 3}
	Catalog[PLP] = Info{"PLP", []uint8{0x28}, 4}

	// NOP: 2 cycles
	Catalog[NOP] = Info{"NOP", []uint8{0xEA}, 2}

	// === Immediate instructions (2 bytes each) ===

	// Load immediate: 2 cycles
	Catalog[LDA_IMM] = Info{"LDA #n", []uint8{0xA9}, 2}
	Catalog[LDX_IMM] = Info{"LDX #n", []uint8{0xA2}, 2}
	Catalog[LDY_IMM] = Info{"LDY #n", []uint8{0xA0}, 2}

	// ALU immediate: 2 cycles
	Catalog[ADC_IMM] = Info{"ADC #n", []uint8{0x69}, 2}
	Catalog[SBC_IMM] = Info{"SBC #n", []uint8{0xE9}, 2}
	Catalog[AND_IMM] = Info{"AND #n", []uint8{0x29}, 2}
	Catalog[ORA_IMM] = Info{"ORA #n", []uint8{0x09}, 2}
	Catalog[EOR_IMM] = Info{"EOR #n", []uint8{0x49}, 2}

	// Compare immediate: 2 cycles
	Catalog[CMP_IMM] = Info{"CMP #n", []uint8{0xC9}, 2}
	Catalog[CPX_IMM] = Info{"CPX #n", []uint8{0xE0}, 2}
	Catalog[CPY_IMM] = Info{"CPY #n", []uint8{0xC0}, 2}

	// === Zero-page / virtual M instructions ===

	// Load from M: 3 cycles, 2 bytes
	Catalog[LDA_M] = Info{"LDA M", []uint8{0xA5}, 3}
	Catalog[LDX_M] = Info{"LDX M", []uint8{0xA6}, 3}
	Catalog[LDY_M] = Info{"LDY M", []uint8{0xA4}, 3}

	// Store to M: 3 cycles, 2 bytes
	Catalog[STA_M] = Info{"STA M", []uint8{0x85}, 3}
	Catalog[STX_M] = Info{"STX M", []uint8{0x86}, 3}
	Catalog[STY_M] = Info{"STY M", []uint8{0x84}, 3}

	// ALU with M: 3 cycles, 2 bytes
	Catalog[ADC_M] = Info{"ADC M", []uint8{0x65}, 3}
	Catalog[SBC_M] = Info{"SBC M", []uint8{0xE5}, 3}
	Catalog[AND_M] = Info{"AND M", []uint8{0x25}, 3}
	Catalog[ORA_M] = Info{"ORA M", []uint8{0x05}, 3}
	Catalog[EOR_M] = Info{"EOR M", []uint8{0x45}, 3}

	// Compare with M: 3 cycles, 2 bytes
	Catalog[CMP_M] = Info{"CMP M", []uint8{0xC5}, 3}
	Catalog[CPX_M] = Info{"CPX M", []uint8{0xE4}, 3}
	Catalog[CPY_M] = Info{"CPY M", []uint8{0xC4}, 3}

	// Inc/Dec M: 5 cycles, 2 bytes
	Catalog[INC_M] = Info{"INC M", []uint8{0xE6}, 5}
	Catalog[DEC_M] = Info{"DEC M", []uint8{0xC6}, 5}

	// Shift M: 5 cycles, 2 bytes
	Catalog[ASL_M] = Info{"ASL M", []uint8{0x06}, 5}
	Catalog[LSR_M] = Info{"LSR M", []uint8{0x46}, 5}
	Catalog[ROL_M] = Info{"ROL M", []uint8{0x26}, 5}
	Catalog[ROR_M] = Info{"ROR M", []uint8{0x66}, 5}

	// BIT test M: 3 cycles, 2 bytes
	Catalog[BIT_M] = Info{"BIT M", []uint8{0x24}, 3}
}
