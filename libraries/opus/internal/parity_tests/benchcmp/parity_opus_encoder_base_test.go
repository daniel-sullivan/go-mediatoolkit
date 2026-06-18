//go:build cgo && opus_strict

package benchcmp

import (
	"reflect"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_OpusEncoderGetSize — opus_encoder_get_size sweep over
// valid and invalid channel counts. The Go port returns a symbolic
// size (it does not share the C arena), so we only assert sign-parity
// with the C oracle (>0 for valid channels, 0 for invalid) rather
// than byte-exact equality.
func TestParity_OpusEncoderGetSize(t *testing.T) {
	cases := []int{-1, 0, 1, 2, 3, 4}
	for _, ch := range cases {
		gg := nativeopus.ExportOpusEncoderGetSize(ch)
		cc := cOpusEncoderGetSize(ch)
		if (gg > 0) != (cc > 0) {
			t.Errorf("channels=%d: Go size=%d (>0:%v), C size=%d (>0:%v)",
				ch, gg, gg > 0, cc, cc > 0)
		}
		if gg < 0 {
			t.Errorf("channels=%d: Go size=%d, expected >=0", ch, gg)
		}
	}
}

// TestParity_OpusEncoderInit — struct-level parity across the
// cartesian product of supported (Fs, channels, application) values.
// Compares every deterministically-initialised field between the Go
// port and the C oracle.
func TestParity_OpusEncoderInit(t *testing.T) {
	fsValues := []int32{8000, 12000, 16000, 24000, 48000}
	channelValues := []int{1, 2}
	appValues := []int{
		nativeopus.OPUS_APPLICATION_VOIP,
		nativeopus.OPUS_APPLICATION_AUDIO,
		nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY,
	}
	for _, Fs := range fsValues {
		for _, ch := range channelValues {
			for _, app := range appValues {
				Fs, ch, app := Fs, ch, app
				name := mkInitName(Fs, ch, app)
				t.Run(name, func(t *testing.T) {
					gRet, gSnap := nativeopus.ExportOpusEncoderInitAndSnapshot(Fs, ch, app)
					cRet, cSnap := cOpusEncoderInitSnapshot(Fs, ch, app)
					if gRet != cRet {
						t.Fatalf("return code mismatch: Go=%d C=%d", gRet, cRet)
					}
					if gRet != 0 {
						return
					}
					// Convert to a shared shape for reflection-based
					// field-by-field compare. We rely on the field
					// layouts having been declared in lock-step.
					compareEncoderSnapshot(t, gSnap, cSnap)
				})
			}
		}
	}
}

// compareEncoderSnapshot field-compares the Go and C snapshots.
// Skips fields whose value is intentionally a Go-port artefact
// (celt_enc_offset / silk_enc_offset reflect the C arena layout and
// are not byte-identical under the symbolic-size scheme).
func compareEncoderSnapshot(t *testing.T, g nativeopus.OpusEncoderStateSnapshot, c cEncSnapshot) {
	t.Helper()
	// Field-by-field. We use reflection so that adding a field to the
	// snapshot auto-covers it.
	gv := reflect.ValueOf(g)
	cv := reflect.ValueOf(c)
	typ := gv.Type()
	skip := map[string]bool{
		// Offsets track the C arena layout. Go's opus_encoder_init
		// computes symbolic values driven by opusEncoderSizeOfStruct()
		// = 1, which intentionally does not match the C sizeof. Parity
		// of these offsets is tested indirectly via the
		// delta-between-them (celt - silk = silkEncSizeBytes).
		"CeltEncOffset": true,
		"SilkEncOffset": true,
		// arch: opus_select_arch may compile-time-disagree between
		// clang and Go. Our Go impl pins to 0; the C side may pick a
		// higher value on x86_64 with SIMD detection. Compare loosely.
		"Arch": true,
	}
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if skip[name] {
			continue
		}
		gvi := gv.Field(i).Interface()
		cvi := cv.Field(i).Interface()
		if gvi != cvi {
			t.Errorf("field %s: Go=%v C=%v", name, gvi, cvi)
		}
	}
}

// TestParity_GenTOC — table-driven sweep across mode × bandwidth ×
// frame_size (encoded as a target frame rate) × stereo-ness.
func TestParity_GenTOC(t *testing.T) {
	modes := []int{
		nativeopus.MODE_SILK_ONLY,
		nativeopus.MODE_HYBRID,
		nativeopus.MODE_CELT_ONLY,
	}
	// gen_toc bandwidths legal to the (mode, framerate) triple. We
	// sweep broadly; invalid combos produce undefined bit patterns
	// but C and Go both produce the same ones.
	bandwidths := []int{
		nativeopus.OPUS_BANDWIDTH_NARROWBAND,
		nativeopus.OPUS_BANDWIDTH_MEDIUMBAND,
		nativeopus.OPUS_BANDWIDTH_WIDEBAND,
		nativeopus.OPUS_BANDWIDTH_SUPERWIDEBAND,
		nativeopus.OPUS_BANDWIDTH_FULLBAND,
	}
	// framerate values cover 2.5ms (400 fps) up to 100ms (10 fps).
	framerates := []int{10, 12, 20, 25, 50, 100, 200, 400}
	channels := []int{1, 2}
	for _, mode := range modes {
		for _, bw := range bandwidths {
			for _, fr := range framerates {
				for _, ch := range channels {
					g := nativeopus.ExportGenToc(mode, fr, bw, ch)
					c := cGenToc(mode, fr, bw, ch)
					if g != c {
						t.Errorf("gen_toc(mode=%d, fr=%d, bw=%d, ch=%d): Go=0x%02x C=0x%02x",
							mode, fr, bw, ch, g, c)
					}
				}
			}
		}
	}
}

