package cpu

import (
	"testing"

	"github.com/oisee/6502-optimizer/pkg/inst"
)

func TestTransfers(t *testing.T) {
	s := State{A: 0x42, X: 0x00, Y: 0x00}

	Exec(&s, inst.TAX, 0)
	if s.X != 0x42 {
		t.Fatalf("TAX: X=%02X want 42", s.X)
	}
	if s.P&FlagZ != 0 {
		t.Fatal("TAX: Z set for non-zero")
	}

	s.A = 0x00
	Exec(&s, inst.TAY, 0)
	if s.Y != 0x00 {
		t.Fatalf("TAY: Y=%02X want 00", s.Y)
	}
	if s.P&FlagZ == 0 {
		t.Fatal("TAY: Z not set for zero")
	}

	s.X = 0x80
	Exec(&s, inst.TXA, 0)
	if s.A != 0x80 {
		t.Fatalf("TXA: A=%02X want 80", s.A)
	}
	if s.P&FlagN == 0 {
		t.Fatal("TXA: N not set for negative")
	}

	s.Y = 0x55
	Exec(&s, inst.TYA, 0)
	if s.A != 0x55 {
		t.Fatalf("TYA: A=%02X want 55", s.A)
	}
}

func TestTXS_TSX(t *testing.T) {
	s := State{X: 0xFD}
	Exec(&s, inst.TXS, 0)
	if s.S != 0xFD {
		t.Fatalf("TXS: S=%02X want FD", s.S)
	}
	// TXS does NOT affect flags
	if s.P&(FlagN|FlagZ) != 0 {
		t.Fatal("TXS should not affect flags")
	}

	s.S = 0x80
	Exec(&s, inst.TSX, 0)
	if s.X != 0x80 {
		t.Fatalf("TSX: X=%02X want 80", s.X)
	}
	if s.P&FlagN == 0 {
		t.Fatal("TSX: N not set for 0x80")
	}
}

func TestIncrementDecrement(t *testing.T) {
	s := State{X: 0xFF}
	Exec(&s, inst.INX, 0)
	if s.X != 0x00 {
		t.Fatalf("INX: X=%02X want 00", s.X)
	}
	if s.P&FlagZ == 0 {
		t.Fatal("INX: Z not set on wrap to zero")
	}

	s.Y = 0x7F
	Exec(&s, inst.INY, 0)
	if s.Y != 0x80 {
		t.Fatalf("INY: Y=%02X want 80", s.Y)
	}
	if s.P&FlagN == 0 {
		t.Fatal("INY: N not set on 0x80")
	}

	s.X = 0x00
	Exec(&s, inst.DEX, 0)
	if s.X != 0xFF {
		t.Fatalf("DEX: X=%02X want FF", s.X)
	}
	if s.P&FlagN == 0 {
		t.Fatal("DEX: N not set on 0xFF")
	}

	s.Y = 0x01
	Exec(&s, inst.DEY, 0)
	if s.Y != 0x00 {
		t.Fatalf("DEY: Y=%02X want 00", s.Y)
	}
	if s.P&FlagZ == 0 {
		t.Fatal("DEY: Z not set on 0x00")
	}
}

func TestADC(t *testing.T) {
	tests := []struct {
		name     string
		a, imm   uint8
		carry    bool
		wantA    uint8
		wantC    bool
		wantV    bool
		wantZ    bool
		wantN    bool
	}{
		{"simple", 0x10, 0x20, false, 0x30, false, false, false, false},
		{"with carry in", 0x10, 0x20, true, 0x31, false, false, false, false},
		{"carry out", 0xFF, 0x01, false, 0x00, true, false, true, false},
		{"overflow pos+pos", 0x7F, 0x01, false, 0x80, false, true, false, true},
		{"overflow neg+neg", 0x80, 0x80, false, 0x00, true, true, true, false},
		{"no overflow", 0x40, 0x20, false, 0x60, false, false, false, false},
		{"zero result", 0x00, 0x00, false, 0x00, false, false, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := State{A: tc.a}
			if tc.carry {
				s.P |= FlagC
			}
			Exec(&s, inst.ADC_IMM, uint16(tc.imm))
			if s.A != tc.wantA {
				t.Errorf("A=%02X want %02X", s.A, tc.wantA)
			}
			if (s.P&FlagC != 0) != tc.wantC {
				t.Errorf("C=%v want %v", s.P&FlagC != 0, tc.wantC)
			}
			if (s.P&FlagV != 0) != tc.wantV {
				t.Errorf("V=%v want %v", s.P&FlagV != 0, tc.wantV)
			}
			if (s.P&FlagZ != 0) != tc.wantZ {
				t.Errorf("Z=%v want %v", s.P&FlagZ != 0, tc.wantZ)
			}
			if (s.P&FlagN != 0) != tc.wantN {
				t.Errorf("N=%v want %v", s.P&FlagN != 0, tc.wantN)
			}
		})
	}
}

