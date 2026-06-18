//go:build arm64

#include "textflag.h"

// func partitionAbsSum32NEON(p *int32, vecs int) uint32
//
// arm64 NEON helper for the 32-bit-accumulator branch of
// precompute_partition_info_sums_ (stream_encoder.c:4785; the Go port is
// precomputePartitionInfoSums_ in encoder_subframe.go). Sums abs(int32)
// over vecs*4 contiguous samples into a single uint32 (wrapping)
// accumulator.
//
// The four int32 lanes accumulate independently and are horizontally
// summed (ADDV) at the end; both the per-lane ADD and the ADDV reduction
// are int32 two's-complement, so the wrapping is associative and the
// result is bit-exact vs the scalar `absResidualPartitionSum += absI32(r)`
// uint32 accumulation. Integer-exact -> built in BOTH default and
// flac_strict. The Go caller computes per-partition start offsets and the
// <4 remainder, and uses this only on the 32-bit-accumulator path (it
// keeps the scalar uint64 path for the pessimistic-64-bit branch).
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose
// MOVI.4S, LDR Q post-increment, ABS.4S, ADD.4S, ADDV.4S, or FMOV w<-s;
// the encodings were produced by Apple clang's assembler.
//
// Register map: R0 p; R1 vecs; V16 accumulator; V0/V1 scratch; R0 result.
TEXT ·partitionAbsSum32NEON(SB), NOSPLIT, $0-20
	MOVD p+0(FP), R0
	MOVD vecs+8(FP), R1

	WORD $0x4f000410         // movi.4s v16, #0
	CBZ  R1, reduce

loop:
	WORD $0x3cc10400         // ldr q0, [x0], #16   ; load 4 samples, p+=16
	WORD $0x4ea0b801         // abs.4s v1, v0
	WORD $0x4ea18610         // add.4s v16, v16, v1
	SUB  $1, R1, R1
	CBNZ R1, loop

reduce:
	WORD $0x4eb1ba10         // addv.4s s16, v16
	WORD $0x1e260200         // fmov   w0, s16
	MOVW R0, ret+16(FP)
	RET
