//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// silkTestInputs is a deterministic cross-section of 32-bit values —
// powers of two and their neighbours, sign-boundary values, some
// random-but-fixed samples — to exercise each macro at edges plus a
// wide interior. Shared across every parity test in this file.
var silkTestInputs = func() []int32 {
	xs := []int32{
		0, 1, -1, 2, -2, 3, -3, 7, -7, 15, -15, 16, -16,
		255, -255, 256, -256, 32767, -32768,
		65535, -65535, 65536, -65536,
		0x00010000, 0x00100000, 0x01000000, 0x10000000, 0x40000000,
		0x7FFFFFFF, -0x80000000, 0x7FFFFFFE, -0x7FFFFFFF,
		0x55555555, 0x33333333, 0x0F0F0F0F, 0x01010101,
		-0x55555555, -0x33333333,
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		xs = append(xs, int32(r.Uint32()))
	}
	return xs
}()

func assertIntEq(t *testing.T, name string, want, got int32, args string) {
	t.Helper()
	if want != got {
		t.Errorf("%s(%s): want %d (0x%08x), got %d (0x%08x)",
			name, args, want, uint32(want), got, uint32(got))
	}
}
func assertInt64Eq(t *testing.T, name string, want, got int64, args string) {
	t.Helper()
	if want != got {
		t.Errorf("%s(%s): want %d (0x%016x), got %d (0x%016x)",
			name, args, want, uint64(want), got, uint64(got))
	}
}

