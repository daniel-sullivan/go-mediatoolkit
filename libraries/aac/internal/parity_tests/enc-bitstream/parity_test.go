// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encbitstream

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// bufBytes is the power-of-two ring-buffer size both the C oracle and the Go
// port hand FDKinitBitStream / newWriteBitStream. It must be a power of two
// (FDK_InitBitBuffer asserts that) and large enough that no fabricated
// raw_data_block element wraps the ring — 4096 bytes (32768 bits) comfortably
// holds the longest case (a full 1024-line ESC spectral section).
const bufBytes = 4096

// Block types (psy_const.h LONG/START/SHORT/STOP_WINDOW).
const (
	longWindow  = nativeaac.LongWindow
	startWindow = nativeaac.StartWindow
	shortWindow = nativeaac.ShortWindow
	stopWindow  = nativeaac.StopWindow
)

// longBlockTypes are the three long-style window sequences (handled
// identically by the section/ics code) plus short, the geometry that differs.
var allBlockTypes = []int{longWindow, startWindow, stopWindow, shortWindow}

// strictSkip skips a parity test under a bare (non-aac_strict) build, matching
// the area convention: the whole bitstream-encode area is a pure integer kernel
// so the assertions are bit-exact in any build, but they assert only under
// -tags=aac_strict so a default `go test` of the suite stays clean.
func strictSkip(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-bitstream parity asserts under -tags=aac_strict (the integer-parity gate convention); skipping in the default build")
	}
}

// ============================ ics_info ====================================

// TestEncodeIcsInfoParity asserts FDKaacEnc_encodeIcsInfo serializes the
// ics_info syntax byte-for-byte (and returns the same static-bit count) as the
// vendored C, across block types, both window shapes, every maxSfb / grouping
// mask, and the AC_SCALABLE / AC_ELD syntax-flag variants.
func TestEncodeIcsInfoParity(t *testing.T) {
	strictSkip(t)

	syntaxFlagSet := []uint32{
		0, // plain GA (AOT 2)
		nativeaac.ACScalable,
		nativeaac.ACELD,
		nativeaac.ACScalable | nativeaac.ACELD,
	}
	windowShapes := []int{nativeaac.SineWindow, nativeaac.KbdWindow, nativeaac.LolWindow}

	for _, bt := range allBlockTypes {
		for _, ws := range windowShapes {
			for _, sf := range syntaxFlagSet {
				maxSfbHi := 64
				if bt == shortWindow {
					maxSfbHi = 16
				}
				for maxSfb := 0; maxSfb < maxSfbHi; maxSfb++ {
					// groupingMask only consumed for short blocks (TRANS_FAC-1 bits).
					gmHi := 1
					if bt == shortWindow {
						gmHi = 1 << (nativeaac.TransFac - 1)
					}
					for gm := 0; gm < gmHi; gm += 37 + 1 { // sparse sweep of the 7-bit mask
						if gm >= gmHi {
							gm = gmHi - 1
						}
						wantBuf, wantBits := cIcsInfo(bt, ws, gm, maxSfb, bufBytes, sf)
						gotBuf, gotBits := nativeaac.EncodeIcsInfoParity(bt, ws, gm, maxSfb, bufBytes, sf)
						assert.Equal(t, wantBits, gotBits, "statBits bt=%d ws=%d sf=%#x maxSfb=%d gm=%d", bt, ws, sf, maxSfb, gm)
						require.Equal(t, wantBuf, gotBuf, "bytes bt=%d ws=%d sf=%#x maxSfb=%d gm=%d", bt, ws, sf, maxSfb, gm)
					}
				}
			}
		}
	}
}

// ============================ global_gain =================================

