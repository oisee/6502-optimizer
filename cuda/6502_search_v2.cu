// 6502 Standalone GPU Superoptimizer Search — v2 Batched Pipeline
//
// 3-stage GPU pipeline:
//   Stage 1: Batched QuickCheck (512 targets x N candidates, 8 test vectors)
//   Stage 2: MidCheck (survivors only, 24 additional test vectors)
//   Stage 3: GPU ExhaustiveCheck (256 threads/block, full input sweep)
//
// Build: nvcc -O2 -o 6502search_v2 6502_search_v2.cu
// Usage: ./6502search_v2 --max-target 2 [--dead-flags 0xFF] [--gpu-id N]
//                        [--first-op-start M] [--first-op-end N]
//
// Output: JSONL to stdout (one result per line)
// Progress: stderr

#include <cstdlib>
#include <cstdio>
#include <cstdint>
#include <cstring>
#include <ctime>
#include <vector>
#include <algorithm>

#include "6502_common.h"

// ============================================================
// Pipeline tuning constants
// ============================================================
#define BATCH_SIZE     512    // targets per batch
#define BLOCK_SIZE     256    // threads per CUDA block
#define EXHAUST_BLOCK  256    // threads per ExhaustiveCheck block (one per A value)

// Bitmap: ceil(max_candidates / 32) words per target
// max_candidates ~ 2863 for 6502 -> 90 words
#define MAX_CAND_WORDS 90

// ============================================================
// CUDA constants (device memory)
// ============================================================
__constant__ uint8_t d_nz_table[256];
__constant__ State6502 d_test_vectors[8];
__constant__ State6502 d_mid_vectors[MID_VECTORS];

// Representative values for GPU exhaustive reduced sweep
__device__ __constant__ uint8_t d_rep_values[32] = {
    0x00, 0x01, 0x02, 0x0F, 0x10, 0x1F, 0x20, 0x3F,
    0x40, 0x55, 0x7E, 0x7F, 0x80, 0x81, 0xAA, 0xBF,
    0xC0, 0xD5, 0xE0, 0xEF, 0xF0, 0xF7, 0xFE, 0xFF,
    0x03, 0x07, 0x11, 0x33, 0x77, 0xBB, 0xDD, 0xEE,
};

static void upload_tables_cuda() {
    cudaMemcpyToSymbol(d_nz_table, h_nz_table, 256);
    cudaMemcpyToSymbol(d_test_vectors, h_test_vectors, sizeof(h_test_vectors));
    cudaMemcpyToSymbol(d_mid_vectors, h_mid_vectors, sizeof(h_mid_vectors));
}

// ============================================================
// GPU device-side ALU helpers
// ============================================================
__device__ inline void d_set_nz(State6502 &s, uint8_t val) {
    s.P = (s.P & ~(FLAG_N | FLAG_Z)) | d_nz_table[val];
}

__device__ void d_exec_adc(State6502 &s, uint8_t operand) {
    uint16_t a = s.A, m = operand, c = s.P & FLAG_C;
    uint16_t result = a + m + c;
    uint8_t v = ((uint8_t)a ^ (uint8_t)result) & (operand ^ (uint8_t)result) & 0x80;
    s.A = (uint8_t)result;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N | FLAG_V);
    s.P |= d_nz_table[s.A];
    if (result > 0xFF) s.P |= FLAG_C;
    if (v) s.P |= FLAG_V;
}

__device__ void d_exec_sbc(State6502 &s, uint8_t operand) {
    uint16_t a = s.A, m = operand, c = s.P & FLAG_C;
    uint16_t result = a - m - (1 - c);
    uint8_t v = (s.A ^ (uint8_t)result) & (s.A ^ operand) & 0x80;
    s.A = (uint8_t)result;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N | FLAG_V);
    s.P |= d_nz_table[s.A];
    if (result <= 0xFF) s.P |= FLAG_C;
    if (v) s.P |= FLAG_V;
}

__device__ void d_exec_cmp(State6502 &s, uint8_t reg, uint8_t operand) {
    uint16_t result = (uint16_t)reg - operand;
    s.P &= ~(FLAG_C | FLAG_Z | FLAG_N);
    s.P |= d_nz_table[(uint8_t)result];
    if (reg >= operand) s.P |= FLAG_C;
}

__device__ void d_exec_asl(State6502 &s, uint8_t *val) {
    s.P &= ~FLAG_C;
    if (*val & 0x80) s.P |= FLAG_C;
    *val <<= 1;
    d_set_nz(s, *val);
}

__device__ void d_exec_lsr(State6502 &s, uint8_t *val) {
    s.P &= ~FLAG_C;
    if (*val & 0x01) s.P |= FLAG_C;
    *val >>= 1;
    d_set_nz(s, *val);
}

__device__ void d_exec_rol(State6502 &s, uint8_t *val) {
    uint8_t oldC = s.P & FLAG_C;
    s.P &= ~FLAG_C;
    if (*val & 0x80) s.P |= FLAG_C;
    *val = (*val << 1) | oldC;
    d_set_nz(s, *val);
}

__device__ void d_exec_ror(State6502 &s, uint8_t *val) {
    uint8_t oldC = s.P & FLAG_C;
    s.P &= ~FLAG_C;
    if (*val & 0x01) s.P |= FLAG_C;
    *val = (*val >> 1) | (oldC << 7);
    d_set_nz(s, *val);
}

__device__ void d_exec_bit(State6502 &s, uint8_t operand) {
    s.P &= ~(FLAG_N | FLAG_V | FLAG_Z);
    if (operand & 0x80) s.P |= FLAG_N;
    if (operand & 0x40) s.P |= FLAG_V;
    if ((s.A & operand) == 0) s.P |= FLAG_Z;
}

__device__ void d_push(State6502 &s, uint8_t val) {
    s.S1 = s.S0; s.S0 = val; s.S--;
}

__device__ uint8_t d_pull(State6502 &s) {
    uint8_t val = s.S0; s.S0 = s.S1; s.S1 = 0; s.S++; return val;
}