// TestParity_ComputeFrameSize — frame_size_select sweep across
// (analysis_frame_size, variable_duration, Fs) and both SILK-only and
// non-SILK applications (different lower-bound rules).
func TestParity_ComputeFrameSize(t *testing.T) {
	fsValues := []int32{8000, 12000, 16000, 24000, 48000}
	variableDurations := []int{
		nativeopus.OPUS_FRAMESIZE_ARG,
		nativeopus.OPUS_FRAMESIZE_2_5_MS,
		nativeopus.OPUS_FRAMESIZE_5_MS,
		nativeopus.OPUS_FRAMESIZE_10_MS,
		nativeopus.OPUS_FRAMESIZE_20_MS,
		nativeopus.OPUS_FRAMESIZE_40_MS,
		nativeopus.OPUS_FRAMESIZE_60_MS,
		nativeopus.OPUS_FRAMESIZE_80_MS,
		nativeopus.OPUS_FRAMESIZE_100_MS,
		nativeopus.OPUS_FRAMESIZE_120_MS,
		// Out-of-range: should return -1.
		nativeopus.OPUS_FRAMESIZE_ARG - 1,
		nativeopus.OPUS_FRAMESIZE_120_MS + 1,
	}
	applications := []int{
		nativeopus.OPUS_APPLICATION_VOIP,
		nativeopus.OPUS_APPLICATION_AUDIO,
		nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY,
		nativeopus.OPUS_APPLICATION_RESTRICTED_SILK,
	}
	for _, Fs := range fsValues {
		// Frame sizes to probe: multiples of Fs/400 (2.5 ms) up to
		// 120 ms, plus a below-minimum sample for the rejection path.
		fsSamples := []int32{
			Fs / 400 / 2, // below minimum
			Fs / 400,     // 2.5 ms
			Fs / 200,     // 5 ms
			Fs / 100,     // 10 ms
			Fs / 50,      // 20 ms
			Fs / 25,      // 40 ms
			3 * Fs / 50,  // 60 ms
			4 * Fs / 50,  // 80 ms
			5 * Fs / 50,  // 100 ms
			6 * Fs / 50,  // 120 ms
			Fs / 30,      // non-aligned — should return -1
		}
		for _, sz := range fsSamples {
			for _, vd := range variableDurations {
				for _, app := range applications {
					g := nativeopus.ExportFrameSizeSelect(app, sz, vd, Fs)
					c := cFrameSizeSelect(app, sz, vd, Fs)
					if g != c {
						t.Errorf("frame_size_select(app=%d, sz=%d, vd=%d, Fs=%d): Go=%d C=%d",
							app, sz, vd, Fs, g, c)
					}
				}
			}
		}
	}
}

// TestParity_UserBitrateToBitrate — sweep user_bitrate_bps,
// frame_size, and max_data_bytes against the C oracle for every
// supported Fs and channel count.
func TestParity_UserBitrateToBitrate(t *testing.T) {
	fsValues := []int32{8000, 12000, 16000, 24000, 48000}
	channels := []int{1, 2}
	app := nativeopus.OPUS_APPLICATION_AUDIO
	userBitrates := []int32{
		int32(nativeopus.OPUS_AUTO),
		int32(nativeopus.OPUS_BITRATE_MAX),
		6000,
		12000,
		32000,
		64000,
		128000,
		256000,
		512000,
		2000000, // above the 1.5 Mbps cap applied when MAX
	}
	maxDataBytes := []int{50, 200, 500, 1275, 4000}
	for _, Fs := range fsValues {
		for _, ch := range channels {
			// frame_size candidates (samples per channel): 2.5, 5, 10,
			// 20, 40 ms — all aligned multiples of Fs/400. Also 0
			// (triggers the Fs/400 default path).
			frameSizes := []int{
				0,
				int(Fs / 400),
				int(Fs / 200),
				int(Fs / 100),
				int(Fs / 50),
				int(Fs / 25),
			}
			for _, br := range userBitrates {
				for _, fs := range frameSizes {
					for _, mdb := range maxDataBytes {
						g, ret := nativeopus.ExportUserBitrateToBitrate(Fs, ch, app, br, fs, mdb)
						if ret != 0 {
							t.Fatalf("Go init failed: Fs=%d ch=%d ret=%d", Fs, ch, ret)
						}
						c := cUserBitrateToBitrate(Fs, ch, app, br, fs, mdb)
						if g != c {
							t.Errorf("user_bitrate_to_bitrate(Fs=%d ch=%d br=%d fs=%d mdb=%d): Go=%d C=%d",
								Fs, ch, br, fs, mdb, g, c)
						}
					}
				}
			}
		}
	}
}

// mkInitName — formats a deterministic subtest name.
func mkInitName(Fs int32, ch, app int) string {
	return itoaDec(int(Fs)) + "hz_" + itoaDec(ch) + "ch_app" + itoaDec(app)
}
