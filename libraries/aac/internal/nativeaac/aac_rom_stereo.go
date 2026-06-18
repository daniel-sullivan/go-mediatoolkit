// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Shared decoder primitives and the inverse-quant / intensity-stereo gain ROM,
// ported 1:1 from the vendored Fraunhofer FDK-AAC reference. The
// inverse-quantizer area (invquant.go, invquant_tables.go), the joint-stereo
// apply driver (stereo_apply.go) and the TNS scale primitives (tns_scale.go)
// all reference dfractBits, fixnormzD and mantissaTable as shared package
// symbols; they are declared here once. Every definition is integer-domain and
// bit-identical regardless of build tag, so this file carries only the `aacfdk`
// license fence and no aac_strict FP split.

// dfractBits is DFRACT_BITS — the FIXP_DBL width.
//
// C counterpart: libFDK/include/common_fix.h:113
//
//	#define DFRACT_BITS 32 /* double precision */
const dfractBits = 32

// fixnormzD counts the leading redundant sign bits of a (i.e. the head room of
// the FIXP_DBL value), a 1:1 port of the generic-C fixnormz_D fallback.
//
// C counterpart: libFDK/include/clz.h:152
//
//	inline INT fixnormz_D(LONG a) {
//	  INT leadingBits = 0;
//	  a = ~a;
//	  while (a & 0x80000000) {
//	    leadingBits++;
//	    a <<= 1;
//	  }
//	  return (leadingBits);
//	}
//
// The bit-twiddling (operate on ~a, test the top bit) is reproduced exactly so
// the count matches the reference for every input. Pure integer kernel —
// bit-identical regardless of vectorization.
func fixnormzD(x int32) int32 {
	a := uint32(^x)
	var leadingBits int32
	for a&0x80000000 != 0 {
		leadingBits++
		a <<= 1
	}
	return leadingBits
}

// mantissaTable is the 1:1 transcription of MantissaTable (aac_rom.cpp:205):
// the Q1.31 gain mantissas 2^((sf%4)) split into mantissa/exponent pairs
// (paired with exponentTable in invquant_tables.go), indexed [sf%4][msb]. Both
// the inverse quantizer (evaluatePower43) and the intensity-stereo tools index
// it. Pure ROM constants — bit-identical regardless of build.
var mantissaTable = [4][14]int32{
	{0x40000000, 0x50A28C00, 0x6597FA80, 0x40000000, 0x50A28C00, 0x6597FA80,
		0x40000000, 0x50A28C00, 0x6597FA80, 0x40000000, 0x50A28C00, 0x6597FA80,
		0x40000000, 0x50A28C00},
	{0x4C1BF800, 0x5FE44380, 0x78D0DF80, 0x4C1BF800, 0x5FE44380, 0x78D0DF80,
		0x4C1BF800, 0x5FE44380, 0x78D0DF80, 0x4C1BF800, 0x5FE44380, 0x78D0DF80,
		0x4C1BF800, 0x5FE44380},
	{0x5A827980, 0x7208F800, 0x47D66B00, 0x5A827980, 0x7208F800, 0x47D66B00,
		0x5A827980, 0x7208F800, 0x47D66B00, 0x5A827980, 0x7208F800, 0x47D66B00,
		0x5A827980, 0x7208F800},
	{0x6BA27E80, 0x43CE3E80, 0x556E0400, 0x6BA27E80, 0x43CE3E80, 0x556E0400,
		0x6BA27E80, 0x43CE3E80, 0x556E0400, 0x6BA27E80, 0x43CE3E80, 0x556E0400,
		0x6BA27E80, 0x43CE3E80},
}