// ============================================================
// GPU device-side CPU executor
// ============================================================
__device__ void exec_instruction(State6502 &s, uint16_t op, uint16_t imm) {
    switch (op) {
    case OP_TAX: s.X = s.A; d_set_nz(s, s.X); return;
    case OP_TAY: s.Y = s.A; d_set_nz(s, s.Y); return;
    case OP_TXA: s.A = s.X; d_set_nz(s, s.A); return;
    case OP_TYA: s.A = s.Y; d_set_nz(s, s.A); return;
    case OP_TSX: s.X = s.S; d_set_nz(s, s.X); return;
    case OP_TXS: s.S = s.X; return;
    case OP_INX: s.X++; d_set_nz(s, s.X); return;
    case OP_INY: s.Y++; d_set_nz(s, s.Y); return;
    case OP_DEX: s.X--; d_set_nz(s, s.X); return;
    case OP_DEY: s.Y--; d_set_nz(s, s.Y); return;
    case OP_ASL_A: d_exec_asl(s, &s.A); return;
    case OP_LSR_A: d_exec_lsr(s, &s.A); return;
    case OP_ROL_A: d_exec_rol(s, &s.A); return;
    case OP_ROR_A: d_exec_ror(s, &s.A); return;
    case OP_CLC: s.P &= ~FLAG_C; return;
    case OP_SEC: s.P |= FLAG_C; return;
    case OP_CLD: s.P &= ~FLAG_D; return;
    case OP_SED: s.P |= FLAG_D; return;
    case OP_CLI: s.P &= ~FLAG_I; return;
    case OP_SEI: s.P |= FLAG_I; return;
    case OP_CLV: s.P &= ~FLAG_V; return;
    case OP_PHA: d_push(s, s.A); return;
    case OP_PLA: s.A = d_pull(s); d_set_nz(s, s.A); return;
    case OP_PHP: d_push(s, s.P | FLAG_B | FLAG_U); return;
    case OP_PLP: s.P = d_pull(s); return;
    case OP_NOP: return;
    case OP_LDA_IMM: s.A = (uint8_t)imm; d_set_nz(s, s.A); return;
    case OP_LDX_IMM: s.X = (uint8_t)imm; d_set_nz(s, s.X); return;
    case OP_LDY_IMM: s.Y = (uint8_t)imm; d_set_nz(s, s.Y); return;
    case OP_ADC_IMM: d_exec_adc(s, (uint8_t)imm); return;
    case OP_SBC_IMM: d_exec_sbc(s, (uint8_t)imm); return;
    case OP_AND_IMM: s.A &= (uint8_t)imm; d_set_nz(s, s.A); return;
    case OP_ORA_IMM: s.A |= (uint8_t)imm; d_set_nz(s, s.A); return;
    case OP_EOR_IMM: s.A ^= (uint8_t)imm; d_set_nz(s, s.A); return;
    case OP_CMP_IMM: d_exec_cmp(s, s.A, (uint8_t)imm); return;
    case OP_CPX_IMM: d_exec_cmp(s, s.X, (uint8_t)imm); return;
    case OP_CPY_IMM: d_exec_cmp(s, s.Y, (uint8_t)imm); return;
    case OP_LDA_M: s.A = s.M; d_set_nz(s, s.A); return;
    case OP_LDX_M: s.X = s.M; d_set_nz(s, s.X); return;
    case OP_LDY_M: s.Y = s.M; d_set_nz(s, s.Y); return;
    case OP_STA_M: s.M = s.A; return;
    case OP_STX_M: s.M = s.X; return;
    case OP_STY_M: s.M = s.Y; return;
    case OP_ADC_M: d_exec_adc(s, s.M); return;
    case OP_SBC_M: d_exec_sbc(s, s.M); return;
    case OP_AND_M: s.A &= s.M; d_set_nz(s, s.A); return;
    case OP_ORA_M: s.A |= s.M; d_set_nz(s, s.A); return;
    case OP_EOR_M: s.A ^= s.M; d_set_nz(s, s.A); return;
    case OP_CMP_M: d_exec_cmp(s, s.A, s.M); return;
    case OP_CPX_M: d_exec_cmp(s, s.X, s.M); return;
    case OP_CPY_M: d_exec_cmp(s, s.Y, s.M); return;
    case OP_INC_M: s.M++; d_set_nz(s, s.M); return;
    case OP_DEC_M: s.M--; d_set_nz(s, s.M); return;
    case OP_ASL_M: d_exec_asl(s, &s.M); return;
    case OP_LSR_M: d_exec_lsr(s, &s.M); return;
    case OP_ROL_M: d_exec_rol(s, &s.M); return;
    case OP_ROR_M: d_exec_ror(s, &s.M); return;
    case OP_BIT_M: d_exec_bit(s, s.M); return;
    }
}

// Device state comparison
__device__ bool d_states_equal(const State6502 &a, const State6502 &b, uint8_t dead_flags) {
    uint8_t pm = PMASK & ~dead_flags;
    return a.A == b.A && a.X == b.X && a.Y == b.Y &&
           (a.P & pm) == (b.P & pm) &&
           a.S == b.S && a.M == b.M &&
           a.S0 == b.S0 && a.S1 == b.S1;
}

// Extra reg offsets: 1=X, 2=Y, 3=M, 4=S, 5=S0, 6=S1
__device__ void d_set_reg_by_offset(State6502 &s, int offset, uint8_t val) {
    switch (offset) {
        case 1: s.X = val; break;  case 2: s.Y = val; break;
        case 3: s.M = val; break;  case 4: s.S = val; break;
        case 5: s.S0 = val; break; case 6: s.S1 = val; break;
    }
}

// ============================================================
// KERNEL 1: Batched QuickCheck
// One thread per (target, candidate) pair.
// Output: bitmap with atomicOr, one bit per candidate per target.
// ============================================================
__global__ void quickcheck_batched(
    const uint32_t* __restrict__ candidates,
    const uint8_t*  __restrict__ target_fps,
    uint32_t*       __restrict__ hit_bitmap,
    uint32_t cand_count,
    uint32_t batch_count,
    uint32_t bitmap_words,
    uint32_t dead_flags
) {
    uint32_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    uint32_t total_threads = batch_count * cand_count;
    if (tid >= total_threads) return;

    uint32_t target_idx = tid / cand_count;
    uint32_t cand_idx   = tid % cand_count;

    uint32_t packed = candidates[cand_idx];
    uint16_t c_op  = (uint16_t)(packed & 0xFFFF);
    uint16_t c_imm = (uint16_t)(packed >> 16);

    const uint8_t* tfp = target_fps + target_idx * FP_LEN;
    uint8_t fm = (uint8_t)dead_flags;

    for (int v = 0; v < NUM_VECTORS; v++) {
        State6502 s = d_test_vectors[v];
        exec_instruction(s, c_op, c_imm);
        int off = v * FP_SIZE;
        // P comparison: both sides already PMASK'd, then apply dead_flags
        uint8_t cp = s.P & PMASK, tp = tfp[off+3];
        if (fm) { cp &= ~fm; tp &= ~fm; }
        if (s.A != tfp[off+0] || s.X != tfp[off+1] || s.Y != tfp[off+2] || cp != tp ||
            s.S != tfp[off+4] || s.M != tfp[off+5] ||
            s.S0 != tfp[off+6] || s.S1 != tfp[off+7])
            return;
    }
    // All vectors match
    uint32_t word = cand_idx / 32;
    uint32_t bit  = cand_idx % 32;
    atomicOr(&hit_bitmap[target_idx * bitmap_words + word], 1u << bit);
}

// ============================================================
// KERNEL 2: MidCheck — 24 additional test vectors
// ============================================================
struct MidPair {
    uint16_t target_idx;
    uint16_t cand_idx;
};

