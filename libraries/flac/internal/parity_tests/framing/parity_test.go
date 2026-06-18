//go:build cgo

package framing

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// goFrame runs f against a fresh, initialised Go BitWriter, byte-aligns
// it the same way the C harness does, and returns the framed bytes.
func goFrame(t *testing.T, f func(bw *nativeflac.BitWriter) bool) []byte {
	bw := nativeflac.NewBitWriter()
	require.True(t, bw.Init())
	require.True(t, f(bw), "Go framing returned false")
	require.True(t, bw.ZeroPadToByteBoundary())
	out, ok := bw.GetBuffer()
	require.True(t, ok)
	// Copy out of the BitWriter-owned buffer before Delete.
	cp := append([]byte(nil), out...)
	bw.ReleaseBuffer()
	bw.Delete()
	return cp
}

// ── Frame header ────────────────────────────────────────────────────

func TestParityFrameHeader(t *testing.T) {
	type tc struct {
		blocksize, sampleRate, channels, ca, bps uint32
		variable                                 bool
		number                                   uint64
	}
	cases := []tc{
		{4096, 44100, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 0},
		{4096, 44100, 2, uint32(nativeflac.ChannelAssignmentLeftSide), 16, false, 1},
		{4096, 44100, 2, uint32(nativeflac.ChannelAssignmentRightSide), 16, false, 42},
		{4096, 44100, 2, uint32(nativeflac.ChannelAssignmentMidSide), 16, true, 123456},
		{192, 88200, 1, uint32(nativeflac.ChannelAssignmentIndependent), 8, false, 7},
		{576, 176400, 3, uint32(nativeflac.ChannelAssignmentIndependent), 12, false, 7},
		{1152, 192000, 4, uint32(nativeflac.ChannelAssignmentIndependent), 20, false, 7},
		{2304, 8000, 5, uint32(nativeflac.ChannelAssignmentIndependent), 24, false, 7},
		{256, 16000, 6, uint32(nativeflac.ChannelAssignmentIndependent), 32, false, 7},
		{512, 22050, 7, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 7},
		{1024, 24000, 8, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 7},
		// blocksize hints (6/7) + sample-rate hints (12/13/14)
		{200, 96000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9},
		{4097, 32000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9},
		{4096, 96000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9},
		{4096, 12000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9},  // /1000 => hint 12
		{4096, 1234, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9},   // <=0xffff => hint 13
		{4096, 320000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, false, 9}, // /10 => hint 14
		{4096, 48000, 2, uint32(nativeflac.ChannelAssignmentIndependent), 16, true, 0xFFFFFFFFF},
	}
	for _, c := range cases {
		want := CgoFrameHeader(c.blocksize, c.sampleRate, c.channels, c.ca, c.bps, c.variable, c.number)
		require.NotEmpty(t, want, "C framing failed for %+v", c)
		got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
			h := &nativeflac.FrameHeader{
				Blocksize:         c.blocksize,
				SampleRate:        c.sampleRate,
				Channels:          c.channels,
				ChannelAssignment: nativeflac.ChannelAssignment(c.ca),
				BitsPerSample:     c.bps,
				Number:            c.number,
			}
			if c.variable {
				h.NumberType = nativeflac.FrameNumberTypeSampleNumber
			} else {
				h.NumberType = nativeflac.FrameNumberTypeFrameNumber
			}
			return nativeflac.FrameAddHeader(h, bw)
		})
		require.Equal(t, want, got, "frame header mismatch for %+v", c)
	}
}

// ── Subframe CONSTANT ───────────────────────────────────────────────

func TestParitySubframeConstant(t *testing.T) {
	for _, bps := range []uint32{4, 8, 16, 24, 32} {
		max := int64(1)<<(bps-1) - 1
		min := -(int64(1) << (bps - 1))
		for _, v := range []int64{0, 1, -1, max, min} {
			for _, wb := range []uint32{0, 1, 5} {
				want := CgoSubframeConstant(v, bps, wb)
				require.NotEmpty(t, want)
				got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
					sf := &nativeflac.SubframeConstantData{Value: v}
					return nativeflac.SubframeAddConstant(sf, bps, wb, bw)
				})
				require.Equal(t, want, got, "constant bps=%d v=%d wb=%d", bps, v, wb)
			}
		}
	}
}

