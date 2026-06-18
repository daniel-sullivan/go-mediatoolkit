//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// encOp is a scripted encoder operation. Tests drive both the C and Go
// encoder with the same sequence and compare the byte output plus the
// reported state (rng, val, tell).
type encOp struct {
	kind string
	// encode: fl, fh, ft
	// encode_bin: fl, fh, bits
	// bit_logp: val, logp
	// icdf: s, icdf_id, ftb
	// icdf16: s, icdf16_id, ftb
	// uint: fl, ft
	// bits: fl, bits
	a, b, c uint32
}

// Canned ICDF tables used for the icdf/icdf16 ops.
var testICDF8 = []byte{200, 150, 100, 50, 0}
var testICDF16 = []uint16{30000, 20000, 10000, 5000, 0}

func runEncScript(enc interface {
	Encode(a, b, c uint32)
	EncodeBin(a, b uint32, c int)
	EncBitLogp(a, b int)
	EncIcdf(s int, icdf []byte, ftb int)
	EncIcdf16(s int, icdf []uint16, ftb int)
	EncUint(a, c uint32)
	EncBits(a uint32, b int)
}, script []encOp) {
	for _, o := range script {
		switch o.kind {
		case "encode":
			enc.Encode(o.a, o.b, o.c)
		case "encode_bin":
			enc.EncodeBin(o.a, o.b, int(o.c))
		case "bit_logp":
			enc.EncBitLogp(int(o.a), int(o.b))
		case "icdf":
			enc.EncIcdf(int(o.a), testICDF8[:], int(o.c))
		case "icdf16":
			enc.EncIcdf16(int(o.a), testICDF16[:], int(o.c))
		case "uint":
			enc.EncUint(o.a, o.c)
		case "bits":
			enc.EncBits(o.a, int(o.b))
		}
	}
}

// encWrapC / encWrapGo implement the interface runEncScript expects.
type encWrapC struct{ e cEc }

func (w encWrapC) Encode(a, b, c uint32)                   { w.e.Encode(a, b, c) }
func (w encWrapC) EncodeBin(a, b uint32, c int)            { w.e.EncodeBin(a, b, c) }
func (w encWrapC) EncBitLogp(a, b int)                     { w.e.EncBitLogp(a, b) }
func (w encWrapC) EncIcdf(s int, icdf []byte, ftb int)     { w.e.EncIcdf(s, icdf, ftb) }
func (w encWrapC) EncIcdf16(s int, icdf []uint16, ftb int) { w.e.EncIcdf16(s, icdf, ftb) }
func (w encWrapC) EncUint(a, c uint32)                     { w.e.EncUint(a, c) }
func (w encWrapC) EncBits(a uint32, b int)                 { w.e.EncBits(a, b) }

type encWrapGo struct{ h nativeopus.EcCtxHandle }

func (w encWrapGo) Encode(a, b, c uint32) {
	nativeopus.ExportTestEcEncode(w.h, a, b, c)
}
func (w encWrapGo) EncodeBin(a, b uint32, c int) {
	nativeopus.ExportTestEcEncodeBin(w.h, a, b, c)
}
func (w encWrapGo) EncBitLogp(a, b int) {
	nativeopus.ExportTestEcEncBitLogp(w.h, a, b)
}
func (w encWrapGo) EncIcdf(s int, icdf []byte, ftb int) {
	nativeopus.ExportTestEcEncIcdf(w.h, s, icdf, ftb)
}
func (w encWrapGo) EncIcdf16(s int, icdf []uint16, ftb int) {
	nativeopus.ExportTestEcEncIcdf16(w.h, s, icdf, ftb)
}
func (w encWrapGo) EncUint(a, c uint32) { nativeopus.ExportTestEcEncUint(w.h, a, c) }
func (w encWrapGo) EncBits(a uint32, b int) {
	nativeopus.ExportTestEcEncBits(w.h, a, b)
}

