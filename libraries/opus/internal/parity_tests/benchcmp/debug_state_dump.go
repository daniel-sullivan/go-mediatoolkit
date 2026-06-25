//go:build cgo

package benchcmp

// debug_state_dump.go — drift-bisection diagnostic harness.
//
// This package is pure infrastructure: it does not fix any drift. Its
// job is to give a follow-up agent (or human) a fast, structured way
// to identify the FIRST field that diverges between the C libopus
// reference encoder and the Go port at any frame boundary across the
// supported opus_encode_float matrix.
//
// Usage outline:
//
//     report := BisectCELTFrame(MatrixConfig{
//         Fs: 24000, Channels: 1, FrameMs: 20,
//         Bitrate: 48000, App: nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY,
//     }, 2 /* encode 2 frames; report divergence from frame 0..1 */)
//     fmt.Println(report.Format())
//
// Fields are named identically on the C (CEncoderStateDump) and Go
// (nativeopus.GoEncoderStateDump) sides; DumpEncoderStateC and
// DumpEncoderStateGo flatten both into a parallel []StateField list so
// DiffEncoderStates can walk them side-by-side and return every
// divergence with both values plus a ULP delta when both values are
// IEEE-float-shaped.

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// StateField is a single (name, type, value) tuple in a flattened
// encoder-state dump. Ordering of the list matters: it must be
// identical between the C and Go dumpers so DiffEncoderStates can
// pair them up by index.
type StateField struct {
	Name  string
	Kind  StateFieldKind
	Value uint64 // upcast int/uint/bits for uniform compare
}

// StateFieldKind distinguishes ints from float-bit-patterns so the
// diff routine can render ULP deltas for floats.
type StateFieldKind uint8

const (
	// KindInt32 — signed 32-bit integer (upcast).
	KindInt32 StateFieldKind = iota + 1
	// KindUint32 — unsigned 32-bit integer (raw).
	KindUint32
	// KindFloat32Bits — uint32 holding an IEEE float32 bit pattern.
	KindFloat32Bits
	// KindCRC — FNV-1a hash of an array; not directly comparable as
	// a float, but a mismatch means at least one element differs.
	KindCRC
)

// Difference captures a single (name, C-value, Go-value) mismatch
// between two parallel state dumps.
type Difference struct {
	Name  string
	Kind  StateFieldKind
	CVal  uint64
	GoVal uint64
	ULP   int64 // meaningful only when Kind == KindFloat32Bits
}

// String renders a Difference into a human-readable line.
func (d Difference) String() string {
	switch d.Kind {
	case KindFloat32Bits:
		cf := math.Float32frombits(uint32(d.CVal))
		gf := math.Float32frombits(uint32(d.GoVal))
		return fmt.Sprintf("%s: C=%g (0x%08x) Go=%g (0x%08x) ULP=%d",
			d.Name, cf, uint32(d.CVal), gf, uint32(d.GoVal), d.ULP)
	case KindUint32, KindCRC:
		return fmt.Sprintf("%s: C=0x%08x Go=0x%08x", d.Name, uint32(d.CVal), uint32(d.GoVal))
	case KindInt32:
		return fmt.Sprintf("%s: C=%d Go=%d", d.Name, int32(d.CVal), int32(d.GoVal))
	default:
		return fmt.Sprintf("%s: C=%d Go=%d (kind?)", d.Name, d.CVal, d.GoVal)
	}
}

// DumpEncoderStateC flattens a CEncoderStateDump into an ordered list
// of StateField values. The ordering must match DumpEncoderStateGo.
func DumpEncoderStateC(s *CEncoderStateDump) []StateField {
	return dumpViaReflect(reflect.ValueOf(s).Elem(), "C")
}

// DumpEncoderStateGo flattens a nativeopus.GoEncoderStateDump identically.
func DumpEncoderStateGo(s *nativeopus.GoEncoderStateDump) []StateField {
	return dumpViaReflect(reflect.ValueOf(s).Elem(), "Go")
}

// dumpViaReflect walks the struct in declaration order and emits one
// StateField per scalar (int32/uint32). Arrays are emitted one element
// at a time with an indexed name suffix. The `_side` parameter is
// unused in the output but documents intent.
func dumpViaReflect(v reflect.Value, _side string) []StateField {
	out := make([]StateField, 0, 256)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fv := v.Field(i)
		name := f.Name
		kindHint := inferKind(name, f.Type)
		switch fv.Kind() {
		case reflect.Int32:
			out = append(out, StateField{Name: name, Kind: KindInt32, Value: uint64(uint32(fv.Int()))})
		case reflect.Uint32:
			out = append(out, StateField{Name: name, Kind: kindHint, Value: fv.Uint()})
		case reflect.Array:
			et := f.Type.Elem()
			elemKind := inferKind(name, et)
			if et.Kind() == reflect.Uint32 {
				n := fv.Len()
				for j := 0; j < n; j++ {
					out = append(out, StateField{
						Name:  fmt.Sprintf("%s[%d]", name, j),
						Kind:  elemKind,
						Value: fv.Index(j).Uint(),
					})
				}
			} else if et.Kind() == reflect.Int32 {
				n := fv.Len()
				for j := 0; j < n; j++ {
					out = append(out, StateField{
						Name:  fmt.Sprintf("%s[%d]", name, j),
						Kind:  KindInt32,
						Value: uint64(uint32(fv.Index(j).Int())),
					})
				}
			}
		default:
			// Skip any field we don't understand; the parallel dumper
			// will skip the same field name so alignment holds.
		}
	}
	return out
}