__global__ void midcheck_kernel(
    const MidPair*  __restrict__ pairs,
    uint32_t pair_count,
    const uint32_t* __restrict__ candidates,
    const uint8_t*  __restrict__ target_mid_fps,
    uint32_t*       __restrict__ survived,
    uint32_t dead_flags
) {
    uint32_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    if (tid >= pair_count) return;

    MidPair p = pairs[tid];
    uint32_t packed = candidates[p.cand_idx];
    uint16_t c_op  = (uint16_t)(packed & 0xFFFF);
    uint16_t c_imm = (uint16_t)(packed >> 16);

    const uint8_t* tmfp = target_mid_fps + p.target_idx * MID_FP_LEN;
    uint8_t fm = (uint8_t)dead_flags;

    for (int v = 0; v < MID_VECTORS; v++) {
        State6502 s = d_mid_vectors[v];
        exec_instruction(s, c_op, c_imm);
        int off = v * MID_FP_SIZE;
        uint8_t cp = s.P & PMASK, tp = tmfp[off+3];
        if (fm) { cp &= ~fm; tp &= ~fm; }
        if (s.A != tmfp[off+0] || s.X != tmfp[off+1] || s.Y != tmfp[off+2] || cp != tp ||
            s.S != tmfp[off+4] || s.M != tmfp[off+5] ||
            s.S0 != tmfp[off+6] || s.S1 != tmfp[off+7]) {
            survived[tid] = 0;
            return;
        }
    }
    survived[tid] = 1;
}

// ============================================================
// KERNEL 3: GPU ExhaustiveCheck
// One thread-block (256 threads) per (target, candidate) pair.
// Each thread handles one value of A (0-255).
// ============================================================
struct ExhaustPair {
    uint16_t t_ops[3];
    uint16_t t_imms[3];
    uint16_t c_ops[1];
    uint16_t c_imms[1];
    uint8_t  t_len;
    uint8_t  c_len;
    uint8_t  nextra;
    uint8_t  extra[6];   // offsets: 1=X, 2=Y, 3=M, 4=S, 5=S0, 6=S1
    uint8_t  use_full;
    uint8_t  dead_flags;
    uint8_t  _pad;
};

__global__ void exhaustive_check_gpu(
    const ExhaustPair* __restrict__ pairs,
    uint32_t* __restrict__ results
) {
    uint32_t pair_idx = blockIdx.x;
    uint32_t a_val = threadIdx.x;  // 0..255

    __shared__ uint32_t mismatch;
    if (threadIdx.x == 0) mismatch = 0;
    __syncthreads();

    ExhaustPair ep = pairs[pair_idx];
    uint8_t dfm = ep.dead_flags;

    #define EXEC_TARGET(st) do { \
        for (int _i = 0; _i < ep.t_len; _i++) \
            exec_instruction(st, ep.t_ops[_i], ep.t_imms[_i]); \
    } while(0)

    #define EXEC_CAND(sc) exec_instruction(sc, ep.c_ops[0], ep.c_imms[0])

    // Helper: build initial state with S=0xFD (matching Go exhaustive check)
    #define INIT_STATE(st, a, carry) do { \
        st = {}; st.A = (uint8_t)(a); st.P = (uint8_t)(carry); st.S = 0xFD; \
    } while(0)

    if (ep.nextra == 0) {
        for (int carry = 0; carry <= 1; carry++) {
            if (mismatch) goto done;
            State6502 st, sc;
            INIT_STATE(st, a_val, carry); sc = st;
            EXEC_TARGET(st); EXEC_CAND(sc);
            if (!d_states_equal(st, sc, dfm)) { atomicOr(&mismatch, 1); goto done; }
        }
    } else if (ep.use_full && ep.nextra == 1) {
        for (int r = 0; r < 256; r++) {
            for (int carry = 0; carry <= 1; carry++) {
                if (mismatch) goto done;
                State6502 st, sc;
                INIT_STATE(st, a_val, carry);
                d_set_reg_by_offset(st, ep.extra[0], (uint8_t)r);
                sc = st;
                EXEC_TARGET(st); EXEC_CAND(sc);
                if (!d_states_equal(st, sc, dfm)) { atomicOr(&mismatch, 1); goto done; }
            }
        }
    } else if (ep.use_full && ep.nextra == 2) {
        for (int r1 = 0; r1 < 256; r1++) {
            for (int r2 = 0; r2 < 256; r2++) {
                for (int carry = 0; carry <= 1; carry++) {
                    if (mismatch) goto done;
                    State6502 st, sc;
                    INIT_STATE(st, a_val, carry);
                    d_set_reg_by_offset(st, ep.extra[0], (uint8_t)r1);
                    d_set_reg_by_offset(st, ep.extra[1], (uint8_t)r2);
                    sc = st;
                    EXEC_TARGET(st); EXEC_CAND(sc);
                    if (!d_states_equal(st, sc, dfm)) { atomicOr(&mismatch, 1); goto done; }
                }
            }
        }
    } else {
        // Reduced sweep: rep_values for 3+ extra regs
        int indices[6] = {};
        bool iter_done = false;
        while (!iter_done) {
            if (mismatch) goto done;
            for (int carry = 0; carry <= 1; carry++) {
                if (mismatch) goto done;
                State6502 st;
                INIT_STATE(st, a_val, carry);
                for (int i = 0; i < ep.nextra; i++)
                    d_set_reg_by_offset(st, ep.extra[i], d_rep_values[indices[i]]);
                State6502 sc = st;
                EXEC_TARGET(st); EXEC_CAND(sc);
                if (!d_states_equal(st, sc, dfm)) { atomicOr(&mismatch, 1); goto done; }
            }

            int k = ep.nextra - 1;
            while (k >= 0) {
                indices[k]++;
                if (indices[k] < 32) break;
                indices[k] = 0;
                k--;
            }
            if (k < 0) iter_done = true;
        }
    }

    #undef EXEC_TARGET
    #undef EXEC_CAND
    #undef INIT_STATE

done:
    __syncthreads();
    if (threadIdx.x == 0) {
        results[pair_idx] = (mismatch == 0) ? 1u : 0u;
    }
}

// ============================================================
// Host-side helpers
// ============================================================
static bool is_imm8(uint16_t op) {
    return op >= OP_LDA_IMM && op <= OP_CPY_IMM;
}

static int byte_size(uint16_t op) {
    if (op <= OP_NOP) return 1;  // implied: 1 byte
    return 2;  // immediate or memory: 2 bytes
}

static int cycles(uint16_t op) {
    if (op == OP_PHA || op == OP_PHP) return 3;
    if (op == OP_PLA || op == OP_PLP) return 4;
    if (op >= OP_LDA_M && op <= OP_CPY_M) return 3;
    if (op == OP_BIT_M) return 3;
    if (op >= OP_INC_M && op <= OP_ROR_M) return 5;
    return 2;  // all implied and immediate
}