// runEncParity encodes the given op sequence via both C and Go, then
// calls ec_enc_done, and asserts both sides produced the same bytes.
func runEncParity(t *testing.T, label string, size int, script []encOp) {
	t.Helper()
	cBuf := make([]byte, size)
	goBuf := make([]byte, size)
	cE := cEcEncNew(cBuf)
	defer cE.Free()
	gH := nativeopus.ExportTestEcEncNew(goBuf)

	runEncScript(encWrapC{cE}, script)
	runEncScript(encWrapGo{gH}, script)

	if cE.Tell() != nativeopus.ExportTestEcTell(gH) {
		t.Errorf("%s: tell mismatch before done: C=%d Go=%d",
			label, cE.Tell(), nativeopus.ExportTestEcTell(gH))
	}
	if cE.Rng() != nativeopus.ExportTestEcRng(gH) {
		t.Errorf("%s: rng mismatch before done: C=0x%08x Go=0x%08x",
			label, cE.Rng(), nativeopus.ExportTestEcRng(gH))
	}
	if cE.Val() != nativeopus.ExportTestEcVal(gH) {
		t.Errorf("%s: val mismatch before done: C=0x%08x Go=0x%08x",
			label, cE.Val(), nativeopus.ExportTestEcVal(gH))
	}

	cE.EncDone()
	nativeopus.ExportTestEcEncDone(gH)

	if !bytes.Equal(cBuf, goBuf) {
		// Log first diverging byte.
		for i := 0; i < len(cBuf); i++ {
			if cBuf[i] != goBuf[i] {
				t.Errorf("%s: byte %d differs: C=0x%02x Go=0x%02x",
					label, i, cBuf[i], goBuf[i])
				break
			}
		}
	}
}

// TestParity_EcEncode_SimpleSequence — basic ec_encode calls.
func TestParity_EcEncode_SimpleSequence(t *testing.T) {
	script := []encOp{
		{kind: "encode", a: 0, b: 10, c: 100},
		{kind: "encode", a: 10, b: 25, c: 100},
		{kind: "encode", a: 25, b: 60, c: 100},
		{kind: "encode", a: 60, b: 100, c: 100},
		{kind: "encode", a: 0, b: 1, c: 2},
		{kind: "encode", a: 1, b: 2, c: 2},
	}
	runEncParity(t, "simple", 1024, script)
}

// TestParity_EcEncode_AllPrimitives — every encoder primitive mixed.
func TestParity_EcEncode_AllPrimitives(t *testing.T) {
	script := []encOp{
		{kind: "encode", a: 0, b: 50, c: 100},
		{kind: "encode_bin", a: 0, b: 1, c: 4},
		{kind: "bit_logp", a: 1, b: 3},
		{kind: "bit_logp", a: 0, b: 3},
		{kind: "icdf", a: 2, c: 8},
		{kind: "icdf", a: 0, c: 8},
		{kind: "icdf16", a: 2, c: 15},
		{kind: "uint", a: 42, c: 100},
		{kind: "uint", a: 1234, c: 65536},
		{kind: "bits", a: 0xabcd, b: 16},
		{kind: "bits", a: 0x3, b: 2},
	}
	runEncParity(t, "all_primitives", 1024, script)
}

// TestParity_EcEncode_Random — long randomised scripts. Covers carry
// propagation, normalization loops, and the full interaction surface.
func TestParity_EcEncode_Random(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for run := 0; run < 50; run++ {
		n := 20 + r.Intn(200)
		script := make([]encOp, 0, n)
		for i := 0; i < n; i++ {
			switch r.Intn(7) {
			case 0:
				ft := uint32(2 + r.Intn(255))
				fl := uint32(r.Intn(int(ft)))
				fh := fl + 1 + uint32(r.Intn(int(ft-fl)))
				script = append(script, encOp{kind: "encode", a: fl, b: fh, c: ft})
			case 1:
				bits := uint32(1 + r.Intn(12))
				ft := uint32(1 << bits)
				fl := uint32(r.Intn(int(ft)))
				fh := fl + 1 + uint32(r.Intn(int(ft-fl)))
				script = append(script, encOp{kind: "encode_bin", a: fl, b: fh, c: bits})
			case 2:
				script = append(script, encOp{kind: "bit_logp",
					a: uint32(r.Intn(2)), b: uint32(1 + r.Intn(7))})
			case 3:
				script = append(script, encOp{kind: "icdf",
					a: uint32(r.Intn(4)), c: 8})
			case 4:
				script = append(script, encOp{kind: "icdf16",
					a: uint32(r.Intn(4)), c: 15})
			case 5:
				ft := uint32(2 + r.Intn(1<<16))
				fl := uint32(r.Intn(int(ft)))
				script = append(script, encOp{kind: "uint", a: fl, c: ft})
			case 6:
				bits := uint32(1 + r.Intn(24))
				fl := uint32(r.Int31()) & ((1 << bits) - 1)
				script = append(script, encOp{kind: "bits", a: fl, b: bits})
			}
		}
		runEncParity(t, "random", 4096, script)
	}
}

