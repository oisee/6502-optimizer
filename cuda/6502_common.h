// 6502 common definitions — shared between CUDA kernels and host code.
// Adapted from z80_common.h for the MOS 6502 CPU.
#pragma once

#include <cstdint>
#include <cstdio>
#include <cstring>

// ============================================================
// 6502 flag bits (P register: NV-BDIZC)
// ============================================================
#define FLAG_C  0x01u  // Carry
#define FLAG_Z  0x02u  // Zero
#define FLAG_I  0x04u  // Interrupt disable
#define FLAG_D  0x08u  // Decimal mode
#define FLAG_B  0x10u  // Break (artifact of push)
#define FLAG_U  0x20u  // Unused (always 1 on push)
#define FLAG_V  0x40u  // Overflow
#define FLAG_N  0x80u  // Negative

// PMask: mask out bits 4 (B) and 5 (U) for comparison.
// On the real 6502 these bits are artifacts of how P is pushed to stack.
#define PMASK   0xCFu  // 11001111 — keep N,V,D,I,Z,C

// ============================================================
// 6502 State (8 bytes)
// ============================================================
struct State6502 {
    uint8_t A, X, Y, P;
    uint8_t S;      // stack pointer
    uint8_t M;      // virtual zero-page memory byte
    uint8_t S0, S1; // virtual stack slots (2-deep)
};

// ============================================================
// Opcode constants (from Go iota enum, 58 total)
// ============================================================
// Implied instructions (26 opcodes, 1 byte each)
#define OP_TAX      0
#define OP_TAY      1
#define OP_TXA      2
#define OP_TYA      3
#define OP_TSX      4
#define OP_TXS      5
#define OP_INX      6
#define OP_INY      7
#define OP_DEX      8
#define OP_DEY      9
#define OP_ASL_A   10
#define OP_LSR_A   11
#define OP_ROL_A   12
#define OP_ROR_A   13
#define OP_CLC     14
#define OP_SEC     15
#define OP_CLD     16
#define OP_SED     17
#define OP_CLI     18
#define OP_SEI     19
#define OP_CLV     20
#define OP_PHA     21
#define OP_PLA     22
#define OP_PHP     23
#define OP_PLP     24
#define OP_NOP     25
// Immediate instructions (11 opcodes, 2 bytes each)
#define OP_LDA_IMM 26
#define OP_LDX_IMM 27
#define OP_LDY_IMM 28
#define OP_ADC_IMM 29
#define OP_SBC_IMM 30
#define OP_AND_IMM 31
#define OP_ORA_IMM 32
#define OP_EOR_IMM 33
#define OP_CMP_IMM 34
#define OP_CPX_IMM 35
#define OP_CPY_IMM 36
// Zero-page memory / virtual M instructions (21 opcodes, 2 bytes each)
#define OP_LDA_M   37
#define OP_LDX_M   38
#define OP_LDY_M   39
#define OP_STA_M   40
#define OP_STX_M   41
#define OP_STY_M   42
#define OP_ADC_M   43
#define OP_SBC_M   44
#define OP_AND_M   45
#define OP_ORA_M   46
#define OP_EOR_M   47
#define OP_CMP_M   48
#define OP_CPX_M   49
#define OP_CPY_M   50
#define OP_INC_M   51
#define OP_DEC_M   52
#define OP_ASL_M   53
#define OP_LSR_M   54
#define OP_ROL_M   55
#define OP_ROR_M   56
#define OP_BIT_M   57
#define OP_COUNT   58

// ============================================================
// Fingerprint constants (QuickCheck — 8 vectors)
// ============================================================
#define FP_SIZE     8   // bytes per state snapshot (A,X,Y,P&PMASK,S,M,S0,S1)
#define NUM_VECTORS 8
#define FP_LEN      (FP_SIZE * NUM_VECTORS)  // 64

