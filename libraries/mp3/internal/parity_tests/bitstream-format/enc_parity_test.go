// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package bitstreamformat

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 port of LAME 3.100's encoder
// frame-assembler (bitstream_format.go) and bit-reservoir framing
// (reservoir_encode.go) against the vendored C reference (enc_oracle.c, which
// #includes the committed liblame bitstream.c / reservoir.c / tables.c /
// version.c). Every routine is driven on both sides over identical fabricated
// state and the outcomes must be bit-for-bit equal.
//
// This slice is integer-only except reservoir.c's two double scalings in
// ResvMaxBits, so the assertions are gated behind nativemp3.StrictMode per the
// FP-parity convention: a bare `go test` is clean and the strict run
// (-tags='mp3lame mp3_strict' + the FP CGO env via mise //libraries/mp3:parity)
// is the authoritative bit-exact gate.
//
// SCOPE NOTE — writeMainData / format_bitstream are driven with empty Huffman
// regions (big_values == count1 == 0, all table_select 0). The Go ht[] codebook
// CODE-WORD table (.Table) is still unpopulated (the tables.c port owns it; see
// huffman_encode.go:51), so the Huffman EMITTERS (Huffmancode /
// huffman_coder_count1) cannot yet emit non-empty regions in the Go port. With
// empty regions those emitters write nothing and read no .Table entry, so the
// SCALEFACTOR-stream half of writeMainData and the full format_bitstream framing
// are exercised bit-exact. The big-value Huffman emission stays covered by the
// sibling huffman-encode slice, to be extended once ht[].Table lands.

// bitrateTable mirrors nativemp3's (init.go) bitrate_table[version][index]
// (tables.c:526): rows are [MPEG-2, MPEG-1, MPEG-2.5] for cfg->version
// 0/1/2. A value <= 0 (0 = free format / index 0, -1 = reserved index) means
// getframebits' `bit_rate` would be out of the asserted [8,640] range, so the
// tests skip those combinations (the encoder never feeds an invalid index).
var bitrateTable = [3][16]int{
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1},     // MPEG 2
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1}, // MPEG 1
	{0, 8, 16, 24, 32, 40, 48, 56, 64, -1, -1, -1, -1, -1, -1, -1},         // MPEG 2.5
}

func bitrateTableVal(version, index int) int { return bitrateTable[version][index] }

// sideinfoLenFor computes cfg->sideinfo_len exactly as lame_init_params does
// (lame.c:964-969): the 4-byte MPEG header plus the Layer III side info
// (MPEG-1: 17 mono / 32 stereo; MPEG-2/2.5: 9 mono / 17 stereo), plus 2 when
// error protection is on. encodeSideInfo2 asserts the bits it writes equal
// sideinfo_len*8, so the tests must size it correctly.
func sideinfoLenFor(version, channels, errorProtection int) int {
	var n int
	if version == 1 { // MPEG-1
		if channels == 1 {
			n = 4 + 17
		} else {
			n = 4 + 32
		}
	} else { // MPEG-2 / 2.5
		if channels == 1 {
			n = 4 + 9
		} else {
			n = 4 + 17
		}
	}
	if errorProtection != 0 {
		n += 2
	}
	return n
}

func encRequireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:parity)")
	}
}

// sfbBandLong is a realistic MPEG-1 44.1 kHz long scalefactor-band boundary
// table (tables.c sfBandIndex[].l for samplerate index 0), monotonically
// increasing to 576. LongHuffmancodebits indexes scalefac_band.l[region*+1];
// with big_values == 0 the regions clamp to 0 but the array read still happens,
// so both sides need an identical valid table.
var sfbBandLong = [23]int{
	0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162,
	196, 238, 288, 342, 418, 576,
}

// sfbBandShort is the matching short table (sfBandIndex[].s), *3 not applied
// here; ShortHuffmancodebits reads scalefac_band.s[3].
var sfbBandShort = [14]int{
	0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192,
}

// applySfbBands primes scalefac_band.l[0..22] / .s[0..13] on both sides.
func applySfbBands(c *cgoEnc, n *nativeEnc) {
	for i, v := range sfbBandLong {
		c.setSfbL(i, v)
		n.setSfbL(i, v)
	}
	for i, v := range sfbBandShort {
		c.setSfbS(i, v)
		n.setSfbS(i, v)
	}
}

// baseCfg is a representative MPEG-1 Layer III 44.1 kHz stereo config (sideinfo
// 32 bytes, 2 granules). Tests vary fields off it.
func baseCfg() encCfg {
	return encCfg{
		version:          1, // MPEG-1
		samplerateOut:    44100,
		samplerateIndex:  0,
		sideinfoLen:      32, // MPEG-1 stereo
		channelsOut:      2,
		modeGr:           2,
		mode:             0, // STEREO
		errorProtection:  0,
		extension:        0,
		copyright:        0,
		original:         1,
		emphasis:         0,
		disableReservoir: 0,
		avgBitrate:       128,
		bufferConstraint: 8 * 1440,
	}
}

