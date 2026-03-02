package stoke

import (
	"math/rand/v2"
	"testing"

	"github.com/oisee/6502-optimizer/pkg/inst"
)

func TestCostIdentical(t *testing.T) {
	seq := []inst.Instruction{{Op: inst.INX}}
	cost := Cost(seq, seq)
	// Should be: 0 mismatches, 1 byte, 2 cycles → 0*1000 + 1 + 2/100 = 1
	if cost >= 1000 {
		t.Fatalf("identical sequences should have 0 mismatches, got cost %d", cost)
	}
}

func TestCostDifferent(t *testing.T) {
	target := []inst.Instruction{{Op: inst.INX}}
	candidate := []inst.Instruction{{Op: inst.INY}}
	cost := Cost(target, candidate)
	// Should have nonzero mismatches (X vs Y differ)
	if cost < 1000 {
		t.Fatalf("different sequences should have nonzero mismatches, got cost %d", cost)
	}
}

func TestMutator(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 42))
	m := NewMutator(rng, 10)

	seq := []inst.Instruction{{Op: inst.INX}, {Op: inst.INY}}

	// Just verify mutations don't panic
	for i := 0; i < 100; i++ {
		result := m.Mutate(seq)
		if len(result) == 0 {
			t.Fatal("mutation produced empty sequence")
		}
	}
}

func TestChainStep(t *testing.T) {
	target := []inst.Instruction{{Op: inst.TXA}, {Op: inst.TAX}}
	chain := NewChain(target, 1.0, 42)

	// Run a few steps
	for i := 0; i < 100; i++ {
		chain.Step(0.9999)
	}

	// Should have accepted some and rejected some
	if chain.Accepted+chain.Rejected != 100 {
		t.Fatalf("expected 100 total steps, got %d accepted + %d rejected",
			chain.Accepted, chain.Rejected)
	}
}

func TestMismatchesIdentical(t *testing.T) {
	seq := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	m := Mismatches(seq, seq)
	if m != 0 {
		t.Fatalf("identical sequences should have 0 mismatches, got %d", m)
	}
}

func TestMismatchesMasked(t *testing.T) {
	seq := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	m := MismatchesMasked(seq, seq, 0xFF)
	if m != 0 {
		t.Fatalf("identical sequences should have 0 masked mismatches, got %d", m)
	}
}