func TestSBC(t *testing.T) {
	tests := []struct {
		name     string
		a, imm   uint8
		carry    bool // carry=1 means no borrow
		wantA    uint8
		wantC    bool
		wantV    bool
	}{
		{"simple", 0x30, 0x10, true, 0x20, true, false},
		{"with borrow", 0x30, 0x10, false, 0x1F, true, false},
		{"borrow out", 0x00, 0x01, true, 0xFF, false, false},
		{"overflow", 0x80, 0x01, true, 0x7F, true, true},
		{"neg overflow", 0x7F, 0xFF, true, 0x80, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := State{A: tc.a}
			if tc.carry {
				s.P |= FlagC
			}
			Exec(&s, inst.SBC_IMM, uint16(tc.imm))
			if s.A != tc.wantA {
				t.Errorf("A=%02X want %02X", s.A, tc.wantA)
			}
			if (s.P&FlagC != 0) != tc.wantC {
				t.Errorf("C=%v want %v", s.P&FlagC != 0, tc.wantC)
			}
			if (s.P&FlagV != 0) != tc.wantV {
				t.Errorf("V=%v want %v", s.P&FlagV != 0, tc.wantV)
			}
		})
	}
}

func TestADC_SBC_Exhaustive(t *testing.T) {
	// Verify ADC: for every A,M,C triple, check result and flags
	for a := 0; a < 256; a++ {
		for m := 0; m < 256; m++ {
			for c := 0; c <= 1; c++ {
				s := State{A: uint8(a)}
				if c == 1 {
					s.P |= FlagC
				}
				Exec(&s, inst.ADC_IMM, uint16(m))

				result16 := uint16(a) + uint16(m) + uint16(c)
				wantA := uint8(result16)
				wantC := result16 > 0xFF
				wantZ := wantA == 0
				wantN := wantA&0x80 != 0
				v := (uint8(a) ^ wantA) & (uint8(m) ^ wantA) & 0x80
				wantV := v != 0

				if s.A != wantA {
					t.Fatalf("ADC %02X+%02X+%d: A=%02X want %02X", a, m, c, s.A, wantA)
				}
				if (s.P&FlagC != 0) != wantC {
					t.Fatalf("ADC %02X+%02X+%d: C=%v want %v", a, m, c, s.P&FlagC != 0, wantC)
				}
				if (s.P&FlagZ != 0) != wantZ {
					t.Fatalf("ADC %02X+%02X+%d: Z=%v want %v", a, m, c, s.P&FlagZ != 0, wantZ)
				}
				if (s.P&FlagN != 0) != wantN {
					t.Fatalf("ADC %02X+%02X+%d: N=%v want %v", a, m, c, s.P&FlagN != 0, wantN)
				}
				if (s.P&FlagV != 0) != wantV {
					t.Fatalf("ADC %02X+%02X+%d: V=%v want %v", a, m, c, s.P&FlagV != 0, wantV)
				}
			}
		}
	}

	// Verify SBC: for every A,M,C triple
	for a := 0; a < 256; a++ {
		for m := 0; m < 256; m++ {
			for c := 0; c <= 1; c++ {
				s := State{A: uint8(a)}
				if c == 1 {
					s.P |= FlagC
				}
				Exec(&s, inst.SBC_IMM, uint16(m))

				result16 := uint16(a) - uint16(m) - uint16(1-c)
				wantA := uint8(result16)
				wantC := result16 <= 0xFF // no borrow
				wantZ := wantA == 0
				wantN := wantA&0x80 != 0
				v := (uint8(a) ^ wantA) & (uint8(a) ^ uint8(m)) & 0x80
				wantV := v != 0

				if s.A != wantA {
					t.Fatalf("SBC %02X-%02X-borrow(%d): A=%02X want %02X", a, m, 1-c, s.A, wantA)
				}
				if (s.P&FlagC != 0) != wantC {
					t.Fatalf("SBC %02X-%02X-borrow(%d): C=%v want %v", a, m, 1-c, s.P&FlagC != 0, wantC)
				}
				if (s.P&FlagZ != 0) != wantZ {
					t.Fatalf("SBC %02X-%02X-borrow(%d): Z=%v want %v", a, m, 1-c, s.P&FlagZ != 0, wantZ)
				}
				if (s.P&FlagN != 0) != wantN {
					t.Fatalf("SBC %02X-%02X-borrow(%d): N=%v want %v", a, m, 1-c, s.P&FlagN != 0, wantN)
				}
				if (s.P&FlagV != 0) != wantV {
					t.Fatalf("SBC %02X-%02X-borrow(%d): V=%v want %v", a, m, 1-c, s.P&FlagV != 0, wantV)
				}
			}
		}
	}
}