// TestParityGetframebits sweeps getframebits / calcFrameLength across versions,
// sample rates, bitrate indices and padding. getframebits indexes
// bitrate_table[version][bitrate_index]; bitrate_index 0 uses avg_bitrate.
func TestParityGetframebits(t *testing.T) {
	encRequireStrict(t)
	type vrow struct {
		version    int
		samplerate int
		srIndex    int
	}
	rows := []vrow{
		{1, 44100, 0}, {1, 48000, 1}, {1, 32000, 2}, // MPEG-1
		{0, 22050, 0}, {0, 24000, 1}, {0, 16000, 2}, // MPEG-2
		{2, 11025, 0}, {2, 12000, 1}, {2, 8000, 2}, // MPEG-2.5
	}
	for _, vr := range rows {
		for bitrateIndex := 0; bitrateIndex < 15; bitrateIndex++ {
			// getframebits asserts 8 <= bit_rate <= 640 (NDEBUG off in the
			// vendored config.h). bit_rate is bitrate_table[version][index]
			// for index!=0 (avg_bitrate otherwise). The encoder only ever calls
			// it with a valid index, so skip table entries <= 0 to match that
			// contract rather than tripping the C assert.
			if bitrateIndex != 0 && bitrateTableVal(vr.version, bitrateIndex) <= 0 {
				continue
			}
			for _, padding := range []int{0, 1} {
				cf := baseCfg()
				cf.version = vr.version
				cf.samplerateOut = vr.samplerate
				cf.samplerateIndex = vr.srIndex
				cf.avgBitrate = 128

				c := newCgoEnc(8192)
				n := newNativeEnc(8192)
				c.setCfg(cf)
				n.setCfg(cf)
				c.setOv(bitrateIndex, padding, 0)
				n.setOv(bitrateIndex, padding, 0)

				require.Equal(t, c.getFrameBits(), n.getFrameBits(),
					"getframebits v=%d sr=%d br=%d pad=%d", vr.version, vr.samplerate, bitrateIndex, padding)
				for _, kbps := range []int{32, 64, 128, 192, 256, 320} {
					require.Equal(t, c.calcFrameLength(kbps, padding), n.calcFrameLength(kbps, padding),
						"calcFrameLength v=%d sr=%d kbps=%d", vr.version, vr.samplerate, kbps)
				}
				c.free()
				n.free()
			}
		}
	}
}

// TestParityMaxFrameBufferSize sweeps get_max_frame_buffer_size_by_constraint
// across version / samplerate / avg_bitrate and all three constraint policies
// (0=DEFAULT, 1=STRICT_ISO, 2=MAXIMUM) plus an out-of-range constraint that must
// hit the `default` branch.
func TestParityMaxFrameBufferSize(t *testing.T) {
	encRequireStrict(t)
	for _, version := range []int{0, 1, 2} {
		for _, sr := range []int{8000, 16000, 22050, 32000, 44100, 48000} {
			for _, avg := range []int{64, 128, 320, 400} {
				cf := baseCfg()
				cf.version = version
				cf.samplerateOut = sr
				cf.avgBitrate = avg
				c := newCgoEnc(64)
				n := newNativeEnc(64)
				c.setCfg(cf)
				n.setCfg(cf)
				for _, constraint := range []int{0, 1, 2, 511, -1} {
					require.Equal(t,
						c.getMaxFrameBufferSizeByConstraint(constraint),
						n.getMaxFrameBufferSizeByConstraint(constraint),
						"maxbuf v=%d sr=%d avg=%d c=%d", version, sr, avg, constraint)
				}
				c.free()
				n.free()
			}
		}
	}
}

// TestParityCRCUpdate sweeps CRC_update over all byte values and a range of
// running CRC states.
func TestParityCRCUpdate(t *testing.T) {
	encRequireStrict(t)
	for value := 0; value < 256; value++ {
		for _, crc := range []int{0xffff, 0x0000, 0x1234, 0x8005, 0xabcd} {
			require.Equal(t, cgoCRCUpdate(value, crc), nativeCRCUpdate(value, crc),
				"CRC_update value=%d crc=%#x", value, crc)
		}
	}
}

// TestParityWriteHeader drives a random sequence of writeheader calls (the
// side-info bit packer) into a fresh header slot on both sides and compares the
// resulting header buffer + bit cursor.
func TestParityWriteHeader(t *testing.T) {
	encRequireStrict(t)
	for seed := uint64(1); seed <= 12; seed++ {
		cf := baseCfg()
		c := newCgoEnc(64)
		n := newNativeEnc(64)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setSv(0, 0, 0, 0, 0)
		n.setSv(0, 0, 0, 0, 0)
		// header[0].ptr starts at 0.
		c.primeHeader(0, 0, 0, nil)
		n.primeHeader(0, 0, 0, nil)

		r := rand.New(rand.NewPCG(seed, seed+5))
		bitsUsed := 0
		for step := 0; step < 40 && bitsUsed < cf.sideinfoLen*8-12; step++ {
			j := r.IntN(12) + 1 // 1..12 bits, < MAX_LENGTH
			if bitsUsed+j > cf.sideinfoLen*8 {
				break
			}
			val := r.IntN(1 << uint(j))
			c.writeHeader(val, j)
			n.writeHeader(val, j)
			bitsUsed += j
		}
		require.Equal(t, c.headerPtr(0), n.headerPtr(0), "writeheader ptr seed=%d", seed)
		require.Equal(t, c.headerBuf(0, cf.sideinfoLen), n.headerBuf(0, cf.sideinfoLen),
			"writeheader buf seed=%d", seed)
		c.free()
		n.free()
	}
}

// TestParityCRCWriteheader fabricates a random 32-byte header and pins
// CRC_writeheader's CRC-16 over bytes [2],[3],[6..sideinfo_len), writing bytes
// [4],[5].
func TestParityCRCWriteheader(t *testing.T) {
	encRequireStrict(t)
	for seed := uint64(1); seed <= 20; seed++ {
		cf := baseCfg()
		c := newCgoEnc(64)
		n := newNativeEnc(64)
		c.setCfg(cf)
		n.setCfg(cf)
		hdr := makeRandomBytes(seed*3+1, cf.sideinfoLen)
		hc := append([]byte(nil), hdr...)
		hn := append([]byte(nil), hdr...)
		c.crcWriteHeader(hc)
		n.crcWriteHeader(hn)
		require.Equal(t, hc, hn, "CRC_writeheader seed=%d", seed)
		c.free()
		n.free()
	}
}