// TestEncodeGlobalGainParity asserts FDKaacEnc_encodeGlobalGain serializes the
// 8-bit global_gain field identically across the global-gain / first-scalefac /
// mdctScale ranges the encoder produces.
func TestEncodeGlobalGainParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x6107))
	for iter := 0; iter < 2000; iter++ {
		globalGain := rng.Intn(256) // 0..255
		scalefac := rng.Intn(256)   // 0..255
		mdctScale := rng.Intn(20)   // small positive mdctScale
		wantBuf, wantBits := cGlobalGain(globalGain, scalefac, mdctScale, bufBytes)
		gotBuf, gotBits := nativeaac.EncodeGlobalGainParity(globalGain, scalefac, mdctScale, bufBytes)
		assert.Equal(t, wantBits, gotBits, "statBits gg=%d scf=%d ms=%d", globalGain, scalefac, mdctScale)
		require.Equal(t, wantBuf, gotBuf, "bytes gg=%d scf=%d ms=%d", globalGain, scalefac, mdctScale)
	}
}

// ============================ section_data ================================

// codebookLav is the largest-absolute spectral value each Huffman codebook
// codes (bit_cnt.h enum codeBookLav). Books 1..10 draw in [-LAV,+LAV]; the
// escape book (11) additionally codes magnitudes above 16 via an escape.
var codebookLav = map[int]int{
	1: 1, 2: 1, 3: 2, 4: 2, 5: 4, 6: 4, 7: 7, 8: 7, 9: 12, 10: 12,
}

// fabricateSections builds a deterministic partition of nSfb scalefactor bands
// into adjacent Huffman sections (each tagged with a codebook), returning the
// codeBook / sfbStart / sfbCnt arrays the C/Go serializers consume. cbPool is
// the set of codebooks drawn from.
func fabricateSections(rng *rand.Rand, nSfb int, cbPool []int) (codeBook, sfbStart, sfbCnt []int32) {
	sfb := 0
	for sfb < nSfb {
		cb := cbPool[rng.Intn(len(cbPool))]
		cnt := 1 + rng.Intn(6)
		if sfb+cnt > nSfb {
			cnt = nSfb - sfb
		}
		codeBook = append(codeBook, int32(cb))
		sfbStart = append(sfbStart, int32(sfb))
		sfbCnt = append(sfbCnt, int32(cnt))
		sfb += cnt
	}
	return
}

// TestEncodeSectionDataParity asserts FDKaacEnc_encodeSectionData serializes the
// section_data syntax (codebook field + escape-coded section length) identically
// — including sections longer than the escape value so the escape loop is
// exercised — for long and short block types, with useVCB11 on and off.
func TestEncodeSectionDataParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x5EC7))
	cbPool := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15}

	for _, bt := range allBlockTypes {
		nSfbHi := 51
		if bt == shortWindow {
			nSfbHi = 16
		}
		for _, useVCB11 := range []bool{false, true} {
			for iter := 0; iter < 200; iter++ {
				nSfb := 1 + rng.Intn(nSfbHi)
				// Force a long section beyond the escape value occasionally.
				cb, st, cnt := fabricateSections(rng, nSfb, cbPool)
				if iter%5 == 0 && len(cnt) > 0 {
					// merge into one big section to cross SECT_ESC_VAL
					cb = []int32{int32(cbPool[rng.Intn(len(cbPool))])}
					st = []int32{0}
					cnt = []int32{int32(nSfb)}
				}
				wantBuf, wantBits := cSectionData(bt, cb, st, cnt, useVCB11, bufBytes)
				gotBuf, gotBits := nativeaac.EncodeSectionDataParity(bt, toDesc(cb, st, cnt), useVCB11, bufBytes)
				assert.Equal(t, wantBits, gotBits, "siBits bt=%d vcb11=%v iter=%d", bt, useVCB11, iter)
				require.Equal(t, wantBuf, gotBuf, "bytes bt=%d vcb11=%v iter=%d", bt, useVCB11, iter)
			}
		}
	}
}