func TestCompare(t *testing.T) {
	// CMP #$30 with A=$30 → Z=1, C=1, N=0
	s := State{A: 0x30}
	Exec(&s, inst.CMP_IMM, 0x30)
	if s.P&FlagZ == 0 {
		t.Fatal("CMP equal: Z not set")
	}
	if s.P&FlagC == 0 {
		t.Fatal("CMP equal: C not set")
	}
	if s.A != 0x30 {
		t.Fatal("CMP modified A")
	}

	// CMP #$40 with A=$30 → Z=0, C=0, N=1 (result is $F0)
	s = State{A: 0x30}
	Exec(&s, inst.CMP_IMM, 0x40)
	if s.P&FlagZ != 0 {
		t.Fatal("CMP less: Z set")
	}
	if s.P&FlagC != 0 {
		t.Fatal("CMP less: C set")
	}
	if s.P&FlagN == 0 {
		t.Fatal("CMP less: N not set")
	}

	// CMP #$10 with A=$30 → Z=0, C=1, N=0
	s = State{A: 0x30}
	Exec(&s, inst.CMP_IMM, 0x10)
	if s.P&FlagZ != 0 {
		t.Fatal("CMP greater: Z set")
	}
	if s.P&FlagC == 0 {
		t.Fatal("CMP greater: C not set")
	}

	// CPX
	s = State{X: 0x42}
	Exec(&s, inst.CPX_IMM, 0x42)
	if s.P&FlagZ == 0 {
		t.Fatal("CPX equal: Z not set")
	}

	// CPY
	s = State{Y: 0x00}
	Exec(&s, inst.CPY_IMM, 0x01)
	if s.P&FlagC != 0 {
		t.Fatal("CPY less: C set")
	}
}

func TestShifts(t *testing.T) {
	// ASL A: 0x81 → 0x02, C=1
	s := State{A: 0x81}
	Exec(&s, inst.ASL_A, 0)
	if s.A != 0x02 {
		t.Fatalf("ASL: A=%02X want 02", s.A)
	}
	if s.P&FlagC == 0 {
		t.Fatal("ASL: C not set")
	}

	// LSR A: 0x03 → 0x01, C=1
	s = State{A: 0x03}
	Exec(&s, inst.LSR_A, 0)
	if s.A != 0x01 {
		t.Fatalf("LSR: A=%02X want 01", s.A)
	}
	if s.P&FlagC == 0 {
		t.Fatal("LSR: C not set")
	}

	// ROL A with C=1: 0x80 → 0x01, new C=1
	s = State{A: 0x80, P: FlagC}
	Exec(&s, inst.ROL_A, 0)
	if s.A != 0x01 {
		t.Fatalf("ROL: A=%02X want 01", s.A)
	}
	if s.P&FlagC == 0 {
		t.Fatal("ROL: C not set")
	}

	// ROR A with C=1: 0x01 → 0x80, new C=1
	s = State{A: 0x01, P: FlagC}
	Exec(&s, inst.ROR_A, 0)
	if s.A != 0x80 {
		t.Fatalf("ROR: A=%02X want 80", s.A)
	}
	if s.P&FlagC == 0 {
		t.Fatal("ROR: C not set")
	}
	if s.P&FlagN == 0 {
		t.Fatal("ROR: N not set")
	}
}