// TestParityDrainIntoAncillary pins drain_into_ancillary: the LAME tag + version
// string + alternating ancillary_flag stuffing, across a range of remainingBits
// and both reservoir-enabled/disabled (which toggles the ancillary_flag XOR).
func TestParityDrainIntoAncillary(t *testing.T) {
	encRequireStrict(t)
	for _, disableResv := range []int{0, 1} {
		for _, remaining := range []int{0, 1, 7, 8, 16, 31, 32, 33, 40, 72, 100, 257} {
			for _, ancStart := range []int{0, 1} {
				cf := baseCfg()
				cf.disableReservoir = disableResv
				c := newCgoEnc(4096)
				n := newNativeEnc(4096)
				c.setCfg(cf)
				n.setCfg(cf)
				// Start with an empty buffer: buf_byte_idx -1 so the first
				// putbits2 byte-advances cleanly. Disarm headers so no splice.
				c.setBs(0, -1, 0)
				n.setBs(0, -1, 0)
				c.setSv(0, 0, ancStart, 0, 0)
				n.setSv(0, 0, ancStart, 0, 0)
				c.disarmHeaders(1 << 30)
				n.disarmHeaders(1 << 30)

				c.drainIntoAncillary(remaining)
				n.drainIntoAncillary(remaining)

				require.Equal(t, c.bsTotbit(), n.bsTotbit(),
					"drain totbit disv=%d rem=%d", disableResv, remaining)
				require.Equal(t, c.bsBufByteIdx(), n.bsBufByteIdx(),
					"drain buf_byte_idx disv=%d rem=%d", disableResv, remaining)
				require.Equal(t, c.bsBufBitIdx(), n.bsBufBitIdx(),
					"drain buf_bit_idx disv=%d rem=%d", disableResv, remaining)
				require.Equal(t, c.ancillaryFlag(), n.ancillaryFlag(),
					"drain ancillary_flag disv=%d rem=%d", disableResv, remaining)
				nb := c.bsBufByteIdx() + 1
				if nb > 0 {
					require.Equal(t, c.bsBuf(nb), n.bsBuf(nb),
						"drain bytes disv=%d rem=%d", disableResv, remaining)
				}
				c.free()
				n.free()
			}
		}
	}
}

// TestParityAddDummyByte pins add_dummy_byte: n stuffing bytes + the
// write_timing shift of every header slot.
func TestParityAddDummyByte(t *testing.T) {
	encRequireStrict(t)
	for _, n := range []uint{0, 1, 3, 8} {
		for _, val := range []byte{0x00, 0xAA, 0xFF} {
			c := newCgoEnc(256)
			ne := newNativeEnc(256)
			cf := baseCfg()
			c.setCfg(cf)
			ne.setCfg(cf)
			c.setBs(0, -1, 0)
			ne.setBs(0, -1, 0)
			// Prime a few header slots with distinct write_timings.
			for slot := 0; slot < 4; slot++ {
				c.primeHeader(slot, 1000+slot*8, 0, nil)
				ne.primeHeader(slot, 1000+slot*8, 0, nil)
			}
			c.addDummyByte(val, n)
			ne.addDummyByte(val, n)
			require.Equal(t, c.bsTotbit(), ne.bsTotbit(), "add_dummy_byte totbit n=%d", n)
			require.Equal(t, c.bsBufByteIdx(), ne.bsBufByteIdx(), "add_dummy_byte idx n=%d", n)
			for slot := 0; slot < 4; slot++ {
				require.Equal(t, c.headerWriteTiming(slot), ne.headerWriteTiming(slot),
					"add_dummy_byte wt slot=%d n=%d", slot, n)
			}
			nb := c.bsBufByteIdx() + 1
			if nb > 0 {
				require.Equal(t, c.bsBuf(nb), ne.bsBuf(nb), "add_dummy_byte bytes n=%d", n)
			}
			c.free()
			ne.free()
		}
	}
}

// TestParityDoCopyBuffer pins do_copy_buffer / copy_buffer: the bit-buffer drain
// (returns the byte count or -1 on too-small), and that copy_buffer(mp3data=0)
// matches (mp3data=1's VbrTag side effects are out of this slice's scope).
func TestParityDoCopyBuffer(t *testing.T) {
	encRequireStrict(t)
	for seed := uint64(1); seed <= 10; seed++ {
		cf := baseCfg()
		c := newCgoEnc(2048)
		n := newNativeEnc(2048)
		c.setCfg(cf)
		n.setCfg(cf)
		// Lay down some bytes via drain, then snapshot buf_byte_idx and copy.
		c.setBs(0, -1, 0)
		n.setBs(0, -1, 0)
		c.disarmHeaders(1 << 30)
		n.disarmHeaders(1 << 30)
		nbits := int(seed)*37 + 8
		c.drainIntoAncillary(nbits)
		n.drainIntoAncillary(nbits)

		idx := c.bsBufByteIdx()
		require.Equal(t, idx, n.bsBufByteIdx(), "pre-copy idx seed=%d", seed)
		want := idx + 1

		// too-small buffer -> -1.
		smallC := make([]byte, want-1)
		smallN := make([]byte, want-1)
		require.Equal(t, c.doCopyBuffer(smallC, want-1), n.doCopyBuffer(smallN, want-1),
			"do_copy_buffer too-small seed=%d", seed)

		bufC := make([]byte, want+16)
		bufN := make([]byte, want+16)
		gotC := c.doCopyBuffer(bufC, len(bufC))
		gotN := n.doCopyBuffer(bufN, len(bufN))
		require.Equal(t, gotC, gotN, "do_copy_buffer count seed=%d", seed)
		require.Equal(t, bufC[:gotC], bufN[:gotN], "do_copy_buffer bytes seed=%d", seed)
		// state reset
		require.Equal(t, c.bsBufByteIdx(), n.bsBufByteIdx(), "post-copy idx seed=%d", seed)
		require.Equal(t, c.bsBufBitIdx(), n.bsBufBitIdx(), "post-copy bitidx seed=%d", seed)
		c.free()
		n.free()
	}
}

