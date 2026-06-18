//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// buildTOC constructs a valid TOC byte from (config, stereo, code).
// config: 0..31. stereo: 0/1. code: 0..3.
func buildTOC(config, stereo, code int) byte {
	return byte((config&0x1F)<<3) | byte((stereo&0x1)<<2) | byte(code&0x3)
}

// -- TOC-byte inspectors --------------------------------------------------

// TestParity_OpusPacketGetBandwidth — sweep all 32 TOC configs and
// both stereo bits. The last four bits (code + stereo's LSB) don't
// affect bandwidth but we vary them too for completeness.
func TestParity_OpusPacketGetBandwidth(t *testing.T) {
	for config := 0; config < 32; config++ {
		for stereo := 0; stereo < 2; stereo++ {
			for code := 0; code < 4; code++ {
				toc := buildTOC(config, stereo, code)
				pkt := []byte{toc}
				want := cOpusPacketGetBandwidth(pkt)
				got := nativeopus.ExportTestOpusPacketGetBandwidth(pkt)
				if want != got {
					t.Errorf("toc=0x%02x: C=%d Go=%d", toc, want, got)
				}
			}
		}
	}
}

func TestParity_OpusPacketGetSamplesPerFrame(t *testing.T) {
	for config := 0; config < 32; config++ {
		for stereo := 0; stereo < 2; stereo++ {
			for code := 0; code < 4; code++ {
				toc := buildTOC(config, stereo, code)
				pkt := []byte{toc}
				for _, Fs := range []int32{8000, 12000, 16000, 24000, 48000} {
					want := cOpusPacketGetSamplesPerFrame(pkt, Fs)
					got := nativeopus.ExportTestOpusPacketGetSamplesPerFrame(pkt, Fs)
					if want != got {
						t.Errorf("toc=0x%02x Fs=%d: C=%d Go=%d", toc, Fs, want, got)
					}
				}
			}
		}
	}
}

func TestParity_OpusPacketGetNbChannels(t *testing.T) {
	for toc := 0; toc < 256; toc++ {
		pkt := []byte{byte(toc)}
		want := cOpusPacketGetNbChannels(pkt)
		got := nativeopus.ExportTestOpusPacketGetNbChannels(pkt)
		if want != got {
			t.Errorf("toc=0x%02x: C=%d Go=%d", toc, want, got)
		}
	}
}

// -- nb_frames / nb_samples -----------------------------------------------

func TestParity_OpusPacketGetNbFrames(t *testing.T) {
	// len=0, len=1 edge cases.
	for _, length := range []int32{0, 1, 2, 10} {
		for config := 0; config < 32; config++ {
			for code := 0; code < 4; code++ {
				toc := buildTOC(config, 0, code)
				// For code=3 we need a second byte encoding frame count.
				pkt := []byte{toc, 0x00}
				// count=0 is invalid but used to sweep rejection path.
				for frameCountByte := 0; frameCountByte < 64; frameCountByte += 7 {
					pkt[1] = byte(frameCountByte)
					want := cOpusPacketGetNbFrames(pkt, length)
					got := nativeopus.ExportTestOpusPacketGetNbFrames(pkt, length)
					if want != got {
						t.Errorf("toc=0x%02x byte1=0x%02x len=%d: C=%d Go=%d",
							toc, pkt[1], length, want, got)
					}
				}
			}
		}
	}
	// Explicit zero-length with nil-like empty slice.
	want := cOpusPacketGetNbFrames([]byte{0x00}, 0)
	got := nativeopus.ExportTestOpusPacketGetNbFrames([]byte{0x00}, 0)
	if want != got {
		t.Errorf("len=0: C=%d Go=%d", want, got)
	}
}

// buildCode0Packet constructs a self-contained code-0 packet with a
// dummy payload of `size` bytes.
func buildCode0Packet(config, stereo, size int, rng *rand.Rand) []byte {
	pkt := make([]byte, 1+size)
	pkt[0] = buildTOC(config, stereo, 0)
	for i := 1; i < len(pkt); i++ {
		pkt[i] = byte(rng.Intn(256))
	}
	return pkt
}