func TestLogic(t *testing.T) {
	s := State{A: 0xFF}
	Exec(&s, inst.AND_IMM, 0x0F)
	if s.A != 0x0F {
		t.Fatalf("AND: A=%02X want 0F", s.A)
	}

	s = State{A: 0x0F}
	Exec(&s, inst.ORA_IMM, 0xF0)
	if s.A != 0xFF {
		t.Fatalf("ORA: A=%02X want FF", s.A)
	}
	if s.P&FlagN == 0 {
		t.Fatal("ORA: N not set")
	}

	s = State{A: 0xFF}
	Exec(&s, inst.EOR_IMM, 0xFF)
	if s.A != 0x00 {
		t.Fatalf("EOR: A=%02X want 00", s.A)
	}
	if s.P&FlagZ == 0 {
		t.Fatal("EOR: Z not set")
	}
}

func TestStack(t *testing.T) {
	// PHA then PLA
	s := State{A: 0x42, S: 0xFD}
	Exec(&s, inst.PHA, 0)
	if s.S != 0xFC {
		t.Fatalf("PHA: S=%02X want FC", s.S)
	}
	if s.S0 != 0x42 {
		t.Fatalf("PHA: S0=%02X want 42", s.S0)
	}

	s.A = 0x00
	Exec(&s, inst.PLA, 0)
	if s.A != 0x42 {
		t.Fatalf("PLA: A=%02X want 42", s.A)
	}
	if s.S != 0xFD {
		t.Fatalf("PLA: S=%02X want FD", s.S)
	}

	// PHP/PLP
	s = State{P: FlagC | FlagN, S: 0xFD}
	Exec(&s, inst.PHP, 0)
	// PHP pushes with B and U bits set
	expectedPush := FlagC | FlagN | FlagB | FlagU
	if s.S0 != expectedPush {
		t.Fatalf("PHP: S0=%02X want %02X", s.S0, expectedPush)
	}

	s.P = 0x00 // clear P
	Exec(&s, inst.PLP, 0)
	if s.P != expectedPush {
		t.Fatalf("PLP: P=%02X want %02X", s.P, expectedPush)
	}
}

func TestBIT(t *testing.T) {
	// BIT M where M=0xC0 (bits 7,6 set), A=0x00 → N=1, V=1, Z=1
	s := State{A: 0x00, M: 0xC0}
	Exec(&s, inst.BIT_M, 0)
	if s.P&FlagN == 0 {
		t.Fatal("BIT: N not set from M bit 7")
	}
	if s.P&FlagV == 0 {
		t.Fatal("BIT: V not set from M bit 6")
	}
	if s.P&FlagZ == 0 {
		t.Fatal("BIT: Z not set (A&M==0)")
	}

	// BIT M where M=0x0F, A=0x0F → N=0, V=0, Z=0
	s = State{A: 0x0F, M: 0x0F}
	Exec(&s, inst.BIT_M, 0)
	if s.P&FlagN != 0 {
		t.Fatal("BIT: N set incorrectly")
	}
	if s.P&FlagV != 0 {
		t.Fatal("BIT: V set incorrectly")
	}
	if s.P&FlagZ != 0 {
		t.Fatal("BIT: Z set incorrectly (A&M != 0)")
	}
}

