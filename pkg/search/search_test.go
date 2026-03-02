package search

import (
	"testing"

	"github.com/oisee/6502-optimizer/pkg/inst"
)

func TestInstructionCount(t *testing.T) {
	// 47 non-immediate + 11 immediate * 256 = 47 + 2816 = 2863
	count := InstructionCount()
	nonImm := len(inst.NonImmediateOps())
	imm := len(inst.ImmediateOps())

	t.Logf("Non-immediate ops: %d", nonImm)
	t.Logf("Immediate ops: %d", imm)
	t.Logf("Total instructions: %d", count)

	if count != nonImm+imm*256 {
		t.Fatalf("InstructionCount=%d, want %d+%d*256=%d", count, nonImm, imm, nonImm+imm*256)
	}
}

func TestQuickCheckIdentical(t *testing.T) {
	// Same sequence should pass QuickCheck
	seq := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	if !QuickCheck(seq, seq) {
		t.Fatal("QuickCheck failed for identical sequences")
	}
}

func TestQuickCheckDifferent(t *testing.T) {
	// LDA #$42 vs LDA #$43 — different
	seq1 := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	seq2 := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x43}}
	if QuickCheck(seq1, seq2) {
		t.Fatal("QuickCheck passed for different sequences")
	}
}

func TestExhaustiveCheckIdentical(t *testing.T) {
	seq := []inst.Instruction{{Op: inst.INX}}
	if !ExhaustiveCheck(seq, seq) {
		t.Fatal("ExhaustiveCheck failed for identical sequences")
	}
}

func TestKnownOptimization_TXA_TAX(t *testing.T) {
	// TXA followed by TAX is equivalent to just TXA
	// (TXA sets A=X with NZ flags, TAX then sets X=A which is still X, with same flags)
	target := []inst.Instruction{{Op: inst.TXA}, {Op: inst.TAX}}
	candidate := []inst.Instruction{{Op: inst.TXA}}

	if !QuickCheck(target, candidate) {
		t.Fatal("QuickCheck rejected TXA:TAX -> TXA")
	}
	if !ExhaustiveCheck(target, candidate) {
		t.Fatal("ExhaustiveCheck rejected TXA:TAX -> TXA")
	}
}

func TestKnownOptimization_CLC_CLC(t *testing.T) {
	// CLC : CLC -> CLC (duplicate clear)
	target := []inst.Instruction{{Op: inst.CLC}, {Op: inst.CLC}}
	candidate := []inst.Instruction{{Op: inst.CLC}}

	if !QuickCheck(target, candidate) {
		t.Fatal("QuickCheck rejected CLC:CLC -> CLC")
	}
	if !ExhaustiveCheck(target, candidate) {
		t.Fatal("ExhaustiveCheck rejected CLC:CLC -> CLC")
	}
}

func TestKnownNonOptimization_LDA_EOR(t *testing.T) {
	// LDA #$00 vs EOR A (XOR A with itself) — NOT equivalent
	// LDA sets NZ flags but preserves C, V
	// EOR #$FF : EOR #$FF would give back the same A but different flags
	// Actually: LDA #$00 zeros A with Z=1,N=0; EOR A xors A with A giving 0 with Z=1,N=0
	// but EOR doesn't affect C while LDA doesn't affect C either... let's test:
	lda := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x00}}
	// AND #$00 also zeros A but leaves C unchanged — should be equivalent to LDA #$00!
	and := []inst.Instruction{{Op: inst.AND_IMM, Imm: 0x00}}

	// AND #0 zeros A, sets Z=1, N=0, but leaves C,V unchanged
	// LDA #0 sets A=0, Z=1, N=0, leaves C,V unchanged
	// These should be equivalent!
	if !QuickCheck(lda, and) {
		t.Fatal("QuickCheck rejected LDA #0 == AND #0")
	}
	if !ExhaustiveCheck(lda, and) {
		t.Fatal("ExhaustiveCheck rejected LDA #0 == AND #0")
	}
}