// TestParity_EcDecode_RoundTrip — encode with C, decode the same bytes
// with both C and Go, confirm identical symbol streams.
func TestParity_EcDecode_RoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for run := 0; run < 20; run++ {
		// Build an op sequence whose results we can replay in the
		// decoder: only primitives that have a matching decode path.
		size := 4096
		cBuf := make([]byte, size)
		cE := cEcEncNew(cBuf)

		// Plain ec_encode calls — replayable via ec_decode+ec_dec_update.
		type plainOp struct{ fl, fh, ft uint32 }
		ops := []plainOp{}
		for i := 0; i < 100; i++ {
			ft := uint32(2 + r.Intn(255))
			fl := uint32(r.Intn(int(ft)))
			fh := fl + 1 + uint32(r.Intn(int(ft-fl)))
			ops = append(ops, plainOp{fl, fh, ft})
			cE.Encode(fl, fh, ft)
		}
		cE.EncDone()
		cE.Free()

		// Decode with both and compare at each step.
		cD := cEcDecNew(cBuf)
		gD := nativeopus.ExportTestEcDecNew(cBuf)
		for i, op := range ops {
			cS := cD.Decode(op.ft)
			gS := nativeopus.ExportTestEcDecode(gD, op.ft)
			if cS != gS {
				t.Fatalf("run %d op %d: decode mismatch C=%d Go=%d",
					run, i, cS, gS)
			}
			cD.DecUpdate(op.fl, op.fh, op.ft)
			nativeopus.ExportTestEcDecUpdate(gD, op.fl, op.fh, op.ft)
			if cD.Rng() != nativeopus.ExportTestEcRng(gD) {
				t.Fatalf("run %d op %d: post-update rng mismatch", run, i)
			}
			if cD.Val() != nativeopus.ExportTestEcVal(gD) {
				t.Fatalf("run %d op %d: post-update val mismatch", run, i)
			}
		}
		cD.Free()
	}
}

// TestParity_EcTellFrac — the fast LUT-based approximation path.
func TestParity_EcTellFrac(t *testing.T) {
	// Encode a varied stream so that rng+nbits_total hit many values,
	// then sample ec_tell_frac at each step.
	size := 4096
	cBuf := make([]byte, size)
	goBuf := make([]byte, size)
	cE := cEcEncNew(cBuf)
	defer cE.Free()
	gH := nativeopus.ExportTestEcEncNew(goBuf)

	r := rand.New(rand.NewSource(99))
	for i := 0; i < 500; i++ {
		ft := uint32(2 + r.Intn(1<<14))
		fl := uint32(r.Intn(int(ft)))
		fh := fl + 1 + uint32(r.Intn(int(ft-fl)))
		cE.Encode(fl, fh, ft)
		nativeopus.ExportTestEcEncode(gH, fl, fh, ft)
		if cE.TellFrac() != nativeopus.ExportTestEcTellFrac(gH) {
			t.Fatalf("step %d: tell_frac mismatch C=%d Go=%d rng_c=0x%x rng_go=0x%x",
				i, cE.TellFrac(), nativeopus.ExportTestEcTellFrac(gH),
				cE.Rng(), nativeopus.ExportTestEcRng(gH))
		}
	}
}