func TestMemoryOps(t *testing.T) {
	// LDA M
	s := State{M: 0x42}
	Exec(&s, inst.LDA_M, 0)
	if s.A != 0x42 {
		t.Fatalf("LDA M: A=%02X want 42", s.A)
	}

	// STA M
	s = State{A: 0x55}
	Exec(&s, inst.STA_M, 0)
	if s.M != 0x55 {
		t.Fatalf("STA M: M=%02X want 55", s.M)
	}

	// INC M
	s = State{M: 0xFF}
	Exec(&s, inst.INC_M, 0)
	if s.M != 0x00 {
		t.Fatalf("INC M: M=%02X want 00", s.M)
	}
	if s.P&FlagZ == 0 {
		t.Fatal("INC M: Z not set")
	}

	// DEC M
	s = State{M: 0x00}
	Exec(&s, inst.DEC_M, 0)
	if s.M != 0xFF {
		t.Fatalf("DEC M: M=%02X want FF", s.M)
	}
	if s.P&FlagN == 0 {
		t.Fatal("DEC M: N not set")
	}

	// ASL M
	s = State{M: 0x81}
	Exec(&s, inst.ASL_M, 0)
	if s.M != 0x02 {
		t.Fatalf("ASL M: M=%02X want 02", s.M)
	}
	if s.P&FlagC == 0 {
		t.Fatal("ASL M: C not set")
	}
}

func TestFlagOps(t *testing.T) {
	s := State{P: 0xFF}
	Exec(&s, inst.CLC, 0)
	if s.P&FlagC != 0 {
		t.Fatal("CLC: C still set")
	}
	Exec(&s, inst.SEC, 0)
	if s.P&FlagC == 0 {
		t.Fatal("SEC: C not set")
	}
	Exec(&s, inst.CLV, 0)
	if s.P&FlagV != 0 {
		t.Fatal("CLV: V still set")
	}
	Exec(&s, inst.CLD, 0)
	if s.P&FlagD != 0 {
		t.Fatal("CLD: D still set")
	}
	Exec(&s, inst.SED, 0)
	if s.P&FlagD == 0 {
		t.Fatal("SED: D not set")
	}
}

func TestStackDepth(t *testing.T) {
	// Push 2 values, pull them back in LIFO order
	s := State{A: 0x11, S: 0xFF}
	Exec(&s, inst.PHA, 0) // push 0x11
	s.A = 0x22
	Exec(&s, inst.PHA, 0) // push 0x22
	if s.S != 0xFD {
		t.Fatalf("after 2 pushes: S=%02X want FD", s.S)
	}

	Exec(&s, inst.PLA, 0) // pull → should be 0x22
	if s.A != 0x22 {
		t.Fatalf("first PLA: A=%02X want 22", s.A)
	}
	Exec(&s, inst.PLA, 0) // pull → should be 0x11
	if s.A != 0x11 {
		t.Fatalf("second PLA: A=%02X want 11", s.A)
	}
	if s.S != 0xFF {
		t.Fatalf("after 2 pulls: S=%02X want FF", s.S)
	}
}

func TestImmediateLoads(t *testing.T) {
	s := State{}
	Exec(&s, inst.LDA_IMM, 0x00)
	if s.A != 0x00 || s.P&FlagZ == 0 {
		t.Fatal("LDA #0: Z not set")
	}
	Exec(&s, inst.LDA_IMM, 0x80)
	if s.A != 0x80 || s.P&FlagN == 0 {
		t.Fatal("LDA #$80: N not set")
	}
	Exec(&s, inst.LDX_IMM, 0xFF)
	if s.X != 0xFF || s.P&FlagN == 0 {
		t.Fatal("LDX #$FF: N not set")
	}
	Exec(&s, inst.LDY_IMM, 0x01)
	if s.Y != 0x01 || s.P&(FlagN|FlagZ) != 0 {
		t.Fatal("LDY #$01: unexpected flags")
	}
}

func BenchmarkExec(b *testing.B) {
	s := State{A: 0x42, X: 0x10, Y: 0x20, P: FlagC}
	for i := 0; i < b.N; i++ {
		Exec(&s, inst.ADC_IMM, uint16(i&0xFF))
	}
}