func TestNotEquivalent_LDA_XOR(t *testing.T) {
	// LDA #$00 vs EOR #$FF followed by EOR #$FF
	// LDA #$00 doesn't depend on initial A; EOR does
	lda := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x00}}
	eor := []inst.Instruction{{Op: inst.EOR_IMM, Imm: 0x00}} // EOR #0 = NOP for A, but sets NZ
	if QuickCheck(lda, eor) {
		// Actually EOR #0 keeps A, while LDA #0 zeros A
		t.Fatal("QuickCheck should reject LDA #0 vs EOR #0")
	}
}

func TestFingerprint(t *testing.T) {
	seq1 := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	seq2 := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x42}}
	seq3 := []inst.Instruction{{Op: inst.LDA_IMM, Imm: 0x43}}

	fp1 := Fingerprint(seq1)
	fp2 := Fingerprint(seq2)
	fp3 := Fingerprint(seq3)

	if fp1 != fp2 {
		t.Fatal("identical sequences have different fingerprints")
	}
	if fp1 == fp3 {
		t.Fatal("different sequences have same fingerprint")
	}
}

func TestShouldPrune_NOP(t *testing.T) {
	seq := []inst.Instruction{{Op: inst.NOP}, {Op: inst.INX}}
	if !ShouldPrune(seq) {
		t.Fatal("should prune sequence with NOP")
	}
}

func TestShouldPrune_StackOverflow(t *testing.T) {
	// 3 pushes without pulls
	seq := []inst.Instruction{
		{Op: inst.PHA}, {Op: inst.PHA}, {Op: inst.PHA},
	}
	if !ShouldPrune(seq) {
		t.Fatal("should prune 3+ pushes")
	}
}

func TestShouldPrune_StackUnderflow(t *testing.T) {
	// PLA without matching PHA
	seq := []inst.Instruction{{Op: inst.PLA}}
	if !ShouldPrune(seq) {
		t.Fatal("should prune PLA without push")
	}
}

func TestShouldPrune_ValidStack(t *testing.T) {
	// PHA:PLA is valid (depth 1)
	seq := []inst.Instruction{{Op: inst.PHA}, {Op: inst.PLA}}
	if ShouldPrune(seq) {
		t.Fatal("should not prune valid PHA:PLA")
	}
}

func TestShouldPrune_CanonicalOrder(t *testing.T) {
	// CLC:CLD are truly independent (both write only flags, different bits)
	// But they both write regP, so they're not independent by our definition.
	// Use SEC:CLI — wait, same issue.
	// Actually, ops that ONLY write flags share WAW on regP, so none are "independent".
	// Test with ops that write different registers and no flags:
	// STA_M and STX_M both write M → not independent (WAW on M).
	// TXS (writes S, no flags) and STA_M (writes M, no flags) → independent!
	seq := []inst.Instruction{{Op: inst.TXS}, {Op: inst.STA_M}}
	if ShouldPrune(seq) {
		t.Fatal("should not prune TXS:STA_M (correct canonical order)")
	}

	// Reverse order should be pruned
	seq2 := []inst.Instruction{{Op: inst.STA_M}, {Op: inst.TXS}}
	if !ShouldPrune(seq2) {
		t.Fatal("should prune STA_M:TXS (wrong canonical order)")
	}
}

func TestMidCheck(t *testing.T) {
	// MidCheck should pass for identical sequences
	seq := []inst.Instruction{{Op: inst.INX}}
	if !MidCheck(seq, seq) {
		t.Fatal("MidCheck failed for identical sequences")
	}

	// MidCheck should reject different sequences
	seq1 := []inst.Instruction{{Op: inst.INX}}
	seq2 := []inst.Instruction{{Op: inst.INY}}
	if MidCheck(seq1, seq2) {
		t.Fatal("MidCheck passed for different sequences")
	}
}

func TestExhaustiveWithExtraRegs(t *testing.T) {
	// TXA : INX — reads X, modifies A and X
	// Should sweep X as an extra register
	target := []inst.Instruction{{Op: inst.TXA}, {Op: inst.INX}}
	if !ExhaustiveCheck(target, target) {
		t.Fatal("ExhaustiveCheck failed for self-check with extra regs")
	}
}

func TestOpCodeCount(t *testing.T) {
	if inst.OpCodeCount != 58 {
		t.Fatalf("OpCodeCount=%d, want 58", inst.OpCodeCount)
	}
}