// ── Subframe VERBATIM ───────────────────────────────────────────────

func TestParitySubframeVerbatim32(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, bps := range []uint32{4, 8, 16, 24, 32} {
		samples := uint32(37)
		sig := make([]int32, samples)
		for i := range sig {
			sig[i] = int32(rng.Uint32()) >> (32 - bps)
		}
		for _, wb := range []uint32{0, 2} {
			want := CgoSubframeVerbatim32(sig, samples, bps, wb)
			require.NotEmpty(t, want)
			got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
				sf := &nativeflac.SubframeVerbatimData{Type: nativeflac.VerbatimDataInt32, Data32: sig}
				return nativeflac.SubframeAddVerbatim(sf, samples, bps, wb, bw)
			})
			require.Equal(t, want, got, "verbatim32 bps=%d wb=%d", bps, wb)
		}
	}
}

func TestParitySubframeVerbatim64(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	bps := uint32(33)
	samples := uint32(29)
	sig := make([]int64, samples)
	for i := range sig {
		sig[i] = int64(rng.Uint64()) >> (64 - bps)
	}
	want := CgoSubframeVerbatim64(sig, samples, bps, 0)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		sf := &nativeflac.SubframeVerbatimData{Type: nativeflac.VerbatimDataInt64, Data64: sig}
		return nativeflac.SubframeAddVerbatim(sf, samples, bps, 0, bw)
	})
	require.Equal(t, want, got, "verbatim64")
}

// makeResidual builds residual + per-partition rice params for a
// non-escape partitioned-rice block.
func makeResidual(rng *rand.Rand, blocksize, order, partitionOrder uint32) (residual []int32, params, rawBits []uint32) {
	residualSamples := blocksize - order
	residual = make([]int32, residualSamples)
	for i := range residual {
		residual[i] = int32(rng.Uint32()&0xFF) - 128
	}
	parts := uint32(1) << partitionOrder
	params = make([]uint32, parts)
	rawBits = make([]uint32, parts)
	for i := range params {
		params[i] = 4 // a modest rice parameter; non-escape
	}
	return residual, params, rawBits
}

// ── Subframe FIXED ──────────────────────────────────────────────────

func TestParitySubframeFixed(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	blocksize := uint32(256)
	for _, order := range []uint32{0, 1, 2, 3, 4} {
		for _, po := range []uint32{0, 1, 2} {
			for _, ext := range []bool{false, true} {
				for _, bps := range []uint32{16, 33} {
					residual, params, rawBits := makeResidual(rng, blocksize, order, po)
					warmup := make([]int64, order)
					for i := range warmup {
						warmup[i] = int64(rng.Uint32()) >> (32 - bps + 1)
					}
					residualSamples := blocksize - order
					want := CgoSubframeFixed(order, residualSamples, bps, 0, warmup, po, ext, residual, params, rawBits)
					require.NotEmpty(t, want, "C fixed order=%d po=%d ext=%v bps=%d", order, po, ext, bps)
					got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
						sf := &nativeflac.SubframeFixedData{Order: order, Residual: residual}
						copy(sf.Warmup[:], warmup)
						sf.EntropyCoding.PartitionOrder = po
						sf.EntropyCoding.Contents.Parameters = params
						sf.EntropyCoding.Contents.RawBits = rawBits
						if ext {
							sf.EntropyCoding.Type = nativeflac.EntropyCodingMethodPartitionedRice2
						} else {
							sf.EntropyCoding.Type = nativeflac.EntropyCodingMethodPartitionedRice
						}
						return nativeflac.SubframeAddFixed(sf, residualSamples, bps, 0, bw)
					})
					require.Equal(t, want, got, "fixed order=%d po=%d ext=%v bps=%d", order, po, ext, bps)
				}
			}
		}
	}
}

// ── Subframe LPC ────────────────────────────────────────────────────