// ============================================================
// Test vectors (8 fixed inputs for QuickCheck, same as Go TestVectors)
// Fields: A, X, Y, P, S, M, S0, S1
// ============================================================
static const State6502 h_test_vectors[8] = {
    {0x00, 0x00, 0x00, 0x00, 0xFD, 0x42, 0x00, 0x00},
    {0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xBD, 0xFF, 0xFF},
    {0x01, 0x02, 0x03, 0x00, 0xFD, 0x13, 0x10, 0x20},
    {0x80, 0x40, 0x20, 0x01, 0xFC, 0x7E, 0x30, 0x40},
    {0x55, 0xAA, 0x55, 0x00, 0xFE, 0xA5, 0x55, 0xAA},
    {0xAA, 0x55, 0xAA, 0x01, 0xFD, 0x5A, 0xAA, 0x55},
    {0x0F, 0xF0, 0x0F, 0x00, 0xFB, 0xE1, 0x0F, 0xF0},
    {0x7F, 0x80, 0x7F, 0x01, 0xFD, 0x33, 0x80, 0x7F},
};

// ============================================================
// NZ Table (host — only flag table needed for 6502)
// ============================================================
static uint8_t h_nz_table[256];

static void init_tables() {
    for (int i = 0; i < 256; i++) {
        uint8_t f = 0;
        if (i == 0) f |= FLAG_Z;
        if (i & 0x80) f |= FLAG_N;
        h_nz_table[i] = f;
    }
}

// ============================================================
// Host-side ALU helpers (match Go exec.go exactly)
// ============================================================
static inline void h_set_nz(State6502 &s, uint8_t val) {
    s.P = (s.P & ~(FLAG_N | FLAG_Z)) | h_nz_table[val];
}

// ADC: result = A + operand + C, V = (A^result) & (operand^result) & 0x80
static void h_exec_adc(State6502 &s, uint8_t operand) {
    uint16_t a = s.A, m = operand, c = s.P & FLAG_C;
    uint16_t result = a + m + c;
    uint8_t v = ((uint8_t)a ^ (uint8_t)result) & (operand ^ (uint8_t)result) & 0x80;
    s.A = (uint8_t)result;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N | FLAG_V);
    s.P |= h_nz_table[s.A];
    if (result > 0xFF) s.P |= FLAG_C;
    if (v) s.P |= FLAG_V;
}

// SBC: result = A - operand - (1-C), carry set when no borrow
static void h_exec_sbc(State6502 &s, uint8_t operand) {
    uint16_t a = s.A, m = operand, c = s.P & FLAG_C;
    uint16_t result = a - m - (1 - c);
    uint8_t v = (s.A ^ (uint8_t)result) & (s.A ^ operand) & 0x80;
    s.A = (uint8_t)result;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N | FLAG_V);
    s.P |= h_nz_table[s.A];
    if (result <= 0xFF) s.P |= FLAG_C;  // no borrow = carry set
    if (v) s.P |= FLAG_V;
}

// CMP/CPX/CPY: set N,Z,C without modifying register
static void h_exec_cmp(State6502 &s, uint8_t reg, uint8_t operand) {
    uint16_t result = (uint16_t)reg - operand;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N);
    s.P |= h_nz_table[(uint8_t)result];
    if (reg >= operand) s.P |= FLAG_C;
}

// ASL: C = old bit 7, then shift left
static void h_exec_asl(State6502 &s, uint8_t *val) {
    s.P &= ~FLAG_C;
    if (*val & 0x80) s.P |= FLAG_C;
    *val <<= 1;
    h_set_nz(s, *val);
}

// LSR: C = old bit 0, then shift right
static void h_exec_lsr(State6502 &s, uint8_t *val) {
    s.P &= ~FLAG_C;
    if (*val & 0x01) s.P |= FLAG_C;
    *val >>= 1;
    h_set_nz(s, *val);
}

// ROL: rotate left through carry
static void h_exec_rol(State6502 &s, uint8_t *val) {
    uint8_t oldC = s.P & FLAG_C;
    s.P &= ~FLAG_C;
    if (*val & 0x80) s.P |= FLAG_C;
    *val = (*val << 1) | oldC;
    h_set_nz(s, *val);
}

// ROR: rotate right through carry
static void h_exec_ror(State6502 &s, uint8_t *val) {
    uint8_t oldC = s.P & FLAG_C;
    s.P &= ~FLAG_C;
    if (*val & 0x01) s.P |= FLAG_C;
    *val = (*val >> 1) | (oldC << 7);
    h_set_nz(s, *val);
}