// TestParityReservoir pins the four reservoir.c framing entry points over random
// frame state: ResvFrameBegin (ResvMax sizing + budget), ResvMaxBits (targ/extra
// split, incl. the two double scalings), ResvAdjust (debit), ResvFrameEnd
// (credit + byte-align + drain into pre/post). substep_shaping is swept to hit
// both the ResvMax*0.9 and the targBits-.1*mean_bits paths.
func TestParityReservoir(t *testing.T) {
	encRequireStrict(t)
	r := rand.New(rand.NewPCG(777, 778))
	for i := 0; i < 1500; i++ {
		cf := baseCfg()
		cf.version = []int{0, 1}[r.IntN(2)]
		if cf.version == 0 {
			cf.modeGr = 1
			cf.samplerateOut = []int{22050, 24000, 16000}[r.IntN(3)]
			cf.sideinfoLen = []int{17, 9}[r.IntN(2)] // MPEG-2 stereo/mono
		} else {
			cf.modeGr = 2
			cf.samplerateOut = []int{44100, 48000, 32000}[r.IntN(3)]
			cf.sideinfoLen = []int{32, 17}[r.IntN(2)] // MPEG-1 stereo/mono
		}
		cf.avgBitrate = []int{64, 128, 192, 320, 400}[r.IntN(5)]
		cf.disableReservoir = r.IntN(2)
		cf.bufferConstraint = []int{8 * 1440, 7680, 8 * 1951, 2000}[r.IntN(4)]

		// pick a bitrate index whose table entry is valid (or 0 -> avg_bitrate),
		// so getframebits' bit_rate stays in the asserted [8,640] range.
		bitrateIndex := r.IntN(15)
		for bitrateIndex != 0 && bitrateTableVal(cf.version, bitrateIndex) <= 0 {
			bitrateIndex = r.IntN(15)
		}
		padding := r.IntN(2)
		resvSize := r.IntN(8192)
		resvMax := r.IntN(8192)
		substep := r.IntN(256)

		c := newCgoEnc(64)
		n := newNativeEnc(64)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setOv(bitrateIndex, padding, 0)
		n.setOv(bitrateIndex, padding, 0)
		c.setSv(0, 0, 0, resvSize, resvMax)
		n.setSv(0, 0, 0, resvSize, resvMax)
		c.setSubstepShaping(substep)
		n.setSubstepShaping(substep)
		mdb := r.IntN(512)
		c.setSide(mdb, 0, 0, 0)
		n.setSide(mdb, 0, 0, 0)

		// ResvFrameBegin
		ffbC, mbC := c.resvFrameBegin()
		ffbN, mbN := n.resvFrameBegin()
		require.Equal(t, ffbC, ffbN, "ResvFrameBegin fullFrameBits i=%d", i)
		require.Equal(t, mbC, mbN, "ResvFrameBegin meanBits i=%d", i)
		require.Equal(t, c.resvMax(), n.resvMax(), "ResvFrameBegin ResvMax i=%d", i)
		require.Equal(t, c.resvDrainPre(), n.resvDrainPre(), "ResvFrameBegin drain_pre i=%d", i)

		// ResvMaxBits (cbr both ways)
		cbr := r.IntN(2)
		tbC, ebC := c.resvMaxBits(mbC, cbr)
		tbN, ebN := n.resvMaxBits(mbN, cbr)
		require.Equal(t, tbC, tbN, "ResvMaxBits targ i=%d", i)
		require.Equal(t, ebC, ebN, "ResvMaxBits extra i=%d", i)
		require.Equal(t, c.substepShaping(), n.substepShaping(), "ResvMaxBits substep i=%d", i)

		// ResvAdjust a granule
		c.setGr(0, 0, encGr{part23Length: r.IntN(4000), part2Length: r.IntN(500)})
		n.setGr(0, 0, encGr{part23Length: 0, part2Length: 0})
		// mirror the exact part lengths to both sides
		p23 := r.IntN(4000)
		p2 := r.IntN(500)
		c.setGr(0, 0, encGr{part23Length: p23, part2Length: p2})
		n.setGr(0, 0, encGr{part23Length: p23, part2Length: p2})
		c.resvAdjust(0, 0)
		n.resvAdjust(0, 0)
		require.Equal(t, c.resvSize(), n.resvSize(), "ResvAdjust ResvSize i=%d", i)

		// ResvFrameEnd
		c.resvFrameEnd(mbC)
		n.resvFrameEnd(mbN)
		require.Equal(t, c.resvSize(), n.resvSize(), "ResvFrameEnd ResvSize i=%d", i)
		require.Equal(t, c.resvDrainPre(), n.resvDrainPre(), "ResvFrameEnd drain_pre i=%d", i)
		require.Equal(t, c.resvDrainPost(), n.resvDrainPost(), "ResvFrameEnd drain_post i=%d", i)
		require.Equal(t, c.mainDataBegin(), n.mainDataBegin(), "ResvFrameEnd mdb i=%d", i)

		c.free()
		n.free()
	}
}