func TestParitySubframeLPC(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	blocksize := uint32(256)
	for _, order := range []uint32{1, 2, 8, 12, 32} {
		for _, po := range []uint32{0, 1, 2} {
			for _, prec := range []uint32{12, 15} {
				bps := uint32(16)
				residual, params, rawBits := makeResidual(rng, blocksize, order, po)
				warmup := make([]int64, order)
				for i := range warmup {
					warmup[i] = int64(rng.Uint32()) >> (32 - bps + 1)
				}
				qlp := make([]int32, order)
				for i := range qlp {
					qlp[i] = int32(rng.Uint32()&((1<<prec)-1)) - (1 << (prec - 1))
				}
				shift := 10
				residualSamples := blocksize - order
				want := CgoSubframeLPC(order, residualSamples, bps, 0, warmup, prec, shift, qlp, po, false, residual, params, rawBits)
				require.NotEmpty(t, want, "C lpc order=%d po=%d prec=%d", order, po, prec)
				got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
					sf := &nativeflac.SubframeLPCData{
						Order:             order,
						QLPCoeffPrecision: prec,
						QuantizationLevel: shift,
						Residual:          residual,
					}
					copy(sf.Warmup[:], warmup)
					copy(sf.QLPCoeff[:], qlp)
					sf.EntropyCoding.PartitionOrder = po
					sf.EntropyCoding.Type = nativeflac.EntropyCodingMethodPartitionedRice
					sf.EntropyCoding.Contents.Parameters = params
					sf.EntropyCoding.Contents.RawBits = rawBits
					return nativeflac.SubframeAddLPC(sf, residualSamples, bps, 0, bw)
				})
				require.Equal(t, want, got, "lpc order=%d po=%d prec=%d", order, po, prec)
			}
		}
	}
}

// ── Metadata: STREAMINFO ────────────────────────────────────────────

func TestParityMetadataStreamInfo(t *testing.T) {
	var md5 [16]byte
	for i := range md5 {
		md5[i] = byte(i * 7)
	}
	length := uint32(34)
	want := CgoMetadataStreamInfo(true, length, 4096, 4096, 100, 2000, 44100, 2, 16, 123456789, md5)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		m := &nativeflac.StreamMetadata{
			Type:   nativeflac.MetadataTypeStreamInfo,
			IsLast: true,
			Length: length,
			StreamInfo: nativeflac.StreamInfo{
				MinBlockSize: 4096, MaxBlockSize: 4096, MinFrameSize: 100, MaxFrameSize: 2000,
				SampleRate: 44100, Channels: 2, BitsPerSample: 16, TotalSamples: 123456789, MD5Sum: md5,
			},
		}
		return nativeflac.AddMetadataBlock(m, bw, false)
	})
	require.Equal(t, want, got)
}

// ── Metadata: PADDING ───────────────────────────────────────────────

func TestParityMetadataPadding(t *testing.T) {
	for _, length := range []uint32{0, 1, 8, 1024} {
		want := CgoMetadataPadding(false, length)
		require.NotEmpty(t, want)
		got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
			m := &nativeflac.StreamMetadata{Type: nativeflac.MetadataTypePadding, Length: length}
			return nativeflac.AddMetadataBlock(m, bw, false)
		})
		require.Equal(t, want, got, "padding length=%d", length)
	}
}

// ── Metadata: APPLICATION ───────────────────────────────────────────

func TestParityMetadataApplication(t *testing.T) {
	id := [4]byte{'t', 'e', 's', 't'}
	data := []byte("hello world application data")
	length := uint32(4 + len(data))
	want := CgoMetadataApplication(false, length, id, data)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		m := &nativeflac.StreamMetadata{
			Type:        nativeflac.MetadataTypeApplication,
			Length:      length,
			Application: nativeflac.Application{ID: id, Data: data},
		}
		return nativeflac.AddMetadataBlock(m, bw, false)
	})
	require.Equal(t, want, got)
}

// ── Metadata: SEEKTABLE ─────────────────────────────────────────────