// BIT: N=M[7], V=M[6], Z=(A&M)==0 — flags from operand, not result
static void h_exec_bit(State6502 &s, uint8_t operand) {
    s.P &= ~(FLAG_N | FLAG_V | FLAG_Z);
    if (operand & 0x80) s.P |= FLAG_N;
    if (operand & 0x40) s.P |= FLAG_V;
    if ((s.A & operand) == 0) s.P |= FLAG_Z;
}

// push: S1=S0, S0=val, S--
static void h_push(State6502 &s, uint8_t val) {
    s.S1 = s.S0;
    s.S0 = val;
    s.S--;
}

// pull: val=S0, S0=S1, S1=0, S++
static uint8_t h_pull(State6502 &s) {
    uint8_t val = s.S0;
    s.S0 = s.S1;
    s.S1 = 0;
    s.S++;
    return val;
}

// ============================================================
// Host-side CPU executor (full, matches Go Exec bit-exact)
// ============================================================
static void h_exec_instruction(State6502 &s, uint16_t op, uint16_t imm) {
    switch (op) {
    // Register transfers
    case OP_TAX: s.X = s.A; h_set_nz(s, s.X); return;
    case OP_TAY: s.Y = s.A; h_set_nz(s, s.Y); return;
    case OP_TXA: s.A = s.X; h_set_nz(s, s.A); return;
    case OP_TYA: s.A = s.Y; h_set_nz(s, s.A); return;
    case OP_TSX: s.X = s.S; h_set_nz(s, s.X); return;
    case OP_TXS: s.S = s.X; return;  // TXS does NOT affect flags
    // Inc/Dec registers
    case OP_INX: s.X++; h_set_nz(s, s.X); return;
    case OP_INY: s.Y++; h_set_nz(s, s.Y); return;
    case OP_DEX: s.X--; h_set_nz(s, s.X); return;
    case OP_DEY: s.Y--; h_set_nz(s, s.Y); return;
    // Shifts on accumulator
    case OP_ASL_A: h_exec_asl(s, &s.A); return;
    case OP_LSR_A: h_exec_lsr(s, &s.A); return;
    case OP_ROL_A: h_exec_rol(s, &s.A); return;
    case OP_ROR_A: h_exec_ror(s, &s.A); return;
    // Flag operations
    case OP_CLC: s.P &= ~FLAG_C; return;
    case OP_SEC: s.P |= FLAG_C; return;
    case OP_CLD: s.P &= ~FLAG_D; return;
    case OP_SED: s.P |= FLAG_D; return;
    case OP_CLI: s.P &= ~FLAG_I; return;
    case OP_SEI: s.P |= FLAG_I; return;
    case OP_CLV: s.P &= ~FLAG_V; return;
    // Stack operations
    case OP_PHA: h_push(s, s.A); return;
    case OP_PLA: s.A = h_pull(s); h_set_nz(s, s.A); return;
    case OP_PHP: h_push(s, s.P | FLAG_B | FLAG_U); return;
    case OP_PLP: s.P = h_pull(s); return;
    // NOP
    case OP_NOP: return;
    // Immediate loads
    case OP_LDA_IMM: s.A = (uint8_t)imm; h_set_nz(s, s.A); return;
    case OP_LDX_IMM: s.X = (uint8_t)imm; h_set_nz(s, s.X); return;
    case OP_LDY_IMM: s.Y = (uint8_t)imm; h_set_nz(s, s.Y); return;
    // Immediate ALU
    case OP_ADC_IMM: h_exec_adc(s, (uint8_t)imm); return;
    case OP_SBC_IMM: h_exec_sbc(s, (uint8_t)imm); return;
    case OP_AND_IMM: s.A &= (uint8_t)imm; h_set_nz(s, s.A); return;
    case OP_ORA_IMM: s.A |= (uint8_t)imm; h_set_nz(s, s.A); return;
    case OP_EOR_IMM: s.A ^= (uint8_t)imm; h_set_nz(s, s.A); return;
    // Immediate compares
    case OP_CMP_IMM: h_exec_cmp(s, s.A, (uint8_t)imm); return;
    case OP_CPX_IMM: h_exec_cmp(s, s.X, (uint8_t)imm); return;
    case OP_CPY_IMM: h_exec_cmp(s, s.Y, (uint8_t)imm); return;
    // Memory loads
    case OP_LDA_M: s.A = s.M; h_set_nz(s, s.A); return;
    case OP_LDX_M: s.X = s.M; h_set_nz(s, s.X); return;
    case OP_LDY_M: s.Y = s.M; h_set_nz(s, s.Y); return;
    // Memory stores
    case OP_STA_M: s.M = s.A; return;
    case OP_STX_M: s.M = s.X; return;
    case OP_STY_M: s.M = s.Y; return;
    // Memory ALU
    case OP_ADC_M: h_exec_adc(s, s.M); return;
    case OP_SBC_M: h_exec_sbc(s, s.M); return;
    case OP_AND_M: s.A &= s.M; h_set_nz(s, s.A); return;
    case OP_ORA_M: s.A |= s.M; h_set_nz(s, s.A); return;
    case OP_EOR_M: s.A ^= s.M; h_set_nz(s, s.A); return;
    // Memory compares
    case OP_CMP_M: h_exec_cmp(s, s.A, s.M); return;
    case OP_CPX_M: h_exec_cmp(s, s.X, s.M); return;
    case OP_CPY_M: h_exec_cmp(s, s.Y, s.M); return;
    // Memory inc/dec
    case OP_INC_M: s.M++; h_set_nz(s, s.M); return;
    case OP_DEC_M: s.M--; h_set_nz(s, s.M); return;
    // Memory shifts
    case OP_ASL_M: h_exec_asl(s, &s.M); return;
    case OP_LSR_M: h_exec_lsr(s, &s.M); return;
    case OP_ROL_M: h_exec_rol(s, &s.M); return;
    case OP_ROR_M: h_exec_ror(s, &s.M); return;
    // BIT test
    case OP_BIT_M: h_exec_bit(s, s.M); return;
    }
}