// primeEmptyGranule sets gr (gr,ch) to a Huffman-empty granule (big_values ==
// count1 == 0, all table_select 0) with the given block type and scalefactors,
// so encodeSideInfo2 / writeMainData exercise the header + scalefactor paths
// without touching the unpopulated ht[].Table. part2Length / part23Length are
// the (caller-precomputed) scalefactor and Huffman bit counts; writeMainData
// asserts data_bits == part2_3_length and scale_bits == part2_length, so they
// must be exact. scalefacCompress selects slen1/slen2 (MPEG-1); sfbdivide splits
// the slen1/slen2 regions.
func primeEmptyGranule(c *cgoEnc, n *nativeEnc, gr, ch, blockType int, scalefacs []int,
	scalefacCompress, sfbdivide, part2Length, part23Length int) {
	g := encGr{
		part23Length:      part23Length,
		part2Length:       part2Length,
		bigValues:         0,
		count1:            0,
		globalGain:        128,
		scalefacCompress:  scalefacCompress,
		blockType:         blockType,
		mixedBlockFlag:    0,
		region0Count:      0,
		region1Count:      0,
		preflag:           0,
		scalefacScale:     0,
		count1tableSelect: 0,
		sfbdivide:         sfbdivide,
		sfbmax:            len(scalefacs),
	}
	c.setGr(gr, ch, g)
	n.setGr(gr, ch, g)
	for idx := 0; idx < 3; idx++ {
		c.setGrTableSelect(gr, ch, idx, 0)
		n.setGrTableSelect(gr, ch, idx, 0)
		c.setGrSubblockGain(gr, ch, idx, 0)
		n.setGrSubblockGain(gr, ch, idx, 0)
	}
	for sfb, v := range scalefacs {
		c.setGrScalefac(gr, ch, sfb, v)
		n.setGrScalefac(gr, ch, sfb, v)
	}
}

// TestParityEncodeSideInfo2 pins encodeSideInfo2: the 4-byte header + Layer III
// side-info packing into the header ring slot, across MPEG-1/2, mono/stereo,
// error-protection on/off, and normal/short block types. With Huffman-empty
// granules the part2_3_length etc. are exact, so the full side-info bitstream is
// comparable byte-for-byte. Also checks the table_select 14->16 remap and h_ptr
// / write_timing scheduling.
func TestParityEncodeSideInfo2(t *testing.T) {
	encRequireStrict(t)
	type sc struct {
		version    int
		channels   int
		modeGr     int
		errorProt  int
		samplerate int
		srIndex    int
		blockType  int
	}
	cases := []sc{
		{1, 2, 2, 0, 44100, 0, 0}, // MPEG-1 stereo long
		{1, 1, 2, 0, 44100, 0, 0}, // MPEG-1 mono long
		{1, 2, 2, 1, 48000, 1, 0}, // MPEG-1 stereo long + CRC
		{1, 2, 2, 0, 32000, 2, 2}, // MPEG-1 stereo short
		{0, 2, 1, 0, 22050, 0, 0}, // MPEG-2 stereo long
		{0, 1, 1, 0, 24000, 1, 2}, // MPEG-2 mono short
		{0, 2, 1, 1, 16000, 2, 0}, // MPEG-2 stereo long + CRC
	}
	for ci, cs := range cases {
		cf := baseCfg()
		cf.version = cs.version
		cf.channelsOut = cs.channels
		cf.sideinfoLen = sideinfoLenFor(cs.version, cs.channels, cs.errorProt)
		cf.modeGr = cs.modeGr
		cf.errorProtection = cs.errorProt
		cf.samplerateOut = cs.samplerate
		cf.samplerateIndex = cs.srIndex
		if cs.channels == 1 {
			cf.mode = 3 // MONO
		}

		c := newCgoEnc(256)
		n := newNativeEnc(256)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setOv(9, 0, 0) // bitrate index 9, no padding
		n.setOv(9, 0, 0)
		c.setSv(0, 0, 0, 0, 0)
		n.setSv(0, 0, 0, 0, 0)
		c.setSide(17, 0, 0, 0) // main_data_begin
		n.setSide(17, 0, 0, 0)
		applySfbBands(c, n)

		// scfsi (MPEG-1 only reads it)
		for ch := 0; ch < cs.channels; ch++ {
			for band := 0; band < 4; band++ {
				v := (ci + ch + band) & 1
				c.setScfsi(ch, band, v)
				n.setScfsi(ch, band, v)
			}
		}

		grs := cs.modeGr
		for gr := 0; gr < grs; gr++ {
			for ch := 0; ch < cs.channels; ch++ {
				// encodeSideInfo2 only writes part2_3_length+part2_length as a
				// 12-bit field; any consistent values work (it does not run the
				// writeMainData asserts). Use a distinct nonzero per granule.
				primeEmptyGranule(c, n, gr, ch, cs.blockType, nil, 0, 0,
					(gr*2+ch)*7, (gr*2+ch)*13)
				// Exercise the table_select 14->16 remap on one entry.
				c.setGrTableSelect(gr, ch, 0, 14)
				n.setGrTableSelect(gr, ch, 0, 14)
			}
		}

		bitsPerFrame := c.getFrameBits()
		require.Equal(t, bitsPerFrame, n.getFrameBits(), "case %d bitsPerFrame", ci)

		c.encodeSideInfo2(bitsPerFrame)
		n.encodeSideInfo2(bitsPerFrame)

		require.Equal(t, c.headerBuf(0, cf.sideinfoLen), n.headerBuf(0, cf.sideinfoLen),
			"encodeSideInfo2 header bytes case %d", ci)
		require.Equal(t, c.headerPtr(0), n.headerPtr(0), "encodeSideInfo2 ptr case %d", ci)
		require.Equal(t, c.hPtr(), n.hPtr(), "encodeSideInfo2 h_ptr case %d", ci)
		require.Equal(t, c.headerWriteTiming(c.hPtr()), n.headerWriteTiming(n.hPtr()),
			"encodeSideInfo2 next write_timing case %d", ci)
		// table_select remap landed identically.
		for gr := 0; gr < grs; gr++ {
			for ch := 0; ch < cs.channels; ch++ {
				require.Equal(t, c.grTableSelect(gr, ch, 0), n.grTableSelect(gr, ch, 0),
					"table_select remap case %d gr%d ch%d", ci, gr, ch)
			}
		}
		c.free()
		n.free()
	}
}

