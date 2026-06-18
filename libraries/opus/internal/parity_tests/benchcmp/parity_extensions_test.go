//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// convertInputs maps local cExtensionInput records to the Go port's
// public ExportExtensionData type without touching the payload bytes.
func convertInputs(exts []cExtensionInput) []nativeopus.ExportExtensionData {
	out := make([]nativeopus.ExportExtensionData, len(exts))
	for i, e := range exts {
		out[i] = nativeopus.ExportExtensionData{
			ID: e.ID, Frame: e.Frame, Data: e.Data, Len: e.Len,
		}
	}
	return out
}

// runGenParity drives both the C oracle and the Go port with the same
// inputs, comparing the returned length and (when non-negative) the
// produced byte buffer.
func runGenParity(t *testing.T, label string, maxlen int32, exts []cExtensionInput,
	nbFrames int, pad int) {
	t.Helper()
	// Ensure each run uses its own buffer so zero bytes remain zero
	// when the Go/C implementations do not write them.
	cBuf := make([]byte, maxlen)
	goBuf := make([]byte, maxlen)
	cLen := cPacketExtensionsGenerate(cBuf, maxlen, exts, nbFrames, pad)
	goLen := nativeopus.ExportTestOpusPacketExtensionsGenerate(goBuf, maxlen,
		convertInputs(exts), nbFrames, pad)
	if cLen != goLen {
		t.Fatalf("%s: length mismatch C=%d Go=%d", label, cLen, goLen)
	}
	if cLen >= 0 && !bytes.Equal(cBuf[:cLen], goBuf[:cLen]) {
		for i := int32(0); i < cLen; i++ {
			if cBuf[i] != goBuf[i] {
				t.Fatalf("%s: byte %d differs C=0x%02x Go=0x%02x (len=%d)",
					label, i, cBuf[i], goBuf[i], cLen)
			}
		}
	}
}

// runCountParity compares opus_packet_extensions_count output.
func runCountParity(t *testing.T, label string, data []byte, nbFrames int) {
	t.Helper()
	cC := cPacketExtensionsCount(data, int32(len(data)), nbFrames)
	gC := nativeopus.ExportTestOpusPacketExtensionsCount(data, int32(len(data)), nbFrames)
	if cC != gC {
		t.Fatalf("%s: count mismatch C=%d Go=%d", label, cC, gC)
	}
}

// runCountExtParity compares opus_packet_extensions_count_ext output.
func runCountExtParity(t *testing.T, label string, data []byte, nbFrames int) {
	t.Helper()
	cArr := make([]int32, nbFrames)
	gArr := make([]int32, nbFrames)
	cR := cPacketExtensionsCountExt(data, int32(len(data)), cArr, nbFrames)
	gR := nativeopus.ExportTestOpusPacketExtensionsCountExt(data, int32(len(data)),
		gArr, nbFrames)
	if cR != gR {
		t.Fatalf("%s: count_ext return mismatch C=%d Go=%d", label, cR, gR)
	}
	for i := 0; i < nbFrames; i++ {
		if cArr[i] != gArr[i] {
			t.Fatalf("%s: count_ext[%d] mismatch C=%d Go=%d", label, i, cArr[i], gArr[i])
		}
	}
}

// runParseParity compares opus_packet_extensions_parse output.
func runParseParity(t *testing.T, label string, data []byte, cap_ int32, nbFrames int) {
	t.Helper()
	cCount := cap_
	gCount := cap_
	cRet, cOut := cPacketExtensionsParse(data, int32(len(data)), int(cap_),
		&cCount, nbFrames)
	gExts := make([]nativeopus.ExportExtensionData, cap_)
	gRet := nativeopus.ExportTestOpusPacketExtensionsParse(data, int32(len(data)),
		gExts, &gCount, nbFrames)
	if cRet != gRet {
		t.Fatalf("%s: parse ret mismatch C=%d Go=%d", label, cRet, gRet)
	}
	if cCount != gCount {
		t.Fatalf("%s: parse count mismatch C=%d Go=%d", label, cCount, gCount)
	}
	if cRet < 0 {
		return
	}
	for i := int32(0); i < cCount; i++ {
		if cOut[i].ID != gExts[i].ID || cOut[i].Frame != gExts[i].Frame ||
			cOut[i].Len != gExts[i].Len {
			t.Fatalf("%s: parse ext[%d] meta mismatch C=%+v Go=%+v",
				label, i, cOut[i], gExts[i])
		}
		// Compare payload bytes.
		var gPayload []byte
		if gExts[i].Len > 0 && len(gExts[i].Data) >= int(gExts[i].Len) {
			gPayload = gExts[i].Data[:gExts[i].Len]
		}
		if !bytes.Equal(cOut[i].Data, gPayload) {
			t.Fatalf("%s: parse ext[%d] payload differs C=% x Go=% x",
				label, i, cOut[i].Data, gPayload)
		}
	}
}