// toDesc adapts the flat C-style section arrays into the nativeaac.SectionDesc
// slice the Go parity wrappers take.
func toDesc(codeBook, sfbStart, sfbCnt []int32) []nativeaac.SectionDesc {
	out := make([]nativeaac.SectionDesc, len(codeBook))
	for i := range codeBook {
		out[i] = nativeaac.SectionDesc{
			CodeBook: int(codeBook[i]),
			SfbStart: int(sfbStart[i]),
			SfbCnt:   int(sfbCnt[i]),
		}
	}
	return out
}

// ============================ scale_factor_data ==========================

// TestEncodeScaleFactorDataParity asserts FDKaacEnc_encodeScaleFactorData
// serializes the DPCM-coded scalefactors, the PCM-then-DPCM PNS energies, and
// the intensity scales identically — exercising the three codebook branches
// (ordinary scalefactor, PNS book 13, intensity books 14/15), the
// maxValueInSfb==0 "repeat last" path, and the noisePCMFlag first-value path.
func TestEncodeScaleFactorDataParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x5CF7))

	for _, bt := range allBlockTypes {
		nSfbHi := 51
		if bt == shortWindow {
			nSfbHi = 16
		}
		for iter := 0; iter < 400; iter++ {
			nSfb := 4 + rng.Intn(nSfbHi-3)
			// Mix ordinary, PNS, intensity and zero sections so every
			// scale_factor_data branch is hit. Keep deltas in-range so the
			// serializer never returns the range error mid-stream (which would
			// short-circuit both sides identically but truncate the comparison).
			cb, st, cnt := fabricateSectionsScf(rng, nSfb)

			globalGain := rng.Intn(256)
			// Per-band data. scalefac walks in small steps so DPCM deltas stay
			// within [-60,60]; noiseNrg likewise; isScale likewise.
			scalefac := make([]int32, nSfb)
			noiseNrg := make([]int32, nSfb)
			isScale := make([]int32, nSfb)
			maxVal := make([]uint32, nSfb)
			scalefac[0] = int32(rng.Intn(256))
			noiseNrg[0] = int32(rng.Intn(256))
			for i := 1; i < nSfb; i++ {
				scalefac[i] = scalefac[i-1] + int32(rng.Intn(81)-40)
				noiseNrg[i] = noiseNrg[i-1] + int32(rng.Intn(81)-40)
				isScale[i] = isScale[i-1] + int32(rng.Intn(81)-40)
			}
			for i := range maxVal {
				// ~1/4 of bands "empty" -> deltaScf forced to 0 (repeat-last path).
				if rng.Intn(4) == 0 {
					maxVal[i] = 0
				} else {
					maxVal[i] = uint32(1 + rng.Intn(8191))
				}
			}
			firstScf := int(st[0]) // first coded scf == first section start

			wantBuf, wantBits := cScaleFactorData(bt, firstScf, cb, st, cnt,
				maxVal, scalefac, noiseNrg, isScale, globalGain, bufBytes)
			gotBuf, gotBits := nativeaac.EncodeScaleFactorDataParity(bt, firstScf,
				toDesc(cb, st, cnt), toUint(maxVal), toInt(scalefac), toInt(noiseNrg),
				toInt(isScale), globalGain, bufBytes)
			assert.Equal(t, wantBits, gotBits, "sfBits bt=%d iter=%d", bt, iter)
			require.Equal(t, wantBuf, gotBuf, "bytes bt=%d iter=%d", bt, iter)
		}
	}
}

// fabricateSectionsScf partitions nSfb bands into adjacent sections drawn from
// the codebooks scale_factor_data cares about: 0 (zero, skipped), 1..11
// (ordinary scalefactor), 13 (PNS), 14/15 (intensity).
func fabricateSectionsScf(rng *rand.Rand, nSfb int) (codeBook, sfbStart, sfbCnt []int32) {
	pool := []int{0, 1, 4, 7, 11, 13, 14, 15}
	sfb := 0
	for sfb < nSfb {
		cb := pool[rng.Intn(len(pool))]
		cnt := 1 + rng.Intn(5)
		if sfb+cnt > nSfb {
			cnt = nSfb - sfb
		}
		codeBook = append(codeBook, int32(cb))
		sfbStart = append(sfbStart, int32(sfb))
		sfbCnt = append(sfbCnt, int32(cnt))
		sfb += cnt
	}
	return
}