func TestParityMetadataSeekTable(t *testing.T) {
	n := uint32(5)
	sampleNumbers := make([]uint64, n)
	streamOffsets := make([]uint64, n)
	frameSamples := make([]uint32, n)
	for i := uint32(0); i < n; i++ {
		sampleNumbers[i] = uint64(i) * 4096
		streamOffsets[i] = uint64(i) * 1000
		frameSamples[i] = 4096
	}
	length := n * 18
	want := CgoMetadataSeekTable(false, length, n, sampleNumbers, streamOffsets, frameSamples)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		pts := make([]nativeflac.SeekPoint, n)
		for i := uint32(0); i < n; i++ {
			pts[i] = nativeflac.SeekPoint{SampleNumber: sampleNumbers[i], StreamOffset: streamOffsets[i], FrameSamples: frameSamples[i]}
		}
		m := &nativeflac.StreamMetadata{
			Type:      nativeflac.MetadataTypeSeekTable,
			Length:    length,
			SeekTable: nativeflac.SeekTable{Points: pts},
		}
		return nativeflac.AddMetadataBlock(m, bw, false)
	})
	require.Equal(t, want, got)
}

// ── Metadata: VORBIS_COMMENT ────────────────────────────────────────

func TestParityMetadataVorbisComment(t *testing.T) {
	vendor := []byte("my custom vendor")
	comments := [][]byte{
		[]byte("TITLE=Song"),
		[]byte("ARTIST=Band"),
		[]byte("ALBUM=Record"),
	}
	// Flatten comments for the C ABI.
	var flat []byte
	offsets := make([]uint32, len(comments))
	lengths := make([]uint32, len(comments))
	for i, c := range comments {
		offsets[i] = uint32(len(flat))
		lengths[i] = uint32(len(c))
		flat = append(flat, c...)
	}
	numComments := uint32(len(comments))

	for _, updateVendor := range []bool{false, true} {
		// length must reflect the *stored* vendor (caller's), per libFLAC;
		// AddMetadataBlock adjusts internally when updateVendor.
		bodyLen := uint32(4 + len(vendor) + 4)
		for _, c := range comments {
			bodyLen += uint32(4 + len(c))
		}
		want := CgoMetadataVorbisComment(false, bodyLen, updateVendor, vendor, numComments, flat, offsets, lengths)
		require.NotEmpty(t, want, "C vorbis updateVendor=%v", updateVendor)
		got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
			cs := make([]nativeflac.VorbisCommentEntry, numComments)
			for i, c := range comments {
				cs[i] = nativeflac.VorbisCommentEntry{Length: uint32(len(c)), Entry: c}
			}
			m := &nativeflac.StreamMetadata{
				Type:   nativeflac.MetadataTypeVorbisComment,
				Length: bodyLen,
				VorbisComment: nativeflac.VorbisComment{
					VendorString: nativeflac.VorbisCommentEntry{Length: uint32(len(vendor)), Entry: vendor},
					NumComments:  numComments,
					Comments:     cs,
				},
			}
			return nativeflac.AddMetadataBlock(m, bw, updateVendor)
		})
		require.Equal(t, want, got, "vorbis updateVendor=%v", updateVendor)
	}
}

// ── Metadata: PICTURE ───────────────────────────────────────────────

func TestParityMetadataPicture(t *testing.T) {
	mime := []byte("image/png")
	desc := []byte("front cover")
	data := []byte("\x89PNGfakeimagebytes")
	dataLength := uint32(len(data))
	length := uint32(4 + 4 + len(mime) + 4 + len(desc) + 4*5 + len(data))
	want := CgoMetadataPicture(false, length, 3, mime, desc, 100, 200, 24, 0, dataLength, data)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		m := &nativeflac.StreamMetadata{
			Type:   nativeflac.MetadataTypePicture,
			Length: length,
			Picture: nativeflac.Picture{
				Type: 3, MimeType: mime, Description: desc,
				Width: 100, Height: 200, Depth: 24, Colors: 0,
				DataLength: dataLength, Data: data,
			},
		}
		return nativeflac.AddMetadataBlock(m, bw, false)
	})
	require.Equal(t, want, got)
}