func TestParity_OpusPacketGetNbSamples(t *testing.T) {
	rng := rand.New(rand.NewSource(0xA55A))
	// Code 0 packets at various configs/sizes.
	for config := 0; config < 32; config++ {
		for stereo := 0; stereo < 2; stereo++ {
			for _, sz := range []int{0, 5, 50, 500} {
				pkt := buildCode0Packet(config, stereo, sz, rng)
				for _, Fs := range []int32{8000, 16000, 24000, 48000} {
					want := cOpusPacketGetNbSamples(pkt, int32(len(pkt)), Fs)
					got := nativeopus.ExportTestOpusPacketGetNbSamples(pkt, int32(len(pkt)), Fs)
					if want != got {
						t.Errorf("config=%d stereo=%d sz=%d Fs=%d: C=%d Go=%d",
							config, stereo, sz, Fs, want, got)
					}
				}
			}
		}
	}
}

// -- encode_size ----------------------------------------------------------

func TestParity_EncodeSize(t *testing.T) {
	// encode_size writes 1 byte for size<252, 2 bytes otherwise. Sweep
	// the full range the callers can produce (0..1275 for a valid Opus
	// frame; go a bit past to cover the 2-byte branch thoroughly).
	for size := 0; size <= 1300; size++ {
		cBuf := make([]byte, 2)
		gBuf := make([]byte, 2)
		cn := cEncodeSize(size, cBuf)
		gn := nativeopus.ExportTestEncodeSize(size, gBuf)
		if cn != gn {
			t.Errorf("size=%d: C returned %d, Go returned %d", size, cn, gn)
			continue
		}
		for i := 0; i < cn; i++ {
			if cBuf[i] != gBuf[i] {
				t.Errorf("size=%d byte%d: C=0x%02x Go=0x%02x",
					size, i, cBuf[i], gBuf[i])
			}
		}
	}
}

// -- opus_packet_parse ---------------------------------------------------

// buildCode1 constructs a CBR 2-frame packet with a deterministic
// payload of `2*frameLen` bytes.
func buildCode1(config, stereo, frameLen int, rng *rand.Rand) []byte {
	pkt := make([]byte, 1+2*frameLen)
	pkt[0] = buildTOC(config, stereo, 1)
	for i := 1; i < len(pkt); i++ {
		pkt[i] = byte(rng.Intn(256))
	}
	return pkt
}

// buildCode2 constructs a VBR 2-frame packet. The first frame length
// is written in the size-encoded prefix; the second takes the remainder.
func buildCode2(config, stereo, len0, len1 int, rng *rand.Rand) []byte {
	// size bytes (1 or 2) + len0 + len1 + TOC
	szBytes := 1
	if len0 >= 252 {
		szBytes = 2
	}
	pkt := make([]byte, 1+szBytes+len0+len1)
	pkt[0] = buildTOC(config, stereo, 2)
	if szBytes == 1 {
		pkt[1] = byte(len0)
	} else {
		pkt[1] = byte(252 + (len0 & 0x3))
		pkt[2] = byte((len0 - int(pkt[1])) >> 2)
	}
	for i := 1 + szBytes; i < len(pkt); i++ {
		pkt[i] = byte(rng.Intn(256))
	}
	return pkt
}

// buildCode3CBR — Code 3 CBR: TOC, frame-count byte (no pad/vbr),
// then count * frameLen payload bytes.
func buildCode3CBR(config, stereo, count, frameLen int, rng *rand.Rand) []byte {
	pkt := make([]byte, 2+count*frameLen)
	pkt[0] = buildTOC(config, stereo, 3)
	pkt[1] = byte(count & 0x3F) // VBR=0, pad=0
	for i := 2; i < len(pkt); i++ {
		pkt[i] = byte(rng.Intn(256))
	}
	return pkt
}

// buildCode3VBR — Code 3 VBR with per-frame sizes.
func buildCode3VBR(config, stereo int, sizes []int, rng *rand.Rand) []byte {
	// Header: TOC + (count|0x80). Then size bytes for first count-1
	// frames, then payloads.
	count := len(sizes)
	hdrSize := 2
	for i := 0; i < count-1; i++ {
		hdrSize++
		if sizes[i] >= 252 {
			hdrSize++
		}
	}
	total := 0
	for _, s := range sizes {
		total += s
	}
	pkt := make([]byte, hdrSize+total)
	pkt[0] = buildTOC(config, stereo, 3)
	pkt[1] = byte(count&0x3F) | 0x80
	pos := 2
	for i := 0; i < count-1; i++ {
		if sizes[i] < 252 {
			pkt[pos] = byte(sizes[i])
			pos++
		} else {
			pkt[pos] = byte(252 + (sizes[i] & 0x3))
			pkt[pos+1] = byte((sizes[i] - int(pkt[pos])) >> 2)
			pos += 2
		}
	}
	for ; pos < len(pkt); pos++ {
		pkt[pos] = byte(rng.Intn(256))
	}
	return pkt
}