// slen1Tab / slen2Tab mirror takehiro.c:961-962 — the scalefac_compress ->
// (slen1, slen2) bit-length tables writeMainData reads for the MPEG-1 path.
var slen1Tab = [16]int{0, 0, 0, 0, 3, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4}
var slen2Tab = [16]int{0, 1, 2, 3, 0, 1, 2, 3, 1, 2, 3, 1, 2, 3, 2, 3}

// TestParityWriteMainData pins writeMainData's scalefactor stream (Huffman
// regions empty per the scope note) for MPEG-1 and MPEG-2 (LSF partitioned),
// long and short blocks. The part2_length / part2_3_length fields are computed
// to exactly match the emitted scalefactor / Huffman bit counts so the C debug
// asserts (data_bits == part2_3_length, scale_bits == part2_length) hold.
func TestParityWriteMainData(t *testing.T) {
	encRequireStrict(t)
	type sc struct {
		version   int
		channels  int
		modeGr    int
		blockType int
		lsf       bool
	}
	cases := []sc{
		{1, 2, 2, 0, false}, // MPEG-1 stereo long
		{1, 1, 2, 2, false}, // MPEG-1 mono short
		{0, 2, 1, 0, true},  // MPEG-2 stereo long (partitioned)
		{0, 1, 1, 2, true},  // MPEG-2 mono short (partitioned)
	}
	for ci, cs := range cases {
		cf := baseCfg()
		cf.version = cs.version
		cf.channelsOut = cs.channels
		cf.sideinfoLen = sideinfoLenFor(cs.version, cs.channels, 0)
		cf.modeGr = cs.modeGr
		cf.samplerateOut = map[int]int{0: 22050, 1: 44100}[cs.version]
		if cs.channels == 1 {
			cf.mode = 3
		}
		c := newCgoEnc(4096)
		n := newNativeEnc(4096)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setBs(0, -1, 0)
		n.setBs(0, -1, 0)
		c.disarmHeaders(1 << 30)
		n.disarmHeaders(1 << 30)
		applySfbBands(c, n)

		const scalefacCompress = 8 // slen1=2, slen2=1 (both nonzero)
		const sfbdivide = 11
		slen1 := slen1Tab[scalefacCompress]
		slen2 := slen2Tab[scalefacCompress]

		grs := cs.modeGr
		for gr := 0; gr < grs; gr++ {
			for ch := 0; ch < cs.channels; ch++ {
				// scalefactors: a spread of small non-negative values (and a -1
				// in the MPEG-1 case to exercise the scfsi skip).
				sf := make([]int, 21)
				for i := range sf {
					sf[i] = (i*3 + ci + gr + ch) % 4 // < 2^slen so it fits
				}
				if !cs.lsf && len(sf) > 5 {
					sf[5] = -1 // scfsi reuse skip (MPEG-1 only)
				}

				var part2Length int
				if !cs.lsf {
					// MPEG-1: slen1 for [0,sfbdivide), slen2 for [sfbdivide,sfbmax);
					// bands == -1 are skipped (scfsi reuse).
					for i := 0; i < len(sf); i++ {
						if sf[i] == -1 {
							continue
						}
						if i < sfbdivide {
							part2Length += slen1
						} else {
							part2Length += slen2
						}
					}
				}
				primeEmptyGranule(c, n, gr, ch, cs.blockType, sf, scalefacCompress, sfbdivide, part2Length, 0)

				if cs.lsf {
					// LSF partition table + slen[4]. part2_length == scale_bits.
					part := [4]int{6, 6, 6, 0}
					slen := [4]int{1, 2, 0, 0}
					var scaleBits int
					if cs.blockType == 2 {
						// short: sfb_partition_table[p]/3 sfbs, each emitting 3
						// scalefactors. Keep total sfb small (<= SBMAX_s) so
						// scalefac[sfb*3+2] stays within scalefac[SFBMAX=39].
						part = [4]int{6, 6, 6, 0} // /3 -> 2 sfbs each (6 total)
						for p := 0; p < 4; p++ {
							scaleBits += (part[p] / 3) * 3 * slen[p]
						}
					} else {
						for p := 0; p < 4; p++ {
							scaleBits += part[p] * slen[p]
						}
					}
					// reset part2_length to the partitioned scale_bits.
					primeEmptyGranule(c, n, gr, ch, cs.blockType, sf, scalefacCompress, sfbdivide, scaleBits, 0)
					c.setGrPartition(gr, ch, part, slen)
					n.setGrPartition(gr, ch, part, slen)
				}
			}
		}

		gotC := c.writeMainData()
		gotN := n.writeMainData()
		require.Equal(t, gotC, gotN, "writeMainData bits case %d", ci)
		require.Equal(t, c.bsTotbit(), n.bsTotbit(), "writeMainData totbit case %d", ci)
		nb := c.bsBufByteIdx() + 1
		require.Equal(t, nb, n.bsBufByteIdx()+1, "writeMainData nbytes case %d", ci)
		if nb > 0 {
			require.Equal(t, c.bsBuf(nb), n.bsBuf(nb), "writeMainData bytes case %d", ci)
		}
		c.free()
		n.free()
	}
}