// Disassembly — matches Go Disassemble format
static void disasm(uint16_t op, uint16_t imm, char* buf, int bufsz) {
    static const char* implied[] = {
        "TAX", "TAY", "TXA", "TYA", "TSX", "TXS",
        "INX", "INY", "DEX", "DEY",
        "ASL A", "LSR A", "ROL A", "ROR A",
        "CLC", "SEC", "CLD", "SED", "CLI", "SEI", "CLV",
        "PHA", "PLA", "PHP", "PLP", "NOP"
    };
    if (op <= OP_NOP) { snprintf(buf, bufsz, "%s", implied[op]); return; }

    // Immediate
    static const char* imm_fmt[] = {
        "LDA #$%02X", "LDX #$%02X", "LDY #$%02X",
        "ADC #$%02X", "SBC #$%02X", "AND #$%02X", "ORA #$%02X", "EOR #$%02X",
        "CMP #$%02X", "CPX #$%02X", "CPY #$%02X"
    };
    if (op >= OP_LDA_IMM && op <= OP_CPY_IMM) {
        snprintf(buf, bufsz, imm_fmt[op - OP_LDA_IMM], imm & 0xFF);
        return;
    }

    // Memory
    static const char* mem_names[] = {
        "LDA M", "LDX M", "LDY M", "STA M", "STX M", "STY M",
        "ADC M", "SBC M", "AND M", "ORA M", "EOR M",
        "CMP M", "CPX M", "CPY M",
        "INC M", "DEC M", "ASL M", "LSR M", "ROL M", "ROR M", "BIT M"
    };
    if (op >= OP_LDA_M && op <= OP_BIT_M) {
        snprintf(buf, bufsz, "%s", mem_names[op - OP_LDA_M]);
        return;
    }
    snprintf(buf, bufsz, "OP(%d)", op);
}

// ============================================================
// Register dependency masks (match Go regMask)
// ============================================================
#define RMASK_A   0x0001u
#define RMASK_P   0x0002u
#define RMASK_X   0x0004u
#define RMASK_Y   0x0008u
#define RMASK_S   0x0010u
#define RMASK_M   0x0020u
#define RMASK_S0  0x0040u
#define RMASK_S1  0x0080u

static uint16_t op_reads(uint16_t op) {
    switch (op) {
    case OP_TAX: case OP_TAY: return RMASK_A;
    case OP_TXA: return RMASK_X;
    case OP_TYA: return RMASK_Y;
    case OP_TSX: return RMASK_S;
    case OP_TXS: return RMASK_X;
    case OP_INX: case OP_DEX: return RMASK_X;
    case OP_INY: case OP_DEY: return RMASK_Y;
    case OP_ASL_A: case OP_LSR_A: return RMASK_A;
    case OP_ROL_A: case OP_ROR_A: return RMASK_A | RMASK_P;
    case OP_CLC: case OP_SEC: case OP_CLD: case OP_SED:
    case OP_CLI: case OP_SEI: case OP_CLV: return 0;
    case OP_PHA: return RMASK_A | RMASK_S0 | RMASK_S1;
    case OP_PLA: return RMASK_S0;
    case OP_PHP: return RMASK_P | RMASK_S0 | RMASK_S1;
    case OP_PLP: return RMASK_S0;
    case OP_NOP: return 0;
    case OP_LDA_IMM: case OP_LDX_IMM: case OP_LDY_IMM: return 0;
    case OP_ADC_IMM: case OP_SBC_IMM: return RMASK_A | RMASK_P;
    case OP_AND_IMM: case OP_ORA_IMM: case OP_EOR_IMM: return RMASK_A;
    case OP_CMP_IMM: return RMASK_A;
    case OP_CPX_IMM: return RMASK_X;
    case OP_CPY_IMM: return RMASK_Y;
    case OP_LDA_M: case OP_LDX_M: case OP_LDY_M: return RMASK_M;
    case OP_STA_M: return RMASK_A;
    case OP_STX_M: return RMASK_X;
    case OP_STY_M: return RMASK_Y;
    case OP_ADC_M: case OP_SBC_M: return RMASK_A | RMASK_M | RMASK_P;
    case OP_AND_M: case OP_ORA_M: case OP_EOR_M: return RMASK_A | RMASK_M;
    case OP_CMP_M: return RMASK_A | RMASK_M;
    case OP_CPX_M: return RMASK_X | RMASK_M;
    case OP_CPY_M: return RMASK_Y | RMASK_M;
    case OP_INC_M: case OP_DEC_M: return RMASK_M;
    case OP_ASL_M: case OP_LSR_M: return RMASK_M;
    case OP_ROL_M: case OP_ROR_M: return RMASK_M | RMASK_P;
    case OP_BIT_M: return RMASK_A | RMASK_M;
    }
    return 0;
}

static uint16_t op_writes(uint16_t op) {
    switch (op) {
    case OP_TAX: return RMASK_X | RMASK_P;
    case OP_TAY: return RMASK_Y | RMASK_P;
    case OP_TXA: case OP_TYA: return RMASK_A | RMASK_P;
    case OP_TSX: return RMASK_X | RMASK_P;
    case OP_TXS: return RMASK_S;  // no flags
    case OP_INX: case OP_DEX: return RMASK_X | RMASK_P;
    case OP_INY: case OP_DEY: return RMASK_Y | RMASK_P;
    case OP_ASL_A: case OP_LSR_A: case OP_ROL_A: case OP_ROR_A: return RMASK_A | RMASK_P;
    case OP_CLC: case OP_SEC: case OP_CLD: case OP_SED:
    case OP_CLI: case OP_SEI: case OP_CLV: return RMASK_P;
    case OP_PHA: case OP_PHP: return RMASK_S | RMASK_S0 | RMASK_S1;
    case OP_PLA: return RMASK_A | RMASK_P | RMASK_S | RMASK_S0;
    case OP_PLP: return RMASK_P | RMASK_S | RMASK_S0;
    case OP_NOP: return 0;
    case OP_LDA_IMM: return RMASK_A | RMASK_P;
    case OP_LDX_IMM: return RMASK_X | RMASK_P;
    case OP_LDY_IMM: return RMASK_Y | RMASK_P;
    case OP_ADC_IMM: case OP_SBC_IMM:
    case OP_AND_IMM: case OP_ORA_IMM: case OP_EOR_IMM: return RMASK_A | RMASK_P;
    case OP_CMP_IMM: case OP_CPX_IMM: case OP_CPY_IMM: return RMASK_P;
    case OP_LDA_M: return RMASK_A | RMASK_P;
    case OP_LDX_M: return RMASK_X | RMASK_P;
    case OP_LDY_M: return RMASK_Y | RMASK_P;
    case OP_STA_M: case OP_STX_M: case OP_STY_M: return RMASK_M;
    case OP_ADC_M: case OP_SBC_M:
    case OP_AND_M: case OP_ORA_M: case OP_EOR_M: return RMASK_A | RMASK_P;
    case OP_CMP_M: case OP_CPX_M: case OP_CPY_M: return RMASK_P;
    case OP_INC_M: case OP_DEC_M: return RMASK_M | RMASK_P;
    case OP_ASL_M: case OP_LSR_M: case OP_ROL_M: case OP_ROR_M: return RMASK_M | RMASK_P;
    case OP_BIT_M: return RMASK_P;
    }
    return 0;
}

