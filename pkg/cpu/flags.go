package cpu

// 6502 flag bit positions in the P register (NV-BDIZC).
const (
	FlagC uint8 = 0x01 // Carry
	FlagZ uint8 = 0x02 // Zero
	FlagI uint8 = 0x04 // Interrupt disable
	FlagD uint8 = 0x08 // Decimal mode
	FlagB uint8 = 0x10 // Break (not a real flag, artifact of push)
	FlagU uint8 = 0x20 // Unused (always 1 on push)
	FlagV uint8 = 0x40 // Overflow
	FlagN uint8 = 0x80 // Negative
)

// NZTable: precomputed N and Z flags for each byte value.
// 6502 has no parity, half-carry, or undocumented flag bits.
var NZTable [256]uint8

func init() {
	for i := 0; i < 256; i++ {
		var f uint8
		if i == 0 {
			f |= FlagZ
		}
		if i&0x80 != 0 {
			f |= FlagN
		}
		NZTable[i] = f
	}
}
