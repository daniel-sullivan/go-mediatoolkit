//go:build cgo && opus_strict

package benchcmp

import (
	"errors"
	"math"
	"testing"

	opus "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
)

func sinF32(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	return out
}

func sinF64(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/48000)
	}
	return out
}

func makePacket(app, bitrate int) []byte {
	enc := NewCEncoder(48000, 1, app)
	defer enc.Destroy()
	enc.SetBitrate(bitrate)
	enc.SetComplexity(10)
	pcm := sinF32(960)
	pkt := make([]byte, 1275)
	n := enc.Encode(pcm, pkt)
	return pkt[:n]
}

// skipIfUnimplemented skips benchmarks that depend on the native Go codec
// while the 1:1 C-to-Go rewrite is in progress.
func skipIfUnimplemented(b *testing.B, err error) {
	b.Helper()
	if errors.Is(err, opus.ErrUnimplemented) {
		b.Skip("native Go impl unimplemented — rewriting via 1:1 C port")
	}
}

// ── CELT encode ─────────────────────────────────────────────────────

func BenchmarkEncodeCELT_C(b *testing.B) {
	enc := NewCEncoder(48000, 1, AppRestrictedLowDel)
	defer enc.Destroy()
	enc.SetBitrate(64000)
	enc.SetComplexity(10)
	pcm := sinF32(960)
	pkt := make([]byte, 1275)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(pcm, pkt)
	}
}

func BenchmarkEncodeCELT_Go(b *testing.B) {
	enc, err := opus.NewNativeEncoder(48000, 1, opus.WithApplication(opus.AppLowDelay))
	skipIfUnimplemented(b, err)
	if err != nil {
		b.Fatalf("NewNativeEncoder: %v", err)
	}
	pcm := sinF64(960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(pcm, 1275)
	}
}

// ── CELT decode ─────────────────────────────────────────────────────

func BenchmarkDecodeCELT_C(b *testing.B) {
	pkt := makePacket(AppRestrictedLowDel, 64000)
	dec := NewCDecoder(48000, 1)
	defer dec.Destroy()
	out := make([]float32, 960)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, out)
	}
}

func BenchmarkDecodeCELT_Go(b *testing.B) {
	pkt := makePacket(AppRestrictedLowDel, 64000)
	dec, err := opus.NewNativeDecoder(48000, 1)
	skipIfUnimplemented(b, err)
	if err != nil {
		b.Fatalf("NewNativeDecoder: %v", err)
	}
	out := make([]float64, opus.MaxFrameSize(48000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, out)
	}
}

// ── SILK decode ─────────────────────────────────────────────────────

func BenchmarkDecodeSILK_C(b *testing.B) {
	pkt := makePacket(AppVOIP, 12000)
	dec := NewCDecoder(48000, 1)
	defer dec.Destroy()
	out := make([]float32, 960)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, out)
	}
}

func BenchmarkDecodeSILK_Go(b *testing.B) {
	pkt := makePacket(AppVOIP, 12000)
	dec, err := opus.NewNativeDecoder(48000, 1)
	skipIfUnimplemented(b, err)
	if err != nil {
		b.Fatalf("NewNativeDecoder: %v", err)
	}
	out := make([]float64, opus.MaxFrameSize(48000))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, out)
	}
}

// ── SILK encode ─────────────────────────────────────────────────────

func BenchmarkEncodeSILK_C(b *testing.B) {
	enc := NewCEncoder(48000, 1, AppVOIP)
	defer enc.Destroy()
	enc.SetBitrate(12000)
	enc.SetComplexity(10)
	pcm := sinF32(960)
	pkt := make([]byte, 1275)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(pcm, pkt)
	}
}

func BenchmarkEncodeSILK_Go(b *testing.B) {
	enc, err := opus.NewNativeEncoder(48000, 1, opus.WithApplication(opus.AppVoIP), opus.WithBitrate(12000))
	skipIfUnimplemented(b, err)
	if err != nil {
		b.Fatalf("NewNativeEncoder: %v", err)
	}
	pcm := sinF64(960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(pcm, 1275)
	}
}