// Pruning
static uint32_t inst_key(uint16_t op, uint16_t imm) { return ((uint32_t)op << 16) | imm; }

static bool are_independent(uint16_t op1, uint16_t op2) {
    uint16_t aR = op_reads(op1), aW = op_writes(op1);
    uint16_t bR = op_reads(op2), bW = op_writes(op2);
    return (aW & bR) == 0 && (aR & bW) == 0 && (aW & bW) == 0;
}

static bool has_stack_violation(const uint16_t* ops, int n) {
    int depth = 0;
    for (int i = 0; i < n; i++) {
        if (ops[i] == OP_PHA || ops[i] == OP_PHP) {
            depth++;
            if (depth > 2) return true;
        }
        if (ops[i] == OP_PLA || ops[i] == OP_PLP) {
            if (depth <= 0) return true;
            depth--;
        }
    }
    return false;
}

static bool should_prune(const uint16_t* ops, const uint16_t* imms, int n) {
    for (int i = 0; i < n; i++) {
        if (ops[i] == OP_NOP) return true;
        // Dead write: exclude P, S, S0, S1 from dead-write detection
        if (i + 1 < n) {
            uint16_t w1 = op_writes(ops[i]);
            if (w1) {
                uint16_t r2 = op_reads(ops[i + 1]);
                uint16_t w2 = op_writes(ops[i + 1]);
                uint16_t dead = w1 & w2 & ~RMASK_P & ~RMASK_S & ~RMASK_S0 & ~RMASK_S1 & ~r2;
                if (dead) return true;
            }
        }
    }
    if (has_stack_violation(ops, n)) return true;
    // Canonical ordering
    for (int i = 0; i + 1 < n; i++) {
        if (are_independent(ops[i], ops[i + 1]) &&
            inst_key(ops[i], imms[i]) > inst_key(ops[i + 1], imms[i + 1]))
            return true;
    }
    return false;
}

// Register reads helper
static uint16_t regs_read(const uint16_t* ops, int n) {
    uint16_t m = 0;
    for (int i = 0; i < n; i++) m |= op_reads(ops[i]);
    return m;
}

// Build ExhaustPair for GPU kernel 3
static ExhaustPair build_exhaust_pair(
    const uint16_t* t_ops, const uint16_t* t_imms, int t_n,
    const uint16_t* c_ops, const uint16_t* c_imms, int c_n,
    uint8_t dead_flags
) {
    ExhaustPair ep = {};
    for (int i = 0; i < t_n && i < 3; i++) { ep.t_ops[i] = t_ops[i]; ep.t_imms[i] = t_imms[i]; }
    ep.c_ops[0] = c_ops[0]; ep.c_imms[0] = c_imms[0];
    ep.t_len = (uint8_t)t_n; ep.c_len = (uint8_t)c_n; ep.dead_flags = dead_flags;
    uint16_t reads = regs_read(t_ops, t_n) | regs_read(c_ops, c_n);
    ep.nextra = 0;
    // Extra regs: X(1), Y(2), M(3), S(4), S0(5), S1(6)
    if (reads & RMASK_X)  ep.extra[ep.nextra++] = 1;
    if (reads & RMASK_Y)  ep.extra[ep.nextra++] = 2;
    if (reads & RMASK_M)  ep.extra[ep.nextra++] = 3;
    if (reads & RMASK_S)  ep.extra[ep.nextra++] = 4;
    if (reads & RMASK_S0) ep.extra[ep.nextra++] = 5;
    if (reads & RMASK_S1) ep.extra[ep.nextra++] = 6;
    ep.use_full = (ep.nextra <= 2) ? 1 : 0;
    return ep;
}

// Host-side set reg by offset
static void h_set_reg_by_offset(State6502 &s, int offset, uint8_t val) {
    switch (offset) {
        case 1: s.X = val; break;  case 2: s.Y = val; break;
        case 3: s.M = val; break;  case 4: s.S = val; break;
        case 5: s.S0 = val; break; case 6: s.S1 = val; break;
    }
}

static const uint8_t h_rep_values[32] = {
    0x00,0x01,0x02,0x0F,0x10,0x1F,0x20,0x3F,
    0x40,0x55,0x7E,0x7F,0x80,0x81,0xAA,0xBF,
    0xC0,0xD5,0xE0,0xEF,0xF0,0xF7,0xFE,0xFF,
    0x03,0x07,0x11,0x33,0x77,0xBB,0xDD,0xEE,
};

// CPU ExhaustiveCheck — fallback for reduced-sweep pairs (nextra > 2)
static bool cpu_exhaustive_check(
    const uint16_t* t_ops, const uint16_t* t_imms, int t_n,
    const uint16_t* c_ops, const uint16_t* c_imms, int c_n,
    uint8_t dead_flags
) {
    uint16_t reads = regs_read(t_ops, t_n) | regs_read(c_ops, c_n);
    int extra[6]; int nextra = 0;
    if (reads & RMASK_X)  extra[nextra++] = 1;
    if (reads & RMASK_Y)  extra[nextra++] = 2;
    if (reads & RMASK_M)  extra[nextra++] = 3;
    if (reads & RMASK_S)  extra[nextra++] = 4;
    if (reads & RMASK_S0) extra[nextra++] = 5;
    if (reads & RMASK_S1) extra[nextra++] = 6;

    for (int a = 0; a < 256; a++) {
        for (int carry = 0; carry <= 1; carry++) {
            if (nextra == 0) {
                State6502 s = {}; s.A = (uint8_t)a; s.P = (uint8_t)carry; s.S = 0xFD;
                State6502 st = s, sc = s;
                h_exec_seq(st, t_ops, t_imms, t_n); h_exec_seq(sc, c_ops, c_imms, c_n);
                if (!h_states_equal(st, sc, dead_flags)) return false;
                continue;
            }
            if (nextra == 1) {
                for (int r = 0; r < 256; r++) {
                    State6502 s = {}; s.A = (uint8_t)a; s.P = (uint8_t)carry; s.S = 0xFD;
                    h_set_reg_by_offset(s, extra[0], (uint8_t)r);
                    State6502 st = s, sc = s;
                    h_exec_seq(st, t_ops, t_imms, t_n); h_exec_seq(sc, c_ops, c_imms, c_n);
                    if (!h_states_equal(st, sc, dead_flags)) return false;
                }
                continue;
            }
            if (nextra == 2) {
                for (int r1 = 0; r1 < 256; r1++) {
                    for (int r2 = 0; r2 < 256; r2++) {
                        State6502 s = {}; s.A = (uint8_t)a; s.P = (uint8_t)carry; s.S = 0xFD;
                        h_set_reg_by_offset(s, extra[0], (uint8_t)r1);
                        h_set_reg_by_offset(s, extra[1], (uint8_t)r2);
                        State6502 st = s, sc = s;
                        h_exec_seq(st, t_ops, t_imms, t_n); h_exec_seq(sc, c_ops, c_imms, c_n);
                        if (!h_states_equal(st, sc, dead_flags)) return false;
                    }
                }
                continue;
            }
            // Reduced sweep: 3+ regs
            auto do_sweep = [&](auto& self, State6502 s, int ri) -> bool {
                if (ri >= nextra) {
                    State6502 st = s, sc = s;
                    h_exec_seq(st, t_ops, t_imms, t_n); h_exec_seq(sc, c_ops, c_imms, c_n);
                    return h_states_equal(st, sc, dead_flags);
                }
                for (int vi = 0; vi < 32; vi++) {
                    State6502 s2 = s;
                    h_set_reg_by_offset(s2, extra[ri], h_rep_values[vi]);
                    if (!self(self, s2, ri + 1)) return false;
                }
                return true;
            };
            State6502 base = {}; base.A = (uint8_t)a; base.P = (uint8_t)carry; base.S = 0xFD;
            if (!do_sweep(do_sweep, base, 0)) return false;
        }
    }
    return true;
}