// inferKind classifies a field as a float-bit-pattern, CRC, or plain
// uint based on its name suffix. Used by the render path to compute
// ULP deltas when comparing floats.
func inferKind(name string, t reflect.Type) StateFieldKind {
	switch t.Kind() {
	case reflect.Int32:
		return KindInt32
	case reflect.Uint32, reflect.Array:
		lname := strings.ToLower(name)
		if strings.Contains(lname, "bits") || strings.Contains(lname, "f16") {
			return KindFloat32Bits
		}
		if strings.Contains(lname, "crc") {
			return KindCRC
		}
		return KindUint32
	}
	return KindUint32
}

// benignFields — field names whose C/Go values differ by design and
// are NOT drift signals. The offsets are Go-side symbolic (sizeof(*) is
// 1 in the Go port since allocation is GC-managed). AnalysisInfo's
// `valid` and `bandwidth` are Go `int` (64-bit) vs C `int` (32-bit) —
// FNV over a 4-byte prefix differs because the upper bytes on Go side
// are zero while the C side has the low 32 bits.
var benignFields = map[string]bool{
	"CeltEncOffset": true,
	"SilkEncOffset": true,
	// AnalysisInfo CRC differs due to `int` field layout mismatch
	// between C (32-bit) and Go (default 64-bit). The content is
	// semantically identical; this field is not a drift signal.
	"AnInfoFpCrc": true,
}

// DiffEncoderStates pairs two dumps by index and returns every field
// where the C and Go values differ. If the lists have different
// lengths an error-style entry is prepended. Fields listed in
// benignFields are skipped — their C/Go values always differ by
// design (offsets are symbolic on the Go side, for instance).
func DiffEncoderStates(a, b []StateField) []Difference {
	if len(a) != len(b) {
		return []Difference{{Name: fmt.Sprintf("<<length mismatch C=%d Go=%d>>", len(a), len(b))}}
	}
	var diffs []Difference
	for i := range a {
		if a[i].Name != b[i].Name {
			diffs = append(diffs, Difference{
				Name: fmt.Sprintf("<<name mismatch at %d: C=%q Go=%q>>", i, a[i].Name, b[i].Name),
			})
			break
		}
		if a[i].Value == b[i].Value {
			continue
		}
		if benignFields[a[i].Name] {
			continue
		}
		d := Difference{Name: a[i].Name, Kind: a[i].Kind, CVal: a[i].Value, GoVal: b[i].Value}
		if a[i].Kind == KindFloat32Bits {
			d.ULP = ulpDeltaF32(uint32(a[i].Value), uint32(b[i].Value))
		}
		diffs = append(diffs, d)
	}
	return diffs
}

// ulpDeltaF32 returns the signed distance (in ULPs) between two float
// bit patterns. Sign-magnitude is converted to biased two's complement
// before subtracting so adjacent floats on either side of zero are 1
// ULP apart.
func ulpDeltaF32(a, b uint32) int64 {
	const signBit = uint32(1 << 31)
	toOrdered := func(u uint32) int64 {
		if u&signBit != 0 {
			// Negative: flip all bits so negative magnitudes sort
			// below positive and larger |value| is further below.
			return -int64(^u&0x7fffffff) - 1
		}
		return int64(u)
	}
	return toOrdered(a) - toOrdered(b)
}

// MatrixConfig captures a single (Fs, channels, frameMs, bitrate, app)
// point from TestParity_OpusEncode_Matrix.
type MatrixConfig struct {
	Fs       int32
	Channels int
	FrameMs  int
	Bitrate  int32
	App      int
}

// BisectReport summarises the first frame + field where C and Go
// diverge for a given config.
type BisectReport struct {
	Config                 MatrixConfig
	NumFramesEncoded       int
	PacketParityOK         bool
	FirstDivergentFrame    int
	FirstDivergentLocation string
	CValue                 uint64
	GoValue                uint64
	ULP                    int64
	AllDifferencesAtFrame  []Difference
	UpstreamCandidates     []string
	PerFramePacketMismatch []int // frame indices with packet byte mismatch
}