// Execute a sequence of instructions.
static void h_exec_seq(State6502 &s, const uint16_t* ops, const uint16_t* imms, int n) {
    for (int i = 0; i < n; i++) h_exec_instruction(s, ops[i], imms[i]);
}

// Compute fingerprint for a sequence (8 QC vectors).
static void h_fingerprint(const uint16_t* ops, const uint16_t* imms, int n, uint8_t fp[FP_LEN]) {
    for (int v = 0; v < NUM_VECTORS; v++) {
        State6502 s = h_test_vectors[v];
        h_exec_seq(s, ops, imms, n);
        int off = v * FP_SIZE;
        fp[off+0] = s.A;
        fp[off+1] = s.X;
        fp[off+2] = s.Y;
        fp[off+3] = s.P & PMASK;
        fp[off+4] = s.S;
        fp[off+5] = s.M;
        fp[off+6] = s.S0;
        fp[off+7] = s.S1;
    }
}

// ============================================================
// MidCheck vectors (24 additional test vectors for Stage 2)
// These are vectors 8-31 from Go MidCheckVectors.
// Vectors 0-7 are the same as TestVectors (already checked by QC).
// ============================================================
#define MID_VECTORS 24
#define MID_FP_SIZE 8
#define MID_FP_LEN  (MID_FP_SIZE * MID_VECTORS)  // 192

