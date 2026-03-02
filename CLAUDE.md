# 6502 Superoptimizer

Brute-force MOS 6502 superoptimizer. Go project.

## Build & Test

```bash
go build -o 6502opt ./cmd/6502opt
go test ./...
```

## Architecture

- `pkg/cpu/` — 6502 State (8 bytes: A,X,Y,P,S,M,S0,S1), NZ flag table, executor
- `pkg/inst/` — OpCode enum (uint16, 58 opcodes), Instruction{Op, Imm}, catalog with encoding/cycles
- `pkg/search/` — QuickCheck (8 vectors, 64-byte fingerprint) + MidCheck (32 vectors) + ExhaustiveCheck (up to 2^24 states), enumerator, pruner, worker pool
- `pkg/stoke/` — STOKE stochastic superoptimizer (MCMC search, parallel chains)
- `pkg/result/` — Rule storage, gob checkpoint, JSON + Go codegen output
- `cmd/6502opt/` — CLI: enumerate, target, verify-jsonl, export, stoke

## Key Invariant

Full state equivalence: target and candidate must produce identical output for ALL possible inputs, including flags and stack. `LDA #$00` != `AND #$00` because flags differ (AND doesn't affect carry).

## State Model

8-byte state: A, X, Y, P (flags: NV-BDIZC), S (stack pointer), M (virtual zero-page memory byte), S0, S1 (virtual stack slots).

P register comparison masks out bits 4 (B) and 5 (unused) — only N,V,D,I,Z,C are ALU-relevant.

## Instruction Set

58 opcodes total:
- 26 implied (transfers, inc/dec, shifts on A, flag ops, stack ops, NOP)
- 11 immediate x 256 = 2,816 concrete instructions
- 21 zero-page memory (virtual M) ops

Total: 2,863 concrete instructions per search position.

## Decimal Mode

Deferred. ADC/SBC assume D=0 (binary mode only).