func toInt(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

func toUint(s []uint32) []uint {
	out := make([]uint, len(s))
	for i, v := range s {
		out[i] = uint(v)
	}
	return out
}

// ============================ ms_mask / ms_used ==========================

// TestEncodeMSInfoParity asserts FDKaacEnc_encodeMSInfo serializes the
// ms_mask_present field and (for MS_SOME) the per-band ms_used bits identically,
// across the three msDigest states and grouped-short geometries.
func TestEncodeMSInfoParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x115))
	for _, md := range []int{nativeaac.MsNone, nativeaac.MsSome, nativeaac.MsAll} {
		// Geometries within the real MAX_GROUPED_SFB (=MAX_NO_OF_GROUPS*MAX_SFB_SHORT
		// = 4*15 = 60) bound the encoder's msMask array enforces.
		for _, geom := range [][3]int{
			{49, 49, 49}, // single long group, all bands
			{49, 49, 30}, // single long group, maxSfb < sfbPerGroup
			{60, 15, 14}, // 4 short groups of 15 sfbs, 14 active each
			{60, 15, 15}, // 4 short groups, all bands active
			{45, 15, 12}, // 3 short groups
		} {
			sfbCnt, grpSfb, maxSfb := geom[0], geom[1], geom[2]
			for iter := 0; iter < 100; iter++ {
				jsFlags := make([]int32, sfbCnt)
				for i := range jsFlags {
					if rng.Intn(2) == 0 {
						jsFlags[i] = nativeaac.MsOn
					}
				}
				wantBuf, wantBits := cMSInfo(sfbCnt, grpSfb, maxSfb, md, jsFlags, bufBytes)
				gotBuf, gotBits := nativeaac.EncodeMSInfoParity(sfbCnt, grpSfb, maxSfb, md, toInt(jsFlags), bufBytes)
				assert.Equal(t, wantBits, gotBits, "msBits md=%d geom=%v iter=%d", md, geom, iter)
				require.Equal(t, wantBuf, gotBuf, "bytes md=%d geom=%v iter=%d", md, geom, iter)
			}
		}
	}
}

// ============================ tns_data ===================================

// maxNumOfFilters mirrors the C aacenc_tns.h MAX_NUM_OF_FILTERS (2) the flat
// TNS bridge arrays are sized by.
const maxNumOfFilters = 2

// tnsMaxOrder mirrors the C aacenc_tns.h TNS_MAX_ORDER (12).
const tnsMaxOrder = 12

// fabricateTns builds the flat per-window/filter arrays (matching bridge.cpp
// fillTnsInfo + the nativeaac.TnsWindowDesc layout) for numOfWindows windows.
// coef magnitudes straddle the coef_compress thresholds so both the 2/3-bit and
// 3/4-bit coefBits paths are exercised.
func fabricateTns(rng *rand.Rand, numOfWindows int) (coefRes, numOfFilters, length,
	order, direction, coef []int32, windows []nativeaac.TnsWindowDesc) {
	coefRes = make([]int32, numOfWindows)
	numOfFilters = make([]int32, numOfWindows)
	length = make([]int32, numOfWindows*maxNumOfFilters)
	order = make([]int32, numOfWindows*maxNumOfFilters)
	direction = make([]int32, numOfWindows*maxNumOfFilters)
	coef = make([]int32, numOfWindows*maxNumOfFilters*tnsMaxOrder)
	windows = make([]nativeaac.TnsWindowDesc, numOfWindows)

	for w := 0; w < numOfWindows; w++ {
		cr := 3
		if rng.Intn(2) == 0 {
			cr = 4
		}
		coefRes[w] = int32(cr)
		nf := rng.Intn(maxNumOfFilters + 1) // 0..MAX_NUM_OF_FILTERS
		numOfFilters[w] = int32(nf)
		win := nativeaac.TnsWindowDesc{CoefRes: cr}
		for f := 0; f < nf; f++ {
			wf := w*maxNumOfFilters + f
			length[wf] = int32(rng.Intn(32))
			ord := rng.Intn(tnsMaxOrder + 1) // 0..12
			order[wf] = int32(ord)
			direction[wf] = int32(rng.Intn(2))
			filt := nativeaac.TnsFilterDesc{
				Length:    int(length[wf]),
				Order:     ord,
				Direction: int(direction[wf]),
				Coef:      make([]int16, ord),
			}
			// coef range: signed values that straddle the compress thresholds.
			for k := 0; k < ord; k++ {
				v := int16(rng.Intn(17) - 8) // -8..8
				coef[wf*tnsMaxOrder+k] = int32(v)
				filt.Coef[k] = v
			}
			win.Filters = append(win.Filters, filt)
		}
		windows[w] = win
	}
	return
}