// TestParityComputeFlushbits pins compute_flushbits: the flushbits result and
// the totalBytesOutput out-value, over a hand-built header ring (w_ptr / h_ptr
// and per-slot write_timing) and a range of totbit / sideinfo_len.
func TestParityComputeFlushbits(t *testing.T) {
	encRequireStrict(t)
	r := rand.New(rand.NewPCG(2024, 2025))
	for i := 0; i < 600; i++ {
		cf := baseCfg()
		cf.version = []int{0, 1}[r.IntN(2)]
		if cf.version == 0 {
			cf.modeGr = 1
			cf.samplerateOut = 22050
			cf.sideinfoLen = 17
		} else {
			cf.modeGr = 2
			cf.samplerateOut = 44100
			cf.sideinfoLen = 32
		}
		bitrateIndex := r.IntN(15)
		for bitrateIndex != 0 && bitrateTableVal(cf.version, bitrateIndex) <= 0 {
			bitrateIndex = r.IntN(15)
		}

		c := newCgoEnc(64)
		n := newNativeEnc(64)
		c.setCfg(cf)
		n.setCfg(cf)
		padding := r.IntN(2)
		c.setOv(bitrateIndex, padding, 0)
		n.setOv(bitrateIndex, padding, 0)

		totbit := r.IntN(100000)
		bufByteIdx := r.IntN(40)
		c.setBs(totbit, bufByteIdx, 0)
		n.setBs(totbit, bufByteIdx, 0)

		// Build a small header ring: w_ptr first, h_ptr a few slots ahead, with
		// monotone write_timings >= totbit (the C contract).
		wPtr := r.IntN(8)
		nHeaders := r.IntN(4) + 1
		hPtr := (wPtr + nHeaders) & (256 - 1)
		c.setSv(hPtr, wPtr, 0, 0, 0)
		n.setSv(hPtr, wPtr, 0, 0, 0)
		wt := totbit + r.IntN(50000)
		for k := 0; k <= nHeaders; k++ {
			slot := (wPtr + k) & (256 - 1)
			c.primeHeader(slot, wt, 0, nil)
			n.primeHeader(slot, wt, 0, nil)
			wt += r.IntN(5000) + 1
		}

		fbC, tboC := c.computeFlushbits()
		fbN, tboN := n.computeFlushbits()
		require.Equal(t, fbC, fbN, "compute_flushbits flushbits i=%d", i)
		require.Equal(t, tboC, tboN, "compute_flushbits totalBytesOutput i=%d", i)
		c.free()
		n.free()
	}
}

// TestParityFormatBitstream pins the full format_bitstream end-to-end: pre/post
// ancillary drain, encodeSideInfo2, writeMainData (scalefactor-only per the
// scope note) and the main_data_begin update. To satisfy the C's
// `assert(totbit % 8 == 0)` the frame is constructed byte-aligned:
// disable_reservoir so resvDrain_pre/post can be set to multiples of 8, and the
// per-granule scalefactor bits are padded to keep the main data byte-aligned.
// The two ERRORF consistency checks call the (stubbed) lame_errorf and do not
// abort.
func TestParityFormatBitstream(t *testing.T) {
	encRequireStrict(t)
	type sc struct {
		version   int
		channels  int
		modeGr    int
		blockType int
	}
	// MPEG-1 only: the MPEG-1 writeMainData path packs scalefactors with the
	// slen tables (no sfb_partition_table needed). The MPEG-2 LSF scalefactor
	// stream is covered bit-exact by TestParityWriteMainData; driving the full
	// MPEG-2 frame here would need a self-consistent sfb_partition_table the
	// quantizer normally fills, which is outside this framing slice.
	cases := []sc{
		{1, 2, 2, 0}, // MPEG-1 stereo long
		{1, 1, 2, 0}, // MPEG-1 mono long
		{1, 2, 2, 2}, // MPEG-1 stereo short
		{1, 1, 2, 2}, // MPEG-1 mono short
	}
	for ci, cs := range cases {
		cf := baseCfg()
		cf.version = cs.version
		cf.channelsOut = cs.channels
		cf.sideinfoLen = sideinfoLenFor(cs.version, cs.channels, 0)
		cf.modeGr = cs.modeGr
		cf.samplerateOut = map[int]int{0: 22050, 1: 44100}[cs.version]
		cf.disableReservoir = 1
		if cs.channels == 1 {
			cf.mode = 3
		}

		c := newCgoEnc(8192)
		n := newNativeEnc(8192)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setOv(9, 0, 0)
		n.setOv(9, 0, 0)
		// fresh writer: buf_byte_idx -1, totbit 0; header slot 0 disarmed so no
		// spurious splice (write_timing far away).
		c.setBs(0, -1, 0)
		n.setBs(0, -1, 0)
		c.disarmHeaders(1 << 30)
		n.disarmHeaders(1 << 30)
		c.setSv(0, 0, 0, 0, 0)
		n.setSv(0, 0, 0, 0, 0)
		c.setSide(0, 0, 0, 0) // main_data_begin 0, drain pre/post 0
		n.setSide(0, 0, 0, 0)
		applySfbBands(c, n)

		const scalefacCompress = 8 // slen1=2, slen2=1
		const sfbdivide = 11
		slen1 := slen1Tab[scalefacCompress]
		slen2 := slen2Tab[scalefacCompress]

		// Each granule emits exactly part2 scalefactor bits (Huffman empty). The
		// MPEG-1 writeMainData assert is data_bits == part2_3_length +
		// part2_length where data_bits == part2_length (Huffman = 0), so
		// part2_3_length must be 0 and part2_length == part2.
		part2PerGr := 0
		for i := 0; i < 21; i++ {
			if i < sfbdivide {
				part2PerGr += slen1
			} else {
				part2PerGr += slen2
			}
		}
		for gr := 0; gr < cs.modeGr; gr++ {
			for ch := 0; ch < cs.channels; ch++ {
				sf := make([]int, 21)
				for i := range sf {
					sf[i] = (i + ci + gr + ch) % 4
				}
				primeEmptyGranule(c, n, gr, ch, cs.blockType, sf, scalefacCompress, sfbdivide,
					part2PerGr, 0)
			}
		}

		// totbit after format_bitstream == drain_pre(0) + sideinfo_len*8 +
		// sum(main data) + drain_post. sideinfo_len*8 is byte-aligned; the main
		// data per frame is part2PerGr per (gr,ch). Absorb any misalignment into
		// resvDrain_post (the post-frame ancillary drain) so the C's
		// `assert(totbit % 8 == 0)` holds; both sides apply it identically.
		totalMain := part2PerGr * cs.modeGr * cs.channels
		drainPost := (8 - (totalMain % 8)) % 8
		c.setSide(0, 0, 0, drainPost)
		n.setSide(0, 0, 0, drainPost)

		rcC := c.formatBitstream()
		rcN := n.formatBitstream()
		require.Equal(t, rcC, rcN, "format_bitstream rc case %d", ci)
		require.Equal(t, c.bsTotbit(), n.bsTotbit(), "format_bitstream totbit case %d", ci)
		require.Equal(t, 0, c.bsTotbit()%8, "format_bitstream totbit byte-aligned case %d", ci)
		require.Equal(t, c.mainDataBegin(), n.mainDataBegin(), "format_bitstream mdb case %d", ci)
		require.Equal(t, c.hPtr(), n.hPtr(), "format_bitstream h_ptr case %d", ci)
		nb := c.bsBufByteIdx() + 1
		require.Equal(t, nb, n.bsBufByteIdx()+1, "format_bitstream nbytes case %d", ci)
		if nb > 0 {
			require.Equal(t, c.bsBuf(nb), n.bsBuf(nb), "format_bitstream bytes case %d", ci)
		}
		c.free()
		n.free()
	}
}