// comparePPR — shared comparator for the parse-result tuple.
func comparePPR(t *testing.T, label string, c COpusPacketParseResult, g nativeopus.OpusPacketParseResult) {
	t.Helper()
	if c.Ret != g.Ret {
		t.Errorf("%s: ret C=%d Go=%d", label, c.Ret, g.Ret)
		return
	}
	if c.Ret <= 0 {
		return // error paths: other fields are unspecified.
	}
	if c.Toc != g.Toc {
		t.Errorf("%s: toc C=0x%02x Go=0x%02x", label, c.Toc, g.Toc)
	}
	if c.PayloadOffset != g.PayloadOffset {
		t.Errorf("%s: payload_offset C=%d Go=%d", label, c.PayloadOffset, g.PayloadOffset)
	}
	for i := 0; i < c.Ret; i++ {
		if c.FrameOffsets[i] != g.FrameOffsets[i] {
			t.Errorf("%s: frame_offsets[%d] C=%d Go=%d",
				label, i, c.FrameOffsets[i], g.FrameOffsets[i])
		}
		if c.Sizes[i] != g.Sizes[i] {
			t.Errorf("%s: sizes[%d] C=%d Go=%d",
				label, i, c.Sizes[i], g.Sizes[i])
		}
	}
}

func TestParity_OpusPacketParse_Code0(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for config := 0; config < 32; config++ {
		for stereo := 0; stereo < 2; stereo++ {
			for _, sz := range []int{0, 1, 10, 100, 252, 300, 1000} {
				pkt := buildCode0Packet(config, stereo, sz, rng)
				c := cOpusPacketParse(pkt)
				g := nativeopus.ExportTestOpusPacketParse(pkt)
				comparePPR(t, "code0", c, g)
			}
		}
	}
}

func TestParity_OpusPacketParse_Code1(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for config := 0; config < 32; config++ {
		for _, fl := range []int{1, 50, 500} {
			pkt := buildCode1(config, 0, fl, rng)
			c := cOpusPacketParse(pkt)
			g := nativeopus.ExportTestOpusPacketParse(pkt)
			comparePPR(t, "code1", c, g)
		}
	}
}

func TestParity_OpusPacketParse_Code2(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for config := 0; config < 32; config++ {
		for _, l0 := range []int{0, 1, 50, 251, 252, 500} {
			for _, l1 := range []int{0, 1, 50, 500} {
				pkt := buildCode2(config, 0, l0, l1, rng)
				c := cOpusPacketParse(pkt)
				g := nativeopus.ExportTestOpusPacketParse(pkt)
				comparePPR(t, "code2", c, g)
			}
		}
	}
}

func TestParity_OpusPacketParse_Code3_CBR(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	// count*framesize(@48kHz) <= 5760 — for config=0 (SILK-NB 10ms => 480
	// samples/frame at 48kHz), max count is 12. Use safe counts.
	for _, count := range []int{1, 2, 4, 6} {
		for _, fl := range []int{10, 100} {
			// Use config=0 (10ms SILK-NB) which has framesize=480 @48kHz.
			// count=6 → 2880 ≤ 5760 OK.
			pkt := buildCode3CBR(0, 0, count, fl, rng)
			c := cOpusPacketParse(pkt)
			g := nativeopus.ExportTestOpusPacketParse(pkt)
			comparePPR(t, "code3_cbr", c, g)
		}
	}
}

func TestParity_OpusPacketParse_Code3_VBR(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	cases := [][]int{
		{10, 20},
		{10, 20, 30},
		{5, 5, 5, 5},
		{100, 50, 75, 80, 40, 30},
	}
	for _, sizes := range cases {
		pkt := buildCode3VBR(0, 0, sizes, rng)
		c := cOpusPacketParse(pkt)
		g := nativeopus.ExportTestOpusPacketParse(pkt)
		comparePPR(t, "code3_vbr", c, g)
	}
}