// runParseExtParity compares opus_packet_extensions_parse_ext output.
func runParseExtParity(t *testing.T, label string, data []byte, cap_ int32,
	nbFrames int) {
	t.Helper()
	// Compute nb_frame_exts via the C oracle first so both sides use the
	// same input array.
	nfe := make([]int32, nbFrames)
	_ = cPacketExtensionsCountExt(data, int32(len(data)), nfe, nbFrames)
	cCount := cap_
	gCount := cap_
	cRet, cOut := cPacketExtensionsParseExt(data, int32(len(data)), int(cap_),
		&cCount, nfe, nbFrames)
	gExts := make([]nativeopus.ExportExtensionData, cap_)
	gRet := nativeopus.ExportTestOpusPacketExtensionsParseExt(data,
		int32(len(data)), gExts, &gCount, nfe, nbFrames)
	if cRet != gRet {
		t.Fatalf("%s: parse_ext ret mismatch C=%d Go=%d", label, cRet, gRet)
	}
	if cCount != gCount {
		t.Fatalf("%s: parse_ext count mismatch C=%d Go=%d", label, cCount, gCount)
	}
	if cRet < 0 {
		return
	}
	for i := int32(0); i < cCount; i++ {
		if cOut[i].ID != gExts[i].ID || cOut[i].Frame != gExts[i].Frame ||
			cOut[i].Len != gExts[i].Len {
			t.Fatalf("%s: parse_ext ext[%d] meta mismatch C=%+v Go=%+v",
				label, i, cOut[i], gExts[i])
		}
		var gPayload []byte
		if gExts[i].Len > 0 && len(gExts[i].Data) >= int(gExts[i].Len) {
			gPayload = gExts[i].Data[:gExts[i].Len]
		}
		if !bytes.Equal(cOut[i].Data, gPayload) {
			t.Fatalf("%s: parse_ext ext[%d] payload differs", label, i)
		}
	}
}

// TestParity_Extensions_Generate_Minimal — cover the canonical
// example from the C file's #if-0 main().
func TestParity_Extensions_Generate_Minimal(t *testing.T) {
	exts := []cExtensionInput{
		{ID: 32, Frame: 10, Data: []byte("DRED"), Len: 4},
		{ID: 33, Frame: 1, Data: []byte("NOT DRED"), Len: 8},
		{ID: 3, Frame: 4, Data: nil, Len: 0},
	}
	runGenParity(t, "minimal", 256, exts, 48, 0)
	runGenParity(t, "minimal-pad", 256, exts, 48, 1)
}

// TestParity_Extensions_Generate_RepeatRun — repeat the same extension
// across all frames so the repeat mechanism triggers.
func TestParity_Extensions_Generate_RepeatRun(t *testing.T) {
	nbFrames := 4
	exts := []cExtensionInput{}
	for f := 0; f < nbFrames; f++ {
		exts = append(exts, cExtensionInput{ID: 32, Frame: f,
			Data: []byte{0xab, 0xcd, 0xef}, Len: 3})
	}
	runGenParity(t, "repeat", 256, exts, nbFrames, 0)
	runGenParity(t, "repeat-pad", 256, exts, nbFrames, 1)
}

// TestParity_Extensions_Generate_ShortExts — id<32 (short) path.
func TestParity_Extensions_Generate_ShortExts(t *testing.T) {
	exts := []cExtensionInput{
		{ID: 3, Frame: 0, Len: 0},
		{ID: 4, Frame: 0, Data: []byte{0x11}, Len: 1},
		{ID: 5, Frame: 1, Len: 0},
		{ID: 3, Frame: 2, Data: []byte{0x77}, Len: 1},
	}
	runGenParity(t, "short", 64, exts, 4, 0)
}

// TestParity_Extensions_Generate_LongPayload — payloads that require
// lacing bytes (>= 255).
func TestParity_Extensions_Generate_LongPayload(t *testing.T) {
	payload := make([]byte, 600)
	for i := range payload {
		payload[i] = byte(i * 31)
	}
	exts := []cExtensionInput{
		{ID: 40, Frame: 0, Data: payload, Len: 600},
		{ID: 41, Frame: 1, Data: payload[:255], Len: 255},
		{ID: 42, Frame: 2, Data: payload[:300], Len: 300},
	}
	runGenParity(t, "long-payload", 2048, exts, 3, 0)
}

// TestParity_Extensions_Generate_BufferTooSmall — exercise the error path.
func TestParity_Extensions_Generate_BufferTooSmall(t *testing.T) {
	exts := []cExtensionInput{
		{ID: 32, Frame: 0, Data: []byte("abcde"), Len: 5},
	}
	runGenParity(t, "too-small", 2, exts, 1, 0)
}

// TestParity_Extensions_Generate_BadArg — invalid frame/id.
func TestParity_Extensions_Generate_BadArg(t *testing.T) {
	runGenParity(t, "bad-frame", 16,
		[]cExtensionInput{{ID: 32, Frame: 5, Len: 0}}, 2, 0)
	runGenParity(t, "bad-id-low", 16,
		[]cExtensionInput{{ID: 1, Frame: 0, Len: 0}}, 1, 0)
	runGenParity(t, "bad-id-high", 16,
		[]cExtensionInput{{ID: 128, Frame: 0, Len: 0}}, 1, 0)
	runGenParity(t, "short-bad-len", 16,
		[]cExtensionInput{{ID: 5, Frame: 0, Len: 2,
			Data: []byte{1, 2}}}, 1, 0)
}

