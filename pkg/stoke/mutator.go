package stoke

import (
	"math/rand/v2"

	"github.com/oisee/6502-optimizer/pkg/inst"
)

// Mutator applies random mutations to instruction sequences.
type Mutator struct {
	rng    *rand.Rand
	nonImm []inst.OpCode
	immOps []inst.OpCode
	allOps []inst.OpCode
	maxLen int
}

// NewMutator creates a Mutator with cached opcode lists.
func NewMutator(rng *rand.Rand, maxLen int) *Mutator {
	return &Mutator{
		rng:    rng,
		nonImm: inst.NonImmediateOps(),
		immOps: inst.ImmediateOps(),
		allOps: inst.AllOps(),
		maxLen: maxLen,
	}
}

// Mutate applies a random mutation to seq and returns the new sequence.
func (m *Mutator) Mutate(seq []inst.Instruction) []inst.Instruction {
	r := m.rng.IntN(100)
	switch {
	case r < 40:
		return m.ReplaceInstruction(seq)
	case r < 60:
		return m.SwapInstructions(seq)
	case r < 80:
		return m.DeleteInstruction(seq)
	case r < 90:
		return m.InsertInstruction(seq)
	default:
		return m.ChangeImmediate(seq)
	}
}

// ReplaceInstruction swaps one instruction with a random one.
func (m *Mutator) ReplaceInstruction(seq []inst.Instruction) []inst.Instruction {
	out := copySeq(seq)
	pos := m.rng.IntN(len(out))
	out[pos] = m.randomInstruction()
	return out
}

// SwapInstructions swaps two adjacent instructions.
func (m *Mutator) SwapInstructions(seq []inst.Instruction) []inst.Instruction {
	out := copySeq(seq)
	if len(out) < 2 {
		return out
	}
	pos := m.rng.IntN(len(out) - 1)
	out[pos], out[pos+1] = out[pos+1], out[pos]
	return out
}

// DeleteInstruction removes one instruction (if len > 1).
func (m *Mutator) DeleteInstruction(seq []inst.Instruction) []inst.Instruction {
	if len(seq) <= 1 {
		return copySeq(seq)
	}
	pos := m.rng.IntN(len(seq))
	out := make([]inst.Instruction, 0, len(seq)-1)
	out = append(out, seq[:pos]...)
	out = append(out, seq[pos+1:]...)
	return out
}

// InsertInstruction adds a random instruction at a random position.
func (m *Mutator) InsertInstruction(seq []inst.Instruction) []inst.Instruction {
	if len(seq) >= m.maxLen {
		return m.ReplaceInstruction(seq)
	}
	pos := m.rng.IntN(len(seq) + 1)
	newInstr := m.randomInstruction()
	out := make([]inst.Instruction, 0, len(seq)+1)
	out = append(out, seq[:pos]...)
	out = append(out, newInstr)
	out = append(out, seq[pos:]...)
	return out
}

// ChangeImmediate randomizes the immediate value of an instruction.
func (m *Mutator) ChangeImmediate(seq []inst.Instruction) []inst.Instruction {
	var immPos []int
	for i, instr := range seq {
		if inst.HasImmediate(instr.Op) {
			immPos = append(immPos, i)
		}
	}
	if len(immPos) == 0 {
		return m.ReplaceInstruction(seq)
	}
	out := copySeq(seq)
	pos := immPos[m.rng.IntN(len(immPos))]
	out[pos].Imm = uint16(m.rng.IntN(256))
	return out
}

func (m *Mutator) randomInstruction() inst.Instruction {
	op := m.allOps[m.rng.IntN(len(m.allOps))]
	var imm uint16
	if inst.HasImmediate(op) {
		imm = uint16(m.rng.IntN(256))
	}
	return inst.Instruction{Op: op, Imm: imm}
}

func copySeq(seq []inst.Instruction) []inst.Instruction {
	out := make([]inst.Instruction, len(seq))
	copy(out, seq)
	return out
}
