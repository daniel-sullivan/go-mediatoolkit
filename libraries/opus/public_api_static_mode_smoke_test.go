package opus

import "testing"

// TestPublicAPI_StaticModeSmoke exercises the minimum Phase 11 wiring
// requirement: construct a native encoder and decoder at 48 kHz mono
// and round-trip a single frame of PCM through both. Prior to the
// static_modes_float.go port this would crash because
// opus_encoder_init / opus_decoder_init left st.celt_enc.mode == nil.
func TestPublicAPI_StaticModeSmoke(t *testing.T) {
	enc, err := NewNativeEncoder(Rate48000, 1, WithBitrate(64000), WithApplication(AppAudio))
	if err != nil {
		t.Fatalf("NewNativeEncoder: %v", err)
	}
	dec, err := NewNativeDecoder(Rate48000, 1)
	if err != nil {
		t.Fatalf("NewNativeDecoder: %v", err)
	}
	pcm := synthPCM(Rate48000, 1, 960, 1) // one 20ms frame
	pkt, err := enc.Encode(pcm, MaxFrameBytes)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(pkt) == 0 {
		t.Fatal("empty packet")
	}
	out := make([]float64, 960)
	n, err := dec.Decode(pkt, out)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != 960 {
		t.Fatalf("decoded %d samples, want 960", n)
	}
}