static const State6502 h_mid_vectors[MID_VECTORS] = {
    // Single-bit A values (Go indices 8-15)
    {0x02, 0x01, 0x04, 0x00, 0xFD, 0x91, 0x11, 0x22},
    {0x04, 0x08, 0x10, 0x01, 0xFD, 0x48, 0x33, 0x44},
    {0x08, 0x10, 0x20, 0x00, 0xFD, 0x24, 0x55, 0x66},
    {0x10, 0x20, 0x40, 0x01, 0xFD, 0xD7, 0x77, 0x88},
    {0x20, 0x40, 0x80, 0x00, 0xFD, 0x6B, 0x99, 0xBB},
    {0x40, 0x80, 0x01, 0x01, 0xFD, 0xB2, 0xCC, 0xDD},
    {0x03, 0x05, 0x09, 0x00, 0xFD, 0x0F, 0xEE, 0x11},
    {0x05, 0x0A, 0x14, 0x01, 0xFD, 0xF0, 0x22, 0x33},
    // Boundary values (Go indices 16-23)
    {0xFE, 0x7F, 0x80, 0x00, 0xFD, 0x01, 0x44, 0x55},
    {0x81, 0xFE, 0x01, 0x01, 0xFD, 0xFE, 0x66, 0x77},
    {0x7E, 0x81, 0x42, 0x00, 0xFD, 0x80, 0x88, 0x99},
    {0xBF, 0x40, 0xBF, 0x01, 0xFD, 0x40, 0xAA, 0xBB},
    {0xC0, 0x3F, 0xC0, 0x00, 0xFD, 0x1F, 0xCC, 0xDD},
    {0xE0, 0x1F, 0xE0, 0x01, 0xFD, 0xEF, 0xEE, 0x00},
    {0xF0, 0x0F, 0xF0, 0x00, 0xFD, 0x55, 0x11, 0x22},
    {0x0E, 0xE0, 0x0E, 0x01, 0xFD, 0xAA, 0x33, 0x44},
    // Stack-exercising (Go indices 24-31)
    {0x11, 0x22, 0x33, 0x00, 0xFE, 0xC3, 0x00, 0x00},
    {0x22, 0x33, 0x44, 0x01, 0xFF, 0x3C, 0x55, 0x66},
    {0x44, 0x55, 0x66, 0x00, 0xFC, 0x87, 0x77, 0x88},
    {0x88, 0x99, 0xAA, 0x01, 0xFB, 0x69, 0x99, 0xAA},
    {0x33, 0x44, 0x55, 0x00, 0xFD, 0xAE, 0xBB, 0xCC},
    {0xCC, 0xDD, 0xEE, 0x01, 0xFD, 0x51, 0xDD, 0xEE},
    {0x66, 0x77, 0x88, 0x00, 0xFD, 0xDA, 0x12, 0x34},
    {0x99, 0xAA, 0xBB, 0x01, 0xFD, 0x25, 0x56, 0x78},
};

// Compute MidCheck fingerprint for a sequence (24 additional vectors).
static void h_mid_fingerprint(const uint16_t* ops, const uint16_t* imms, int n, uint8_t mfp[MID_FP_LEN]) {
    for (int v = 0; v < MID_VECTORS; v++) {
        State6502 s = h_mid_vectors[v];
        h_exec_seq(s, ops, imms, n);
        int off = v * MID_FP_SIZE;
        mfp[off+0] = s.A;
        mfp[off+1] = s.X;
        mfp[off+2] = s.Y;
        mfp[off+3] = s.P & PMASK;
        mfp[off+4] = s.S;
        mfp[off+5] = s.M;
        mfp[off+6] = s.S0;
        mfp[off+7] = s.S1;
    }
}

// Compare states, optionally masking dead flags.
// Effective P mask = PMASK & ~dead_flags.
static bool h_states_equal(const State6502 &a, const State6502 &b, uint8_t dead_flags) {
    uint8_t pm = PMASK & ~dead_flags;
    return a.A == b.A && a.X == b.X && a.Y == b.Y &&
           (a.P & pm) == (b.P & pm) &&
           a.S == b.S && a.M == b.M &&
           a.S0 == b.S0 && a.S1 == b.S1;
}

// MidCheck: run target and candidate on all 24 additional vectors, compare.
static bool h_midcheck(
    const uint16_t* t_ops, const uint16_t* t_imms, int t_n,
    const uint16_t* c_ops, const uint16_t* c_imms, int c_n,
    uint8_t dead_flags
) {
    for (int v = 0; v < MID_VECTORS; v++) {
        State6502 st = h_mid_vectors[v], sc = h_mid_vectors[v];
        h_exec_seq(st, t_ops, t_imms, t_n);
        h_exec_seq(sc, c_ops, c_imms, c_n);
        if (!h_states_equal(st, sc, dead_flags)) return false;
    }
    return true;
}
