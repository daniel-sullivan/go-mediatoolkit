//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_EcLaplaceGetFreq1 — the file-static helper that maps
// (fs0, decay) → the PDF frequency of |value|==1.
func TestParity_EcLaplaceGetFreq1(t *testing.T) {
	for _, fs0 := range []uint32{0, 128, 1024, 8192, 16384, 30000, 32000} {
		for decay := 0; decay <= 11456; decay += 97 {
			c := cEcLaplaceGetFreq1(fs0, decay)
			g := nativeopus.ExportTestEcLaplaceGetFreq1(fs0, decay)
			if c != g {
				t.Errorf("fs0=%d decay=%d: C=%d Go=%d", fs0, decay, c, g)
			}
		}
	}
}

// TestParity_EcLaplace_Encode — every encode call should produce the
// same byte stream and leave the coder in the same (rng, val) state.
func TestParity_EcLaplace_Encode(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for run := 0; run < 30; run++ {
		n := 5 + r.Intn(40)
		values := make([]int, n)
		fss := make([]uint32, n)
		decays := make([]int, n)
		for i := 0; i < n; i++ {
			values[i] = r.Intn(41) - 20
			fss[i] = uint32(2000 + r.Intn(20000))
			decays[i] = r.Intn(11457)
		}

		cBuf := make([]byte, 4096)
		goBuf := make([]byte, 4096)
		cE := cEcEncNew(cBuf)
		defer cE.Free()
		gH := nativeopus.ExportTestEcEncNew(goBuf)

		for i := 0; i < n; i++ {
			cv := values[i]
			gv := values[i]
			cEcLaplaceEncode(cE, &cv, fss[i], decays[i])
			nativeopus.ExportTestEcLaplaceEncode(gH, &gv, fss[i], decays[i])
			if cv != gv {
				t.Errorf("run %d op %d: value-out mismatch C=%d Go=%d",
					run, i, cv, gv)
			}
			if cE.Rng() != nativeopus.ExportTestEcRng(gH) ||
				cE.Val() != nativeopus.ExportTestEcVal(gH) {
				t.Fatalf("run %d op %d: state mismatch", run, i)
			}
		}

		cE.EncDone()
		nativeopus.ExportTestEcEncDone(gH)
		if !bytes.Equal(cBuf, goBuf) {
			t.Errorf("run %d: final bytes differ", run)
		}
	}
}

// TestParity_EcLaplace_RoundTrip — encode with C, decode with both
// and compare the recovered symbols.
func TestParity_EcLaplace_RoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for run := 0; run < 20; run++ {
		n := 5 + r.Intn(40)
		values := make([]int, n)
		fss := make([]uint32, n)
		decays := make([]int, n)
		for i := 0; i < n; i++ {
			values[i] = r.Intn(41) - 20
			fss[i] = uint32(2000 + r.Intn(20000))
			decays[i] = r.Intn(11457)
		}
		cBuf := make([]byte, 4096)
		cE := cEcEncNew(cBuf)
		saved := make([]int, n)
		for i := range values {
			cv := values[i]
			cEcLaplaceEncode(cE, &cv, fss[i], decays[i])
			saved[i] = cv // encoder may rewrite overflowed values
		}
		cE.EncDone()
		cE.Free()

		cD := cEcDecNew(cBuf)
		defer cD.Free()
		gD := nativeopus.ExportTestEcDecNew(cBuf)
		for i := 0; i < n; i++ {
			cGot := cEcLaplaceDecode(cD, fss[i], decays[i])
			gGot := nativeopus.ExportTestEcLaplaceDecode(gD, fss[i], decays[i])
			if cGot != gGot {
				t.Fatalf("run %d op %d: decode mismatch C=%d Go=%d saved=%d",
					run, i, cGot, gGot, saved[i])
			}
			if cGot != saved[i] {
				t.Fatalf("run %d op %d: C round-trip lost value: saved=%d got=%d",
					run, i, saved[i], cGot)
			}
		}
	}
}

// TestParity_EcLaplaceP0 — encode_p0 / decode_p0 round-trip and state
// check for the QEXT variant.
func TestParity_EcLaplaceP0(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for run := 0; run < 20; run++ {
		n := 5 + r.Intn(20)
		values := make([]int, n)
		for i := range values {
			values[i] = r.Intn(61) - 30
		}
		p0 := uint16(1000 + r.Intn(30000))
		decay := uint16(1000 + r.Intn(30000))

		cBuf := make([]byte, 4096)
		goBuf := make([]byte, 4096)
		cE := cEcEncNew(cBuf)
		gH := nativeopus.ExportTestEcEncNew(goBuf)
		for _, v := range values {
			cEcLaplaceEncodeP0(cE, v, p0, decay)
			nativeopus.ExportTestEcLaplaceEncodeP0(gH, v, p0, decay)
		}
		cE.EncDone()
		nativeopus.ExportTestEcEncDone(gH)
		if !bytes.Equal(cBuf, goBuf) {
			t.Fatalf("run %d: encode_p0 bytes differ", run)
		}
		cE.Free()

		cD := cEcDecNew(cBuf)
		gD := nativeopus.ExportTestEcDecNew(cBuf)
		for i, v := range values {
			cGot := cEcLaplaceDecodeP0(cD, p0, decay)
			gGot := nativeopus.ExportTestEcLaplaceDecodeP0(gD, p0, decay)
			if cGot != v {
				t.Fatalf("run %d op %d: C round-trip lost value: want=%d got=%d",
					run, i, v, cGot)
			}
			if cGot != gGot {
				t.Fatalf("run %d op %d: C=%d Go=%d", run, i, cGot, gGot)
			}
		}
		cD.Free()
	}
}