// Format renders a BisectReport into a multi-line string.
func (r BisectReport) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "BisectReport for Fs=%d c=%d %dms %dbps app=%d\n",
		r.Config.Fs, r.Config.Channels, r.Config.FrameMs, r.Config.Bitrate, r.Config.App)
	fmt.Fprintf(&b, "  Frames encoded: %d\n", r.NumFramesEncoded)
	fmt.Fprintf(&b, "  Packet parity OK: %v\n", r.PacketParityOK)
	if len(r.PerFramePacketMismatch) > 0 {
		fmt.Fprintf(&b, "  Packet mismatch at frame(s): %v\n", r.PerFramePacketMismatch)
	}
	if r.FirstDivergentLocation == "" {
		fmt.Fprintf(&b, "  No tracked-field divergence detected.\n")
	} else {
		fmt.Fprintf(&b, "  First divergent frame: %d\n", r.FirstDivergentFrame)
		fmt.Fprintf(&b, "  First divergent location: %s\n", r.FirstDivergentLocation)
		switch {
		case r.ULP != 0:
			cf := math.Float32frombits(uint32(r.CValue))
			gf := math.Float32frombits(uint32(r.GoValue))
			fmt.Fprintf(&b, "  C: %g (0x%08x)\n", cf, uint32(r.CValue))
			fmt.Fprintf(&b, "  Go: %g (0x%08x)\n", gf, uint32(r.GoValue))
			fmt.Fprintf(&b, "  ULP: %d\n", r.ULP)
		default:
			fmt.Fprintf(&b, "  C: %d (0x%08x)\n", int32(r.CValue), uint32(r.CValue))
			fmt.Fprintf(&b, "  Go: %d (0x%08x)\n", int32(r.GoValue), uint32(r.GoValue))
		}
	}
	if len(r.AllDifferencesAtFrame) > 0 {
		fmt.Fprintf(&b, "  All differences at first divergent frame:\n")
		for _, d := range r.AllDifferencesAtFrame {
			fmt.Fprintf(&b, "    - %s\n", d.String())
		}
	}
	if len(r.UpstreamCandidates) > 0 {
		fmt.Fprintf(&b, "  Upstream candidate hints:\n")
		for _, s := range r.UpstreamCandidates {
			fmt.Fprintf(&b, "    - %s\n", s)
		}
	}
	return b.String()
}

// BisectCELTFrame runs `numFrames` frames through both the C and Go
// encoders for the given config, comparing packet bytes and post-frame
// state dumps at every frame boundary. Returns a BisectReport pinned
// to the FIRST divergence it sees.
//
// The caller is responsible for supplying the shared CELT mode via
// SetBisectCeltMode before calling; this avoids re-building the mode
// per bisect run.
func BisectCELTFrame(cfg MatrixConfig, numFrames int) BisectReport {
	r := BisectReport{Config: cfg, PacketParityOK: true}
	frameSize := int(cfg.Fs) * cfg.FrameMs / 1000

	cEnc := NewCEncoder(int(cfg.Fs), cfg.Channels, cfg.App)
	if cEnc == nil {
		r.FirstDivergentLocation = "<<C encoder create failed>>"
		return r
	}
	defer cEnc.Destroy()
	cEnc.SetBitrate(int(cfg.Bitrate))

	goEnc, gerr := nativeopus.ExportOpusEncoderCreate(cfg.Fs, cfg.Channels, cfg.App)
	if gerr != nativeopus.OPUS_OK {
		r.FirstDivergentLocation = fmt.Sprintf("<<Go encoder create failed: %d>>", gerr)
		return r
	}
	if bisectCeltModeSet {
		if ret := nativeopus.ExportSetEncoderCeltMode(goEnc, bisectCeltMode); ret != nativeopus.OPUS_OK {
			r.FirstDivergentLocation = fmt.Sprintf("<<Go celt mode install failed: %d>>", ret)
			return r
		}
	}
	if ret := nativeopus.ExportOpusEncoderCtl(goEnc, nativeopus.OPUS_SET_BITRATE_REQUEST, cfg.Bitrate); ret != nativeopus.OPUS_OK {
		r.FirstDivergentLocation = fmt.Sprintf("<<Go SET_BITRATE failed: %d>>", ret)
		return r
	}

	pcm := make([]float32, frameSize*cfg.Channels)
	cPkt := make([]byte, 4000)
	goPkt := make([]byte, 4000)

	for f := 0; f < numFrames; f++ {
		bisectGeneratePCM(pcm, f, int(cfg.Fs))

		cn, cDump := CEncodeAndDump(cEnc, pcm, frameSize, cPkt)
		gn := int(nativeopus.ExportOpusEncodeFloat(goEnc, pcm, frameSize, goPkt, int32(len(goPkt))))
		goDump := nativeopus.ExportDumpGoEncoderState(goEnc)

		r.NumFramesEncoded = f + 1
		if cn < 0 || gn < 0 || cn != gn || !bytesEqual(cPkt[:cn], goPkt[:gn]) {
			r.PacketParityOK = false
			r.PerFramePacketMismatch = append(r.PerFramePacketMismatch, f)
		}

		cFields := DumpEncoderStateC(&cDump)
		goFields := DumpEncoderStateGo(&goDump)
		diffs := DiffEncoderStates(cFields, goFields)
		if len(diffs) > 0 && r.FirstDivergentLocation == "" {
			r.FirstDivergentFrame = f
			r.FirstDivergentLocation = diffs[0].Name
			r.CValue = diffs[0].CVal
			r.GoValue = diffs[0].GoVal
			r.ULP = diffs[0].ULP
			// Sort by name for stable output.
			sort.SliceStable(diffs, func(i, j int) bool { return diffs[i].Name < diffs[j].Name })
			r.AllDifferencesAtFrame = diffs
			r.UpstreamCandidates = inferUpstream(diffs)
		}
	}

	return r
}