// ── Metadata: CUESHEET ──────────────────────────────────────────────

func TestParityMetadataCueSheet(t *testing.T) {
	var mcn [129]byte
	copy(mcn[:], "1234567890123")
	leadIn := uint64(88200)
	numTracks := uint32(2)
	tOffset := []uint64{0, 44100 * 60}
	tNumber := []byte{1, 2}
	// ISRC: 13 bytes per track (12 + trailing NUL).
	tISRC := make([]byte, numTracks*13)
	copy(tISRC[0:], "USABC1234567")
	copy(tISRC[13:], "USXYZ7654321")
	tType := []uint32{0, 1}
	tPreEmph := []uint32{0, 1}
	tNumIndices := []byte{2, 1}
	tIndexBase := []uint32{0, 2}
	idxOffset := []uint64{0, 588, 0}
	idxNumber := []byte{0, 1, 1}

	// body length: 396 header + per-track (per RFC). We let libFLAC tell
	// us the right length by computing it the same way the encoder does.
	// MEDIA_CATALOG(128)+LEAD_IN(8)+IS_CD/reserved(1+258+ rounding)... use
	// the documented fixed sizes: header = 128 + 8 + 1 + 258 + 1 = 396.
	// CUESHEET header is 396 bytes: MEDIA_CATALOG(1024b)+LEAD_IN(64b)+
	// IS_CD(1b)+RESERVED(2071b)+NUM_TRACKS(8b) = 3168b = 396 bytes
	// (per FLAC__add_metadata_block, stream_encoder_framing.c:154-165).
	bodyLen := uint32(396)
	for i := uint32(0); i < numTracks; i++ {
		// per track (stream_encoder_framing.c:166-183):
		//   TRACK_OFFSET 64b + TRACK_NUMBER 8b + TRACK_ISRC 96b +
		//   TRACK_TYPE 1b + TRACK_PRE_EMPHASIS 1b + TRACK_RESERVED (6+13*8=110b) +
		//   TRACK_NUM_INDICES 8b = 288b = 36 bytes.
		bodyLen += 36
		for j := byte(0); j < tNumIndices[i]; j++ {
			// per index (stream_encoder_framing.c:184-192):
			//   INDEX_OFFSET 64b + INDEX_NUMBER 8b + INDEX_RESERVED 24b = 96b = 12 bytes.
			bodyLen += 12
		}
	}

	want := CgoMetadataCueSheet(false, bodyLen, mcn, leadIn, true, numTracks,
		tOffset, tNumber, tISRC, tType, tPreEmph, tNumIndices, tIndexBase, idxOffset, idxNumber)
	require.NotEmpty(t, want)
	got := goFrame(t, func(bw *nativeflac.BitWriter) bool {
		var mcn128 [128]byte
		copy(mcn128[:], mcn[:128])
		tracks := make([]nativeflac.CueSheetTrack, numTracks)
		for i := uint32(0); i < numTracks; i++ {
			var isrc [12]byte
			copy(isrc[:], tISRC[i*13:i*13+12])
			ni := tNumIndices[i]
			indices := make([]nativeflac.CueSheetIndex, ni)
			base := tIndexBase[i]
			for j := byte(0); j < ni; j++ {
				indices[j] = nativeflac.CueSheetIndex{Offset: idxOffset[base+uint32(j)], Number: idxNumber[base+uint32(j)]}
			}
			tracks[i] = nativeflac.CueSheetTrack{
				Offset: tOffset[i], Number: tNumber[i], ISRC: isrc,
				Type: tType[i], PreEmphasis: tPreEmph[i], NumIndices: ni, Indices: indices,
			}
		}
		m := &nativeflac.StreamMetadata{
			Type:   nativeflac.MetadataTypeCuesheet,
			Length: bodyLen,
			CueSheet: nativeflac.CueSheet{
				MediaCatalogNumber: mcn128, LeadIn: leadIn, IsCD: true,
				NumTracks: numTracks, Tracks: tracks,
			},
		}
		return nativeflac.AddMetadataBlock(m, bw, false)
	})
	require.Equal(t, want, got)
}