// -- Repacketizer ---------------------------------------------------------

func TestParity_RepacketizerCatOut(t *testing.T) {
	rng := rand.New(rand.NewSource(0xBEEF))
	// Use config=0 (SILK NB 10ms) so framesize=80 @8kHz — 12 frames max
	// before the 960-sample limit kicks in.
	// Input packets: each a code-0 packet sharing the same high 6 TOC
	// bits. Vary payload size.
	configs := []struct {
		sizes []int
	}{
		{[]int{10}},
		{[]int{10, 20}},
		{[]int{10, 20, 30}},
		{[]int{5, 5, 5, 5, 5, 5}},
		{[]int{50, 75, 100}},
	}
	for _, cfg := range configs {
		packets := make([][]byte, len(cfg.sizes))
		for i, sz := range cfg.sizes {
			packets[i] = buildCode0Packet(0, 0, sz, rng)
		}
		maxlen := 2048
		cOut, cRet := cRepacketizerCatOut(packets, maxlen)
		gOut, gRet := nativeopus.ExportTestRepacketizerCatOut(packets, maxlen)
		if cRet != gRet {
			t.Errorf("cfg=%v: ret C=%d Go=%d", cfg.sizes, cRet, gRet)
			continue
		}
		if cRet < 0 {
			continue
		}
		if len(cOut) != len(gOut) {
			t.Errorf("cfg=%v: len C=%d Go=%d", cfg.sizes, len(cOut), len(gOut))
			continue
		}
		for i := 0; i < len(cOut); i++ {
			if cOut[i] != gOut[i] {
				t.Errorf("cfg=%v: byte %d: C=0x%02x Go=0x%02x",
					cfg.sizes, i, cOut[i], gOut[i])
				break
			}
		}
	}
}

func TestParity_OpusPacketPad(t *testing.T) {
	rng := rand.New(rand.NewSource(0xCAFE))
	// Build a simple code-0 packet and pad to a larger length.
	for _, payloadSize := range []int{10, 50, 200} {
		pkt := buildCode0Packet(0, 0, payloadSize, rng)
		for _, extra := range []int{0, 1, 10, 100} {
			newLen := len(pkt) + extra
			cOut, cRet := cOpusPacketPad(pkt, newLen)
			gOut, gRet := nativeopus.ExportTestOpusPacketPad(pkt, newLen)
			if cRet != gRet {
				t.Errorf("size=%d extra=%d: ret C=%d Go=%d",
					payloadSize, extra, cRet, gRet)
				continue
			}
			if cRet != 0 { // OPUS_OK
				continue
			}
			for i := 0; i < newLen; i++ {
				if cOut[i] != gOut[i] {
					t.Errorf("size=%d extra=%d: byte %d: C=0x%02x Go=0x%02x",
						payloadSize, extra, i, cOut[i], gOut[i])
					break
				}
			}
		}
	}
}

func TestParity_OpusPacketUnpad(t *testing.T) {
	rng := rand.New(rand.NewSource(0xF00D))
	// Build a padded packet first, then unpad.
	for _, payloadSize := range []int{10, 50} {
		pkt := buildCode0Packet(0, 0, payloadSize, rng)
		padded, padRet := cOpusPacketPad(pkt, len(pkt)+50)
		if padRet != 0 {
			continue
		}
		cOut, cRet := cOpusPacketUnpad(padded)
		gOut, gRet := nativeopus.ExportTestOpusPacketUnpad(padded)
		if cRet != gRet {
			t.Errorf("size=%d: ret C=%d Go=%d", payloadSize, cRet, gRet)
			continue
		}
		if cRet < 0 {
			continue
		}
		if len(cOut) != len(gOut) {
			t.Errorf("size=%d: len C=%d Go=%d", payloadSize, len(cOut), len(gOut))
			continue
		}
		for i := 0; i < len(cOut); i++ {
			if cOut[i] != gOut[i] {
				t.Errorf("size=%d: byte %d: C=0x%02x Go=0x%02x",
					payloadSize, i, cOut[i], gOut[i])
				break
			}
		}
	}
}