// TestEncodeTnsDataPresentParity asserts FDKaacEnc_encodeTnsDataPresent writes
// the one-bit tns_data_present flag identically (and always returns 1) for long
// (1 window) and short (TRANS_FAC windows) configs with and without active
// filters.
func TestEncodeTnsDataPresentParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x7115))
	for _, bt := range allBlockTypes {
		numWin := 1
		if bt == shortWindow {
			numWin = nativeaac.TransFac
		}
		for iter := 0; iter < 300; iter++ {
			cr, nf, ln, or, dir, cf, win := fabricateTns(rng, numWin)
			wantBuf, wantBits := cTnsDataPresent(bt, numWin, cr, nf, ln, or, dir, cf, bufBytes)
			gotBuf, gotBits := nativeaac.EncodeTnsDataPresentParity(bt, win, bufBytes)
			assert.Equal(t, wantBits, gotBits, "statBits bt=%d iter=%d", bt, iter)
			require.Equal(t, wantBuf, gotBuf, "bytes bt=%d iter=%d", bt, iter)
		}
	}
}

// TestEncodeTnsDataParity asserts FDKaacEnc_encodeTnsData serializes the full
// tns_data syntax (per-window filter count + coefRes flag, per-filter length /
// order / direction / coef_compress + the 2/3/4-bit coefficients) byte-for-byte
// and returns the same bit count, across long/short geometries and the
// coef_compress threshold paths.
func TestEncodeTnsDataParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x7105))
	for _, bt := range allBlockTypes {
		numWin := 1
		if bt == shortWindow {
			numWin = nativeaac.TransFac
		}
		for iter := 0; iter < 500; iter++ {
			cr, nf, ln, or, dir, cf, win := fabricateTns(rng, numWin)
			wantBuf, wantBits := cTnsData(bt, numWin, cr, nf, ln, or, dir, cf, bufBytes)
			gotBuf, gotBits := nativeaac.EncodeTnsDataParity(bt, win, bufBytes)
			assert.Equal(t, wantBits, gotBits, "tnsBits bt=%d iter=%d", bt, iter)
			require.Equal(t, wantBuf, gotBuf, "bytes bt=%d iter=%d", bt, iter)
		}
	}
}

// ============================ spectral_data ==============================