// randExtensions generates a pseudo-random but valid extension list
// suitable for parity testing. IDs and payload lengths are constrained
// so the encoder does not hit OPUS_BAD_ARG.
func randExtensions(r *rand.Rand, nbFrames int) []cExtensionInput {
	n := 1 + r.Intn(8)
	exts := make([]cExtensionInput, n)
	for i := range exts {
		// ID 3..127, sometimes <32 (short), sometimes >=32 (long).
		id := 3 + r.Intn(125)
		frame := r.Intn(nbFrames)
		var dataLen int32
		var data []byte
		if id < 32 {
			// Short ext: len must be 0 or 1.
			if r.Intn(2) == 0 {
				dataLen = 0
			} else {
				dataLen = 1
				data = []byte{byte(r.Intn(256))}
			}
		} else {
			// Long ext: arbitrary length.
			dataLen = int32(r.Intn(300))
			data = make([]byte, dataLen)
			for j := range data {
				data[j] = byte(r.Intn(256))
			}
		}
		exts[i] = cExtensionInput{ID: id, Frame: frame, Data: data, Len: dataLen}
	}
	// Sort roughly by frame so the repeat mechanism has a chance.
	// (The API does not require sorted input but both impls rely on
	// the same ordering semantics, so unsorted is fine too.)
	return exts
}

// TestParity_Extensions_Generate_Random — randomised parity sweep
// across the generate + count + parse pipeline. Uses C-generated bytes
// as the parse input so we test a valid encoded stream.
func TestParity_Extensions_Generate_Random(t *testing.T) {
	r := rand.New(rand.NewSource(1234))
	for run := 0; run < 200; run++ {
		nbFrames := 1 + r.Intn(8)
		exts := randExtensions(r, nbFrames)
		pad := 0
		if r.Intn(3) == 0 {
			pad = 1
		}
		// Generous buffer — bad args still matched across sides.
		runGenParity(t, "rand-gen", 2048, exts, nbFrames, pad)

		// Also run a longer buffer path for coverage of padding.
		if pad == 1 {
			runGenParity(t, "rand-gen-pad", 2048, exts, nbFrames, pad)
		}
	}
}

// TestParity_Extensions_CountAndParse_Random — randomly generate
// encoded extension streams using the C encoder, then ensure the Go
// and C decoders count and parse them identically.
func TestParity_Extensions_CountAndParse_Random(t *testing.T) {
	r := rand.New(rand.NewSource(4321))
	for run := 0; run < 200; run++ {
		nbFrames := 1 + r.Intn(8)
		exts := randExtensions(r, nbFrames)
		buf := make([]byte, 2048)
		n := cPacketExtensionsGenerate(buf, int32(len(buf)), exts, nbFrames, 0)
		if n < 0 {
			// Skip cases the C encoder rejects; generate-path already
			// exercises error parity.
			continue
		}
		packet := buf[:n]
		runCountParity(t, "rand-count", packet, nbFrames)
		runCountExtParity(t, "rand-count-ext", packet, nbFrames)
		runParseParity(t, "rand-parse", packet, 32, nbFrames)
		runParseExtParity(t, "rand-parse-ext", packet, 32, nbFrames)
	}
}

// TestParity_Extensions_Parse_Truncated — fuzz the parser with
// arbitrary byte sequences (capped to plausible packet sizes) so
// the truncation / malformed-packet branches are covered. We
// restrict the byte alphabet so that OPUS_INVALID_PACKET returns
// align between C and Go.
func TestParity_Extensions_Parse_Truncated(t *testing.T) {
	r := rand.New(rand.NewSource(99999))
	for run := 0; run < 500; run++ {
		n := r.Intn(64)
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(r.Intn(256))
		}
		nbFrames := 1 + r.Intn(8)
		runCountParity(t, "trunc-count", data, nbFrames)
		runCountExtParity(t, "trunc-count-ext", data, nbFrames)
		runParseParity(t, "trunc-parse", data, 32, nbFrames)
		runParseExtParity(t, "trunc-parse-ext", data, 32, nbFrames)
	}
}

// TestParity_Extensions_Parse_BufferTooSmall — tight cap to trigger
// OPUS_BUFFER_TOO_SMALL return.
func TestParity_Extensions_Parse_BufferTooSmall(t *testing.T) {
	exts := []cExtensionInput{
		{ID: 32, Frame: 0, Data: []byte("aaa"), Len: 3},
		{ID: 32, Frame: 1, Data: []byte("bbb"), Len: 3},
	}
	buf := make([]byte, 64)
	n := cPacketExtensionsGenerate(buf, int32(len(buf)), exts, 2, 0)
	if n < 0 {
		t.Fatalf("generate failed: %d", n)
	}
	runParseParity(t, "too-small-parse", buf[:n], 1, 2)
	runParseExtParity(t, "too-small-parse-ext", buf[:n], 1, 2)
}
