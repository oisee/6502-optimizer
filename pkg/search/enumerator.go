package search

import "github.com/oisee/6502-optimizer/pkg/inst"

// EnumerateSequences generates all instruction sequences of exactly length n.
// 6502 has no 16-bit immediates, so this is the same as EnumerateSequences8.
func EnumerateSequences(n int, fn func(seq []inst.Instruction) bool) {
	nonImm := inst.NonImmediateOps()
	immOps := inst.ImmediateOps()
	seq := make([]inst.Instruction, n)
	enumerateRec(seq, 0, nonImm, immOps, fn)
}

func enumerateRec(seq []inst.Instruction, pos int, nonImm []inst.OpCode, immOps []inst.OpCode, fn func([]inst.Instruction) bool) bool {
	if pos == len(seq) {
		return fn(seq)
	}

	// Non-immediate instructions
	for _, op := range nonImm {
		seq[pos] = inst.Instruction{Op: op, Imm: 0}
		if !enumerateRec(seq, pos+1, nonImm, immOps, fn) {
			return false
		}
	}

	// 8-bit immediate instructions with all 256 values
	for _, op := range immOps {
		for imm := 0; imm < 256; imm++ {
			seq[pos] = inst.Instruction{Op: op, Imm: uint16(imm)}
			if !enumerateRec(seq, pos+1, nonImm, immOps, fn) {
				return false
			}
		}
	}

	return true
}

// InstructionCount returns the number of distinct instructions.
// 6502: 47 non-immediate + 11 immediate * 256 = 2,863
func InstructionCount() int {
	return len(inst.NonImmediateOps()) + len(inst.ImmediateOps())*256
}

// EnumerateFirstOp returns all possible first instructions (for partitioning).
func EnumerateFirstOp() []inst.Instruction {
	result := make([]inst.Instruction, 0, InstructionCount())
	for _, op := range inst.NonImmediateOps() {
		result = append(result, inst.Instruction{Op: op, Imm: 0})
	}
	for _, op := range inst.ImmediateOps() {
		for imm := 0; imm < 256; imm++ {
			result = append(result, inst.Instruction{Op: op, Imm: uint16(imm)})
		}
	}
	return result
}