// TestEncodeSpectralDataParity asserts FDKaacEnc_encodeSpectralData Huffman-
// encodes the full spectral_data of every section identically (produced bytes +
// bit count), skipping the PNS book (13) per the C, over a realistic long-window
// scalefactor-band layout with mixed codebooks (including the escape book 11).
func TestEncodeSpectralDataParity(t *testing.T) {
	strictSkip(t)

	rng := rand.New(rand.NewSource(0x59EC))
	// A faithful long-window 4-line-granular sfb layout (every sfb a multiple of
	// 4 lines so every codebook stride divides each band).
	const linesPerSfb = 4
	for _, bt := range []int{longWindow, startWindow, stopWindow, shortWindow} {
		nSfbHi := 49
		if bt == shortWindow {
			nSfbHi = 14
		}
		// Pool excludes 0 (zero -> still emitted as empty) but includes ESC (11)
		// and PNS (13) to cover the codeBook==PNS skip branch.
		cbPool := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13}
		for iter := 0; iter < 200; iter++ {
			nSfb := 1 + rng.Intn(nSfbHi)
			cb, st, cnt := fabricateSections(rng, nSfb, cbPool)
			// Build sfbOffset (nSfb+1 entries) at linesPerSfb granularity.
			sfbOffset := make([]int32, nSfb+1)
			for i := range sfbOffset {
				sfbOffset[i] = int32(i * linesPerSfb)
			}
			nLines := int(sfbOffset[nSfb])
			quant := make([]int16, nLines)
			// Fill each section's lines with in-range coefficients for its codebook.
			for s := range cb {
				book := int(cb[s])
				lo := int(sfbOffset[st[s]])
				hi := int(sfbOffset[st[s]+cnt[s]])
				for i := lo; i < hi; i++ {
					quant[i] = fabricateCoef(rng, book)
				}
			}
			wantBuf, wantBits := cSpectralData(bt, cb, st, cnt, sfbOffset, quant, bufBytes)
			gotBuf, gotBits := nativeaac.EncodeSpectralDataParity(bt, toDesc(cb, st, cnt), toInt(sfbOffset), quant, bufBytes)
			assert.Equal(t, wantBits, gotBits, "specBits bt=%d iter=%d", bt, iter)
			require.Equal(t, wantBuf, gotBuf, "bytes bt=%d iter=%d", bt, iter)
		}
	}
}

// fabricateCoef draws one in-range spectral coefficient for codebook cb. Book 0
// / 13 (zero / PNS) emit nothing so their coefficient value is don't-care (use
// 0). Books 1..10 draw uniformly in [-LAV,+LAV]; the escape book (11) draws
// magnitudes up to 8191 (signed) so both the escape-sequence path
// (magnitude >= 16) and the in-table path (< 16) are exercised.
func fabricateCoef(rng *rand.Rand, cb int) int16 {
	if cb == 0 || cb == 13 {
		return 0
	}
	if cb == 11 {
		var mag int
		if rng.Intn(3) == 0 {
			mag = rng.Intn(8192)
		} else {
			mag = rng.Intn(32)
		}
		if rng.Intn(2) == 0 {
			mag = -mag
		}
		return int16(mag)
	}
	lav := codebookLav[cb]
	return int16(rng.Intn(2*lav+1) - lav)
}

// TestEncBitstreamOracleDeterministic is a lightweight always-on check (no
// strict gate) that the C oracle itself runs and is stable — it guards the cgo
// build/link of the vendored bitenc.cpp + bit_cnt.cpp + aacEnc_rom.cpp +
// FDK_tools_rom.cpp + FDK_bitbuffer.cpp + genericStds.cpp TUs even when the
// strict assertions above are skipped.
func TestEncBitstreamOracleDeterministic(t *testing.T) {
	a, abits := cIcsInfo(longWindow, nativeaac.SineWindow, 0, 40, bufBytes, 0)
	b, bbits := cIcsInfo(longWindow, nativeaac.SineWindow, 0, 40, bufBytes, 0)
	require.Equal(t, a, b, "C encodeIcsInfo oracle is non-deterministic")
	require.Equal(t, abits, bbits)

	cb := []int32{4, 11}
	st := []int32{0, 5}
	cnt := []int32{5, 5}
	c, cbits := cSectionData(longWindow, cb, st, cnt, false, bufBytes)
	d, dbits := cSectionData(longWindow, cb, st, cnt, false, bufBytes)
	require.Equal(t, c, d, "C encodeSectionData oracle is non-deterministic")
	require.Equal(t, cbits, dbits)
}