// Instruction enumeration
struct Inst { uint16_t op, imm; };

static std::vector<Inst> enumerate_instructions_8() {
    std::vector<Inst> result;
    for (uint16_t op = 0; op < OP_COUNT; op++) {
        if (is_imm8(op)) {
            for (int imm = 0; imm < 256; imm++)
                result.push_back({op, (uint16_t)imm});
        } else {
            result.push_back({op, 0});
        }
    }
    return result;
}

// Batch target
struct BatchTarget {
    uint16_t ops[3], imms[3];
    int len, bytes;
};

// ============================================================
// Main — 3-stage batched pipeline
// ============================================================
int main(int argc, char** argv) {
    int max_target = 2; uint8_t dead_flags = 0; int gpu_id = 0;
    int first_op_start = 0, first_op_end = -1;
    bool no_exhaust = false;

    for (int i = 1; i < argc; i++) {
        if (!strcmp(argv[i], "--max-target") && i + 1 < argc) max_target = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--dead-flags") && i + 1 < argc) {
            i++;
            if (!strcmp(argv[i], "all")) dead_flags = 0xFF;
            else dead_flags = (uint8_t)strtoul(argv[i], NULL, 0);
        }
        else if (!strcmp(argv[i], "--gpu-id") && i + 1 < argc) gpu_id = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--first-op-start") && i + 1 < argc) first_op_start = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--first-op-end") && i + 1 < argc) first_op_end = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--no-exhaust")) no_exhaust = true;
        else if (!strcmp(argv[i], "--help")) {
            fprintf(stderr, "Usage: 6502search_v2 [OPTIONS]\n"
                "  --max-target N        Max target sequence length (default: 2)\n"
                "  --dead-flags 0xNN|all Flag mask for dead flags\n"
                "  --gpu-id N            CUDA device ID (default: 0)\n"
                "  --first-op-start M    Start outer loop at instruction index M\n"
                "  --first-op-end N      End outer loop at instruction index N\n"
                "  --no-exhaust          Skip ExhaustiveCheck, output MidCheck survivors\n");
            return 0;
        }
    }

    cudaSetDevice(gpu_id);
    cudaDeviceProp prop; cudaGetDeviceProperties(&prop, gpu_id);
    fprintf(stderr, "GPU %d: %s (%.1f GB, SM %d.%d)\n", gpu_id, prop.name,
            prop.totalGlobalMem / 1e9, prop.major, prop.minor);

    init_tables();
    upload_tables_cuda();

    std::vector<Inst> all_insts = enumerate_instructions_8();
    uint32_t cand_count = (uint32_t)all_insts.size();
    fprintf(stderr, "Instruction set: %u instructions (8-bit)\n", cand_count);
    if (first_op_end < 0) first_op_end = (int)all_insts.size();

    std::vector<uint32_t> cand_packed(cand_count);
    for (size_t i = 0; i < all_insts.size(); i++)
        cand_packed[i] = (uint32_t)all_insts[i].op | ((uint32_t)all_insts[i].imm << 16);

    // GPU allocations
    uint32_t bitmap_words = (cand_count + 31) / 32;
    uint32_t *d_candidates, *d_hit_bitmap;
    uint8_t *d_target_fps, *d_target_mid_fps;
    MidPair *d_mid_pairs; uint32_t *d_mid_survived;
    ExhaustPair *d_exhaust_pairs; uint32_t *d_exhaust_results;

    uint32_t max_mid_pairs = BATCH_SIZE * 256;
    uint32_t max_exhaust_pairs = 16384;

    cudaMalloc(&d_candidates, cand_count * sizeof(uint32_t));
    cudaMalloc(&d_target_fps, BATCH_SIZE * FP_LEN);
    cudaMalloc(&d_target_mid_fps, BATCH_SIZE * MID_FP_LEN);
    cudaMalloc(&d_hit_bitmap, BATCH_SIZE * bitmap_words * sizeof(uint32_t));
    cudaMalloc(&d_mid_pairs, max_mid_pairs * sizeof(MidPair));
    cudaMalloc(&d_mid_survived, max_mid_pairs * sizeof(uint32_t));
    cudaMalloc(&d_exhaust_pairs, max_exhaust_pairs * sizeof(ExhaustPair));
    cudaMalloc(&d_exhaust_results, max_exhaust_pairs * sizeof(uint32_t));
    cudaMemcpy(d_candidates, cand_packed.data(), cand_count * sizeof(uint32_t), cudaMemcpyHostToDevice);

    // Host buffers
    uint8_t *h_fps = (uint8_t*)malloc(BATCH_SIZE * FP_LEN);
    uint8_t *h_mfps = (uint8_t*)malloc(BATCH_SIZE * MID_FP_LEN);
    uint32_t *h_bitmap = (uint32_t*)malloc(BATCH_SIZE * bitmap_words * sizeof(uint32_t));
    MidPair *h_mpairs = (MidPair*)malloc(max_mid_pairs * sizeof(MidPair));
    uint32_t *h_msurv = (uint32_t*)malloc(max_mid_pairs * sizeof(uint32_t));
    ExhaustPair *h_epairs = (ExhaustPair*)malloc(max_exhaust_pairs * sizeof(ExhaustPair));
    uint32_t *h_eresults = (uint32_t*)malloc(max_exhaust_pairs * sizeof(uint32_t));

    uint64_t total_found = 0, total_targets = 0, total_qc_hits = 0;
    uint64_t total_mid_hits = 0, total_exhaust = 0, total_cpu_exhaust = 0, total_batches = 0;
    time_t start_time = time(NULL);

    fprintf(stderr, "Starting v2 search: max_target=%d, dead_flags=0x%02X, gpu=%d, ops=[%d,%d)\n",
            max_target, dead_flags, gpu_id, first_op_start, first_op_end);

    std::vector<BatchTarget> batch;
    batch.reserve(BATCH_SIZE);

    // Flush one batch through 3-stage pipeline
    auto flush_batch = [&]() {
        if (batch.empty()) return;
        uint32_t bc = (uint32_t)batch.size();
        total_batches++;

        // CPU: compute fingerprints
        for (uint32_t bi = 0; bi < bc; bi++) {
            h_fingerprint(batch[bi].ops, batch[bi].imms, batch[bi].len, h_fps + bi * FP_LEN);
            h_mid_fingerprint(batch[bi].ops, batch[bi].imms, batch[bi].len, h_mfps + bi * MID_FP_LEN);
        }
        cudaMemcpy(d_target_fps, h_fps, bc * FP_LEN, cudaMemcpyHostToDevice);
        cudaMemcpy(d_target_mid_fps, h_mfps, bc * MID_FP_LEN, cudaMemcpyHostToDevice);
        cudaMemset(d_hit_bitmap, 0, bc * bitmap_words * sizeof(uint32_t));

        // Stage 1: Batched QuickCheck
        uint32_t total_threads = bc * cand_count;
        int grid1 = (total_threads + BLOCK_SIZE - 1) / BLOCK_SIZE;
        quickcheck_batched<<<grid1, BLOCK_SIZE>>>(d_candidates, d_target_fps, d_hit_bitmap,
            cand_count, bc, bitmap_words, dead_flags);
        cudaDeviceSynchronize();
        cudaMemcpy(h_bitmap, d_hit_bitmap, bc * bitmap_words * sizeof(uint32_t), cudaMemcpyDeviceToHost);

        // Collect QC hits
        uint32_t mid_count = 0;
        for (uint32_t bi = 0; bi < bc; bi++) {
            int tbytes = batch[bi].bytes;
            for (uint32_t w = 0; w < bitmap_words && mid_count < max_mid_pairs; w++) {
                uint32_t bits = h_bitmap[bi * bitmap_words + w];
                while (bits && mid_count < max_mid_pairs) {
                    int bit = __builtin_ctz(bits);
                    bits &= bits - 1;
                    uint32_t ci = w * 32 + bit;
                    if (ci >= cand_count) break;
                    int cb = byte_size(all_insts[ci].op);
                    if (cb >= tbytes) continue;  // candidate must be shorter
                    uint16_t co[1] = {all_insts[ci].op}, cm[1] = {all_insts[ci].imm};
                    if (should_prune(co, cm, 1)) continue;
                    h_mpairs[mid_count++] = {(uint16_t)bi, (uint16_t)ci};
                }
            }
        }
        total_qc_hits += mid_count;
        if (mid_count == 0) { batch.clear(); return; }

        // Stage 2: MidCheck
        cudaMemcpy(d_mid_pairs, h_mpairs, mid_count * sizeof(MidPair), cudaMemcpyHostToDevice);
        int grid2 = (mid_count + BLOCK_SIZE - 1) / BLOCK_SIZE;
        midcheck_kernel<<<grid2, BLOCK_SIZE>>>(d_mid_pairs, mid_count, d_candidates,
            d_target_mid_fps, d_mid_survived, dead_flags);
        cudaDeviceSynchronize();
        cudaMemcpy(h_msurv, d_mid_survived, mid_count * sizeof(uint32_t), cudaMemcpyDeviceToHost);

        // Collect MidCheck survivors
        struct EInfo { uint32_t bi, ci; };
        std::vector<EInfo> mid_survivors;
        for (uint32_t mi = 0; mi < mid_count; mi++) {
            if (!h_msurv[mi]) continue;
            MidPair mp = h_mpairs[mi];
            mid_survivors.push_back({mp.target_idx, mp.cand_idx});
        }
        total_mid_hits += (uint64_t)mid_survivors.size();
        if (mid_survivors.empty()) { batch.clear(); return; }

        // Emit one JSONL result
        auto emit_jsonl = [&](BatchTarget &bt, uint16_t cop, uint16_t cimm) {
            int cb = byte_size(cop);
            total_found++;
            char sbuf[256], rbuf[64], p[3][64];
            for (int j = 0; j < bt.len; j++) disasm(bt.ops[j], bt.imms[j], p[j], sizeof(p[j]));
            disasm(cop, cimm, rbuf, sizeof(rbuf));
            if (bt.len == 2) snprintf(sbuf, sizeof(sbuf), "%s : %s", p[0], p[1]);
            else if (bt.len == 3) snprintf(sbuf, sizeof(sbuf), "%s : %s : %s", p[0], p[1], p[2]);
            else snprintf(sbuf, sizeof(sbuf), "%s", p[0]);
            int bsaved = bt.bytes - cb, csaved = 0;
            for (int j = 0; j < bt.len; j++) csaved += cycles(bt.ops[j]);
            csaved -= cycles(cop);
            printf("{\"source_asm\":\"%s\",\"replacement_asm\":\"%s\","
                   "\"source_bytes\":%d,\"replacement_bytes\":%d,"
                   "\"bytes_saved\":%d,\"cycles_saved\":%d",
                   sbuf, rbuf, bt.bytes, cb, bsaved, csaved);
            if (dead_flags) printf(",\"dead_flags\":\"0x%02X\"", dead_flags);
            printf("}\n"); fflush(stdout);
        };

        if (no_exhaust) {
            for (auto &inf : mid_survivors)
                emit_jsonl(batch[inf.bi], all_insts[inf.ci].op, all_insts[inf.ci].imm);
        } else {
            // Stage 3: split into GPU (full sweep) and CPU (reduced sweep)
            uint32_t exhaust_count = 0;
            std::vector<EInfo> gpu_einfo;
            std::vector<EInfo> cpu_einfo;
            for (auto &inf : mid_survivors) {
                uint16_t co[1] = {all_insts[inf.ci].op}, cm[1] = {all_insts[inf.ci].imm};
                ExhaustPair ep = build_exhaust_pair(
                    batch[inf.bi].ops, batch[inf.bi].imms, batch[inf.bi].len,
                    co, cm, 1, dead_flags);
                if (ep.use_full && exhaust_count < max_exhaust_pairs) {
                    h_epairs[exhaust_count] = ep;
                    gpu_einfo.push_back(inf);
                    exhaust_count++;
                } else {
                    cpu_einfo.push_back(inf);
                }
            }

            // Stage 3a: GPU ExhaustiveCheck (full-sweep pairs)
            if (exhaust_count > 0) {
                cudaMemcpy(d_exhaust_pairs, h_epairs, exhaust_count * sizeof(ExhaustPair), cudaMemcpyHostToDevice);
                exhaustive_check_gpu<<<exhaust_count, EXHAUST_BLOCK>>>(d_exhaust_pairs, d_exhaust_results);
                cudaDeviceSynchronize();
                cudaMemcpy(h_eresults, d_exhaust_results, exhaust_count * sizeof(uint32_t), cudaMemcpyDeviceToHost);
                total_exhaust += exhaust_count;
                for (uint32_t ei = 0; ei < exhaust_count; ei++) {
                    if (!h_eresults[ei]) continue;
                    emit_jsonl(batch[gpu_einfo[ei].bi], all_insts[gpu_einfo[ei].ci].op, all_insts[gpu_einfo[ei].ci].imm);
                }
            }

            // Stage 3b: CPU ExhaustiveCheck (reduced-sweep pairs)
            for (auto &inf : cpu_einfo) {
                BatchTarget &bt = batch[inf.bi];
                uint16_t co[1] = {all_insts[inf.ci].op}, cm[1] = {all_insts[inf.ci].imm};
                total_cpu_exhaust++;
                if (cpu_exhaustive_check(bt.ops, bt.imms, bt.len, co, cm, 1, dead_flags))
                    emit_jsonl(bt, co[0], cm[0]);
            }
        }
        batch.clear();
    };

    // Enumerate targets
    for (int target_len = 2; target_len <= max_target; target_len++) {
        fprintf(stderr, "=== Target length %d ===\n", target_len);
        uint64_t targets_this = 0, found_before = total_found;
        time_t len_start = time(NULL), last_report = len_start;

        if (target_len == 2) {
            for (int i0 = first_op_start; i0 < first_op_end && i0 < (int)all_insts.size(); i0++) {
                time_t now = time(NULL);
                if (now - last_report >= 10) {
                    last_report = now;
                    double pct = 100.0 * (i0 - first_op_start) / (first_op_end - first_op_start);
                    double el = difftime(now, start_time), eta = (pct > 0.1) ? el * (100.0 / pct - 1.0) : 0;
                    fprintf(stderr, "  [%.1f%%] op %d/%d | targets:%lu | QC:%lu Mid:%lu Ex:%lu | found:%lu | %lds, ETA %lds\n",
                        pct, i0, first_op_end, (unsigned long)total_targets,
                        (unsigned long)total_qc_hits, (unsigned long)total_mid_hits,
                        (unsigned long)total_exhaust, (unsigned long)total_found,
                        (long)el, (long)eta);
                }
                for (size_t i1 = 0; i1 < all_insts.size(); i1++) {
                    uint16_t to[2] = {all_insts[i0].op, all_insts[i1].op};
                    uint16_t ti[2] = {all_insts[i0].imm, all_insts[i1].imm};
                    if (should_prune(to, ti, 2)) continue;
                    targets_this++; total_targets++;
                    BatchTarget bt;
                    bt.ops[0] = to[0]; bt.ops[1] = to[1]; bt.ops[2] = 0;
                    bt.imms[0] = ti[0]; bt.imms[1] = ti[1]; bt.imms[2] = 0;
                    bt.len = 2; bt.bytes = byte_size(to[0]) + byte_size(to[1]);
                    batch.push_back(bt);
                    if ((int)batch.size() >= BATCH_SIZE) flush_batch();
                }
            }
        } else if (target_len == 3) {
            for (int i0 = first_op_start; i0 < first_op_end && i0 < (int)all_insts.size(); i0++) {
                time_t now = time(NULL);
                if (now - last_report >= 10) {
                    last_report = now;
                    double pct = 100.0 * (i0 - first_op_start) / (first_op_end - first_op_start);
                    double el = difftime(now, start_time), eta = (pct > 0.1) ? el * (100.0 / pct - 1.0) : 0;
                    fprintf(stderr, "  [%.1f%%] op %d/%d | targets:%lu | found:%lu | %lds, ETA %lds\n",
                        pct, i0, first_op_end, (unsigned long)total_targets,
                        (unsigned long)total_found, (long)el, (long)eta);
                }
                for (size_t i1 = 0; i1 < all_insts.size(); i1++) {
                    for (size_t i2 = 0; i2 < all_insts.size(); i2++) {
                        uint16_t to[3] = {all_insts[i0].op, all_insts[i1].op, all_insts[i2].op};
                        uint16_t ti[3] = {all_insts[i0].imm, all_insts[i1].imm, all_insts[i2].imm};
                        if (should_prune(to, ti, 3)) continue;
                        targets_this++; total_targets++;
                        BatchTarget bt;
                        bt.ops[0] = to[0]; bt.ops[1] = to[1]; bt.ops[2] = to[2];
                        bt.imms[0] = ti[0]; bt.imms[1] = ti[1]; bt.imms[2] = ti[2];
                        bt.len = 3; bt.bytes = byte_size(to[0]) + byte_size(to[1]) + byte_size(to[2]);
                        batch.push_back(bt);
                        if ((int)batch.size() >= BATCH_SIZE) flush_batch();
                    }
                }
            }
        }
        flush_batch();
        time_t len_end = time(NULL);
        fprintf(stderr, "  Length %d done: %lu targets, %lu found (%lds)\n",
            target_len, (unsigned long)targets_this, (unsigned long)(total_found - found_before),
            (long)(len_end - len_start));
    }

    time_t end_time = time(NULL);
    fprintf(stderr, "\n=== DONE (v2 batched pipeline) ===\n");
    fprintf(stderr, "Targets tested:     %lu\n", (unsigned long)total_targets);
    fprintf(stderr, "Batches processed:  %lu\n", (unsigned long)total_batches);
    fprintf(stderr, "QuickCheck hits:    %lu\n", (unsigned long)total_qc_hits);
    fprintf(stderr, "MidCheck survivors: %lu\n", (unsigned long)total_mid_hits);
    fprintf(stderr, "ExhaustiveCheck:    %lu (GPU:%lu CPU:%lu)\n",
            (unsigned long)(total_exhaust + total_cpu_exhaust),
            (unsigned long)total_exhaust, (unsigned long)total_cpu_exhaust);
    fprintf(stderr, "Results found:      %lu\n", (unsigned long)total_found);
    fprintf(stderr, "Total time:         %lds\n", (long)(end_time - start_time));
    if (total_qc_hits > 0)
        fprintf(stderr, "False positive rate: %.1f%% (QC->confirmed)\n",
                100.0 * (1.0 - (double)total_found / total_qc_hits));

    free(h_fps); free(h_mfps); free(h_bitmap);
    free(h_mpairs); free(h_msurv); free(h_epairs); free(h_eresults);
    cudaFree(d_candidates); cudaFree(d_target_fps); cudaFree(d_target_mid_fps);
    cudaFree(d_hit_bitmap); cudaFree(d_mid_pairs); cudaFree(d_mid_survived);
    cudaFree(d_exhaust_pairs); cudaFree(d_exhaust_results);
    return 0;
}