func TestParity_SilkSMULXX(t *testing.T) {
	// Pairs of every interesting input combo — O(n²) but n=~240.
	for _, a := range silkTestInputs {
		for _, b := range silkTestInputs {
			assertIntEq(t, "silk_SMULWB", cSilkSMULWB(a, b),
				nativeopus.ExportTestSilkSMULWB(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SMULWT", cSilkSMULWT(a, b),
				nativeopus.ExportTestSilkSMULWT(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SMULBB", cSilkSMULBB(a, b),
				nativeopus.ExportTestSilkSMULBB(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SMULBT", cSilkSMULBT(a, b),
				nativeopus.ExportTestSilkSMULBT(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SMULWW", cSilkSMULWW(a, b),
				nativeopus.ExportTestSilkSMULWW(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SMMUL", cSilkSMMUL(a, b),
				nativeopus.ExportTestSilkSMMUL(a, b), fmtInts2(a, b))
			assertInt64Eq(t, "silk_SMULL", cSilkSMULL(a, b),
				nativeopus.ExportTestSilkSMULL(a, b), fmtInts2(a, b))
		}
	}
}

func TestParity_SilkSMLAXX(t *testing.T) {
	// Accumulator-style macros need a third input; sample a subset of
	// silkTestInputs as the accumulator to keep runtime reasonable.
	accs := silkTestInputs[:40]
	for _, acc := range accs {
		for _, a := range silkTestInputs {
			for _, b := range silkTestInputs {
				assertIntEq(t, "silk_SMLAWB", cSilkSMLAWB(acc, a, b),
					nativeopus.ExportTestSilkSMLAWB(acc, a, b), fmtInts3(acc, a, b))
				assertIntEq(t, "silk_SMLAWT", cSilkSMLAWT(acc, a, b),
					nativeopus.ExportTestSilkSMLAWT(acc, a, b), fmtInts3(acc, a, b))
				assertIntEq(t, "silk_SMLABB", cSilkSMLABB(acc, a, b),
					nativeopus.ExportTestSilkSMLABB(acc, a, b), fmtInts3(acc, a, b))
				assertIntEq(t, "silk_SMLABT", cSilkSMLABT(acc, a, b),
					nativeopus.ExportTestSilkSMLABT(acc, a, b), fmtInts3(acc, a, b))
				assertIntEq(t, "silk_SMLAWW", cSilkSMLAWW(acc, a, b),
					nativeopus.ExportTestSilkSMLAWW(acc, a, b), fmtInts3(acc, a, b))
			}
		}
	}
}

func TestParity_SilkSMLAL(t *testing.T) {
	for _, acc := range []int64{0, 1, -1, int64(1) << 40, -(int64(1) << 40),
		1<<62 - 1, -(1 << 62)} {
		for _, a := range silkTestInputs {
			for _, b := range silkTestInputs {
				assertInt64Eq(t, "silk_SMLAL", cSilkSMLAL(acc, a, b),
					nativeopus.ExportTestSilkSMLAL(acc, a, b),
					fmtInts3_64(acc, a, b))
			}
		}
	}
}

func TestParity_SilkCLZ(t *testing.T) {
	// int16 full sweep.
	for v := int32(-32768); v <= 32767; v++ {
		assertIntEq(t, "silk_CLZ16", cSilkCLZ16(int16(v)),
			nativeopus.ExportTestSilkCLZ16(int16(v)), fmtI(v))
	}
	for _, v := range silkTestInputs {
		assertIntEq(t, "silk_CLZ32", cSilkCLZ32(v),
			nativeopus.ExportTestSilkCLZ32(v), fmtI(v))
	}
	// int64 samples.
	vs64 := []int64{0, 1, -1, int64(1) << 32, -(int64(1) << 32),
		1<<62 - 1, -(1 << 62), 0x7FFFFFFFFFFFFFFF, -0x7FFFFFFFFFFFFFFF}
	for _, v := range vs64 {
		assertIntEq(t, "silk_CLZ64", cSilkCLZ64(v),
			nativeopus.ExportTestSilkCLZ64(v), fmtI64(v))
	}
}

func TestParity_SilkSATx(t *testing.T) {
	for _, a := range silkTestInputs {
		for _, b := range silkTestInputs {
			assertIntEq(t, "silk_ADD_SAT32", cSilkADDSAT32(a, b),
				nativeopus.ExportTestSilkADDSAT32(a, b), fmtInts2(a, b))
			assertIntEq(t, "silk_SUB_SAT32", cSilkSUBSAT32(a, b),
				nativeopus.ExportTestSilkSUBSAT32(a, b), fmtInts2(a, b))
			assertInt64Eq(t, "silk_ADD_SAT64",
				cSilkADDSAT64(int64(a), int64(b)),
				nativeopus.ExportTestSilkADDSAT64(int64(a), int64(b)),
				fmtInts2(a, b))
			assertInt64Eq(t, "silk_SUB_SAT64",
				cSilkSUBSAT64(int64(a), int64(b)),
				nativeopus.ExportTestSilkSUBSAT64(int64(a), int64(b)),
				fmtInts2(a, b))
		}
	}
}

func TestParity_SilkROR32(t *testing.T) {
	// silk_ROR32's C contract covers rot in (-32, 32); outside that
	// window it relies on UB shift-by-width behaviour that differs
	// from Go's defined zero result. SILK only ever calls it inside
	// the valid range.
	for _, a := range silkTestInputs {
		for rot := int32(-31); rot <= 31; rot++ {
			assertIntEq(t, "silk_ROR32", cSilkROR32(a, rot),
				nativeopus.ExportTestSilkROR32(a, int(rot)), fmtInts2(a, rot))
		}
	}
}

func TestParity_SilkRSHIFTROUND(t *testing.T) {
	for _, a := range silkTestInputs {
		for s := int32(1); s < 31; s++ {
			assertIntEq(t, "silk_RSHIFT_ROUND", cSilkRSHIFTROUND(a, s),
				nativeopus.ExportTestSilkRSHIFTROUND(a, int(s)), fmtInts2(a, s))
		}
	}
	vs64 := []int64{0, 1, -1, 0x7FFFFFFFFFFFFFFF, -0x7FFFFFFFFFFFFFFF,
		int64(1) << 40, -(int64(1) << 40)}
	for _, a := range vs64 {
		for s := int32(1); s < 63; s++ {
			assertInt64Eq(t, "silk_RSHIFT_ROUND64",
				cSilkRSHIFTROUND64(a, s),
				nativeopus.ExportTestSilkRSHIFTROUND64(a, int(s)),
				fmtI64_i(a, s))
		}
	}
}

func TestParity_SilkRAND(t *testing.T) {
	// Replay long PRNG sequences — bit-exact across the whole run is
	// essential for any code path that seeds the LCG once.
	seeds := []int32{0, 1, -1, 0x12345678, -0x7ABCDEF0}
	for _, s := range seeds {
		v := s
		for i := 0; i < 10000; i++ {
			cw := cSilkRAND(v)
			gw := nativeopus.ExportTestSilkRAND(v)
			if cw != gw {
				t.Fatalf("silk_RAND seed=%d step %d: want %d, got %d", s, i, cw, gw)
			}
			v = gw
		}
	}
}

func TestParity_SilkSQRTAPPROX(t *testing.T) {
	xs := []int32{0, 1, 2, 10, 100, 1000, 10000, 100000, 1000000,
		0x00FFFFFF, 0x01000000, 0x0FFFFFFF, 0x7FFFFFFF,
		-1, -100, -100000, int32(-0x80000000)}
	for v := int32(0); v < 1<<16; v += 137 {
		xs = append(xs, v)
	}
	for _, x := range xs {
		assertIntEq(t, "silk_SQRT_APPROX", cSilkSQRTAPPROX(x),
			nativeopus.ExportTestSilkSQRTAPPROX(x), fmtI(x))
	}
}

func TestParity_SilkDIV32varQ(t *testing.T) {
	// varQ routines assert b != 0, Q in-range; stay inside those.
	numerators := []int32{0, 1, -1, 1024, -1024, 1 << 20, -(1 << 20),
		0x7FFFFFFF, -0x7FFFFFFF, 0x12345678, -0x12345678}
	denoms := []int32{1, -1, 2, -2, 1024, -1024, 0x10000, -0x10000,
		0x7FFFFFFF, -0x7FFFFFFF, 0x12345678, -0x12345678}
	// Qres stays ≤ 29 because the internal `lshift = 29 + a_hr - b_hr
	// - Qres` can reach -32 at Qres=31 with a full-range numerator,
	// putting silk_LSHIFT_SAT32's width-equal shifts into the same
	// Go-vs-C UB territory as silk_ROR32. SILK itself calls this with
	// Qres in the 10-29 range.
	for _, q := range []int32{0, 1, 8, 14, 16, 24, 29} {
		for _, a := range numerators {
			for _, b := range denoms {
				assertIntEq(t, "silk_DIV32_varQ",
					cSilkDIV32varQ(a, b, q),
					nativeopus.ExportTestSilkDIV32varQ(a, b, int(q)),
					fmtInts3(a, b, q))
			}
		}
	}
}

func TestParity_SilkINVERSE32varQ(t *testing.T) {
	denoms := []int32{1, -1, 2, -2, 1024, -1024, 0x10000, -0x10000,
		0x7FFFFFFF, -0x7FFFFFFF, 0x12345678, -0x12345678}
	for _, q := range []int32{1, 8, 16, 24, 29} {
		for _, b := range denoms {
			assertIntEq(t, "silk_INVERSE32_varQ",
				cSilkINVERSE32varQ(b, q),
				nativeopus.ExportTestSilkINVERSE32varQ(b, int(q)),
				fmtInts2(b, q))
		}
	}
}

// ── formatting helpers ──────────────────────────────────────────────

func fmtI(a int32) string        { return fmtInts1(a) }
func fmtI64(a int64) string      { return fmtInts1_64(a) }
func fmtInts1(a int32) string    { return intStr(a) }
func fmtInts1_64(a int64) string { return intStr64(a) }
func fmtInts2(a, b int32) string { return intStr(a) + ", " + intStr(b) }
func fmtInts3(a, b, c int32) string {
	return intStr(a) + ", " + intStr(b) + ", " + intStr(c)
}
func fmtInts3_64(a int64, b, c int32) string {
	return intStr64(a) + ", " + intStr(b) + ", " + intStr(c)
}
func fmtI64_i(a int64, b int32) string {
	return intStr64(a) + ", " + intStr(b)
}

func intStr(a int32) string {
	return sprintfDec32(a)
}
func intStr64(a int64) string {
	return sprintfDec64(a)
}

// Local %d formatters avoid pulling fmt into every test hot loop.
func sprintfDec32(a int32) string {
	if a == 0 {
		return "0"
	}
	neg := false
	var u uint32
	if a < 0 {
		neg = true
		u = uint32(-a)
	} else {
		u = uint32(a)
	}
	var buf [12]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sprintfDec64(a int64) string {
	if a == 0 {
		return "0"
	}
	neg := false
	var u uint64
	if a < 0 {
		neg = true
		u = uint64(-a)
	} else {
		u = uint64(a)
	}
	var buf [21]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