// TestParityFlushBitstream pins flush_bitstream by running it in its natural
// position — right after a format_bitstream frame — so the header ring is
// self-consistent and flush_bitstream's internal
// `assert(header[last_ptr].write_timing + getframebits == bs.totbit)` holds. It
// checks the drained ancillary bytes, the reset ResvSize / main_data_begin, and
// the final totbit / buffer.
func TestParityFlushBitstream(t *testing.T) {
	encRequireStrict(t)
	type sc struct {
		channels  int
		blockType int
	}
	cases := []sc{
		{2, 0}, // MPEG-1 stereo long
		{1, 0}, // MPEG-1 mono long
		{2, 2}, // MPEG-1 stereo short
	}
	for ci, cs := range cases {
		cf := baseCfg()
		cf.version = 1
		cf.modeGr = 2
		cf.samplerateOut = 44100
		cf.channelsOut = cs.channels
		cf.sideinfoLen = sideinfoLenFor(1, cs.channels, 0)
		cf.disableReservoir = 1
		if cs.channels == 1 {
			cf.mode = 3
		}

		c := newCgoEnc(16384)
		n := newNativeEnc(16384)
		c.setCfg(cf)
		n.setCfg(cf)
		c.setOv(9, 0, 0)
		n.setOv(9, 0, 0)
		c.setBs(0, -1, 0)
		n.setBs(0, -1, 0)
		c.disarmHeaders(1 << 30)
		n.disarmHeaders(1 << 30)
		c.setSv(0, 0, 0, 0, 0)
		n.setSv(0, 0, 0, 0, 0)
		// init_bit_stream_w seeds header[0].write_timing = 0 (h_ptr = w_ptr = 0)
		// so format_bitstream's encodeSideInfo2 schedules header[1].write_timing
		// = 0 + bitsPerFrame and the subsequent flush threads correctly.
		c.primeHeader(0, 0, 0, nil)
		n.primeHeader(0, 0, 0, nil)
		c.setSide(0, 0, 0, 0)
		n.setSide(0, 0, 0, 0)
		applySfbBands(c, n)

		const scalefacCompress = 8
		const sfbdivide = 11
		slen1 := slen1Tab[scalefacCompress]
		slen2 := slen2Tab[scalefacCompress]
		part2PerGr := 0
		for i := 0; i < 21; i++ {
			if i < sfbdivide {
				part2PerGr += slen1
			} else {
				part2PerGr += slen2
			}
		}
		for gr := 0; gr < cf.modeGr; gr++ {
			for ch := 0; ch < cs.channels; ch++ {
				sf := make([]int, 21)
				for i := range sf {
					sf[i] = (i + ci + gr + ch) % 4
				}
				primeEmptyGranule(c, n, gr, ch, cs.blockType, sf, scalefacCompress, sfbdivide, part2PerGr, 0)
			}
		}
		totalMain := part2PerGr * cf.modeGr * cs.channels
		drainPost := (8 - (totalMain % 8)) % 8
		c.setSide(0, 0, 0, drainPost)
		n.setSide(0, 0, 0, drainPost)

		// Assemble one frame so the header ring is consistent, then flush.
		require.Equal(t, c.formatBitstream(), n.formatBitstream(), "fb rc case %d", ci)
		c.flushBitstream()
		n.flushBitstream()

		require.Equal(t, c.bsTotbit(), n.bsTotbit(), "flush_bitstream totbit case %d", ci)
		require.Equal(t, c.bsBufByteIdx(), n.bsBufByteIdx(), "flush_bitstream idx case %d", ci)
		require.Equal(t, c.resvSize(), n.resvSize(), "flush_bitstream ResvSize case %d", ci)
		require.Equal(t, c.mainDataBegin(), n.mainDataBegin(), "flush_bitstream mdb case %d", ci)
		nb := c.bsBufByteIdx() + 1
		if nb > 0 {
			require.Equal(t, c.bsBuf(nb), n.bsBuf(nb), "flush_bitstream bytes case %d", ci)
		}
		c.free()
		n.free()
	}
}

// makeRandomBytes returns n deterministically-seeded random bytes (shared with
// the decoder-half tests' helper of the same name — defined there).

// ensure the nativemp3 import is used even if every test is skipped in a non-
// strict build (the StrictMode reference in encRequireStrict already uses it).
var _ = nativemp3.StrictMode