// inferUpstream applies crude heuristics to turn a list of state diffs
// into a set of candidate "likely culprit" hints. This is intentionally
// not exhaustive — the point is to give a follow-up agent a jumping-off
// point rather than solve the bisect itself.
func inferUpstream(diffs []Difference) []string {
	var hints []string
	for _, d := range diffs {
		switch {
		case strings.HasPrefix(d.Name, "CeltOldLogE2F16"),
			strings.HasPrefix(d.Name, "CeltOldLogE2"):
			hints = append(hints, d.Name+
				" differs — check quant_coarse_energy output (CELT oldLogE2 is set to oldLogE after encode)")
		case strings.HasPrefix(d.Name, "CeltOldLogEF16"),
			strings.HasPrefix(d.Name, "CeltOldLogE"):
			hints = append(hints, d.Name+
				" differs — check quant_coarse_energy / amp2Log2 / bandLogE chain")
		case strings.HasPrefix(d.Name, "CeltEnergyError"):
			hints = append(hints, d.Name+
				" differs — check quant_fine_energy error feedback; likely FP contract in ADD/SUB chain")
		case strings.HasPrefix(d.Name, "CeltOldBandE"):
			hints = append(hints, d.Name+
				" differs — check compute_band_energies (celt_inner_prod / celt_sqrt / amp2Log2)")
		case strings.HasPrefix(d.Name, "CeltTonalAverage"):
			hints = append(hints, d.Name+
				" differs — check spreading_decision update; divergence often traces to hf_sum / spread_weight")
		case strings.HasPrefix(d.Name, "CeltPreemphMem"):
			hints = append(hints, d.Name+
				" differs — check preemphasis at celt_preemphasis()")
		case strings.HasPrefix(d.Name, "CeltRng"):
			hints = append(hints, d.Name+
				" differs — range coder state drift; by this point at least one quant_* call must have written a different symbol")
		case strings.HasPrefix(d.Name, "Silk"):
			hints = append(hints, d.Name+" differs — SILK-path state drift; check silk_encode_frame_FLP")
		}
	}
	// De-duplicate while preserving order.
	seen := map[string]bool{}
	var out []string
	for _, h := range hints {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// bisectCeltMode holds the shared CELT mode handle; callers should set
// it once via SetBisectCeltMode before invoking BisectCELTFrame.
var (
	bisectCeltMode    nativeopus.CeltModeHandle
	bisectCeltModeSet bool
)

// SetBisectCeltMode installs the CELT mode handle used by
// BisectCELTFrame when creating Go encoders.
func SetBisectCeltMode(h nativeopus.CeltModeHandle) {
	bisectCeltMode = h
	bisectCeltModeSet = true
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bisectGeneratePCM mirrors the private generatePCM helper from
// parity_silk_encode_mono_test.go (kept in-sync so the harness is
// callable from non-test code). If the two ever diverge the harness
// reports divergences against synthetic PCM that the matrix test
// doesn't use, so keep them identical.
func bisectGeneratePCM(dst []float32, frameIdx, sampleRate int) {
	n := len(dst)
	baseT := frameIdx * n
	for i := 0; i < n; i++ {
		t := float64(baseT+i) / float64(sampleRate)
		v := 0.3*sin2pi(440*t) +
			0.15*sin2pi(1000*t) +
			0.02*sin2pi(47*float64(baseT+i)/13.0)
		dst[i] = float32(v)
	}
}

// sin2pi — local sine wrapper to avoid pulling math into this file's
// top-of-file imports if unused; forwarded to math.Sin with the 2π
// multiplier.
func sin2pi(x float64) float64 { return mathSin(2 * math.Pi * x) }

// Indirection so refactors don't shadow the math import.
func mathSin(x float64) float64 { return math.Sin(x) }
