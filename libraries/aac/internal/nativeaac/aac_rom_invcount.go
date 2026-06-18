// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// invCount ROM + GetInvInt accessor, ported 1:1 from the vendored Fraunhofer
// FDK-AAC reference (libFDK/include/fixpoint_math.h + libFDK/src/FDK_tools_rom.cpp).
// GetInvInt(n) returns 1/n as a FIXP_DBL (Q1.31), used by the encode-side
// intensity-stereo tool (intensity.go) to scale per-band sums by 1/N. Pure ROM
// + an integer table lookup — bit-identical regardless of build, so this file
// carries only the aacfdk license fence.

// invCount is the 1:1 transcription of const FIXP_DBL invCount[80]
// (FDK_tools_rom.cpp:6522): invCount[n] == round(2^31 / n) for n>=1, 0 for n==0.
//
//	const FIXP_DBL invCount[80] = { 0x00000000, 0x7fffffff, ... };
var invCount = [80]int32{
	0x00000000, 0x7fffffff, 0x40000000, 0x2aaaaaab, 0x20000000, 0x1999999a,
	0x15555555, 0x12492492, 0x10000000, 0x0e38e38e, 0x0ccccccd, 0x0ba2e8ba,
	0x0aaaaaab, 0x09d89d8a, 0x09249249, 0x08888889, 0x08000000, 0x07878788,
	0x071c71c7, 0x06bca1af, 0x06666666, 0x06186186, 0x05d1745d, 0x0590b216,
	0x05555555, 0x051eb852, 0x04ec4ec5, 0x04bda12f, 0x04924925, 0x0469ee58,
	0x04444444, 0x04210842, 0x04000000, 0x03e0f83e, 0x03c3c3c4, 0x03a83a84,
	0x038e38e4, 0x03759f23, 0x035e50d8, 0x03483483, 0x03333333, 0x031f3832,
	0x030c30c3, 0x02fa0be8, 0x02e8ba2f, 0x02d82d83, 0x02c8590b, 0x02b93105,
	0x02aaaaab, 0x029cbc15, 0x028f5c29, 0x02828283, 0x02762762, 0x026a439f,
	0x025ed098, 0x0253c825, 0x02492492, 0x023ee090, 0x0234f72c, 0x022b63cc,
	0x02222222, 0x02192e2a, 0x02108421, 0x02082082, 0x02000000, 0x01f81f82,
	0x01f07c1f, 0x01e9131b, 0x01e1e1e2, 0x01dae607, 0x01d41d42, 0x01cd8569,
	0x01c71c72, 0x01c0e070, 0x01bacf91, 0x01b4e81b, 0x01af286c, 0x01a98ef6,
	0x01a41a42, 0x019ec8e9,
}

// getInvInt is the 1:1 port of GetInvInt(int intValue) (fixpoint_math.h:948):
// clamp intValue to [0, 79] and return invCount[intValue].
//
//	inline FIXP_DBL GetInvInt(int intValue) {
//	  return invCount[fMin(fMax(intValue, 0), 80 - 1)];
//	}
func getInvInt(intValue int) int32 {
	return invCount[fixMin(fixMax(intValue, 0), 80-1)]
}
