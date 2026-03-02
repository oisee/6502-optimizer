# 6502 Superoptimizer

Brute-force superoptimizer for the MOS 6502 CPU. Given a sequence of instructions, finds the shortest equivalent replacement — proven correct by exhaustive verification over all possible inputs.

Forked from [z80-optimizer](https://github.com/oisee/z80-optimizer).

## Quick Start

```bash
go build -o 6502opt ./cmd/6502opt
go test ./...

# Find optimal replacement for a specific sequence
./6502opt target "TXA : TAX"
# Target: TXA : TAX (2 bytes, 4 cycles)
# Replacement: TXA (-1 bytes, -2 cycles)

# Enumerate all length-2 optimizations
./6502opt enumerate --max-target 2 --workers 8 -v

# STOKE stochastic search for longer sequences
./6502opt stoke --target "PHA : TXA : PLA" --chains 8 --iterations 1000000 -v
```

## How It Works

The optimizer uses a 3-stage verification pipeline to efficiently search the space of all possible instruction sequences:

1. **QuickCheck** — 8 test vectors, 64-byte fingerprint. Rejects ~99.99% of non-equivalent candidates instantly via fingerprint comparison.
2. **MidCheck** — 32 additional test vectors. Filters survivors before the expensive exhaustive stage.
3. **ExhaustiveCheck** — Sweeps all relevant input combinations (up to 2^24 states). Any candidate that passes is provably equivalent.

For brute-force enumeration, the optimizer generates all possible target sequences of a given length, prunes obviously redundant ones, and searches for shorter replacements. For longer sequences where brute-force is infeasible, STOKE (Stochastic Superoptimization) uses MCMC random walks to explore the search space.

## State Model

The 6502 is modeled with an 8-byte state:

| Register | Description |
|----------|-------------|
| A | Accumulator |
| X, Y | Index registers |
| P | Processor flags (NV-BDIZC) |
| S | Stack pointer |
| M | Virtual zero-page memory byte |
| S0, S1 | Virtual stack slots (2-deep) |

Flag comparison masks out bits 4 (B) and 5 (unused) — only N, V, D, I, Z, C are ALU-relevant.

## Instruction Set

58 opcodes, 2,863 concrete instructions:

| Category | Count | Examples |
|----------|-------|---------|
| Register transfers | 6 | TAX, TAY, TXA, TYA, TSX, TXS |
| Inc/Dec registers | 4 | INX, INY, DEX, DEY |
| Shifts on A | 4 | ASL A, LSR A, ROL A, ROR A |
| Flag operations | 7 | CLC, SEC, CLD, SED, CLI, SEI, CLV |
| Stack operations | 4 | PHA, PLA, PHP, PLP |
| NOP | 1 | NOP |
| Immediate (x256) | 11 | LDA #n, ADC #n, CMP #n, ... |
| Zero-page / M | 21 | LDA M, STA M, INC M, BIT M, ... |

Decimal mode (BCD) is deferred — ADC/SBC assume D=0.

## CLI Commands

| Command | Description |
|---------|-------------|
| `enumerate` | Brute-force search over all sequences up to `--max-target` length |
| `target` | Find optimal replacement for a specific instruction sequence |
| `stoke` | STOKE stochastic search (MCMC) for longer sequences |
| `verify-jsonl` | Verify JSONL rule files against CPU ExhaustiveCheck |
| `export` | Export rules as Go code |

### Dead Flags

Use `--dead-flags` to find optimizations where certain flags don't need to be preserved:

```bash
# Find optimizations where carry flag is dead
./6502opt enumerate --max-target 2 --dead-flags 0x01

# All flags dead (register-only equivalence)
./6502opt target "CLC : ADC #$01" --dead-flags all
```

## Architecture

```
pkg/cpu/         State, flag tables, executor (58 opcodes)
pkg/inst/        OpCode enum, Instruction type, catalog with encodings/cycles
pkg/search/      QuickCheck + MidCheck + ExhaustiveCheck, enumerator, pruner, worker pool
pkg/stoke/       STOKE MCMC optimizer (cost function, mutator, parallel chains)
pkg/result/      Rule storage, JSON/Go output, gob checkpoints
cmd/6502opt/     CLI
```

## Pruning

The pruner eliminates redundant sequences before searching:

- **NOP elimination** — sequences containing NOP are skipped (NOP is never optimal)
- **Dead writes** — instruction whose output is immediately overwritten by the next
- **Stack violations** — sequences with stack underflow or overflow (>2 pushes)
- **Canonical ordering** — independent instructions kept in canonical order to avoid permutation duplicates

## Key Differences from Z80

| | 6502 | Z80 |
|---|---|---|
| Registers | A, X, Y (3) | A, B, C, D, E, H, L (7) |
| State size | 8 bytes | 11 bytes |
| Opcodes | 58 | 455 |
| Instructions | 2,863 | 4,215 |
| Flags | N, V, D, I, Z, C | S, Z, H, P/V, N, C + undoc |
| Immediates | 8-bit only | 8-bit and 16-bit |
| Timing | Cycles | T-states |

## License

MIT
