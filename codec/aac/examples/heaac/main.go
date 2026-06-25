// HE-AAC v2 example: encode a STEREO signal as HE-AAC v2 (AOT-29, parametric
// stereo) and decode it back, demonstrating the family's most advanced mode —
// a mono AAC-LC core + SBR + parametric stereo that reconstructs a 2-channel,
// SBR-doubled-rate output.
//
// This uses the libraries/aac layer directly (not the codec/aac streaming
// wrapper) because the round-trip needs two things the per-packet layer exposes
// and the streaming wrapper does not: the encoder's resulting
// AudioSpecificConfig (Config()) — which carries the explicit-hierarchical
// HE-AAC signalling the decoder needs — and one-access-unit-at-a-time framing.
//
// Two HE-AAC v2 facts this teaches:
//   - Output projection: an AOT-29 stream reports a STEREO (2-channel) output
//     at the SBR-DOUBLED sample rate even though its coded core is mono at the
//     half rate. AudioSpecificConfig.Output() reports that up front.
//   - Stereo-only: parametric stereo requires 2 input channels; encoding a
//     mono input as AOT-29 returns ErrPSRequiresStereo (shown at the end).
//
// FDK-AAC is the only AAC engine and is fenced behind the aacfdk build tag, so
// a default build surfaces ErrEngineRequiresFDK; this example reports that and
// exits cleanly. Build with `-tags aacfdk` to run the round-trip for real:
//
//	go run -tags aacfdk ./codec/aac/examples/heaac
package main

import (
	"errors"
	"log"
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

func main() {
	const (
		outRate  = consts.SampleRate44100 // the SBR-doubled OUTPUT rate
		channels = 2                      // PS needs a stereo input
		frames   = 16
	)

	// Build a phase-shifted, level-scaled stereo chirp so the parametric-stereo
	// tool has genuine inter-channel intensity/coherence to model (two pure
	// decorrelated tones at a low PS bitrate would collapse to near-silence).
	frame := aaclib.FrameSamplesLong // PS codes long (SBR-doubled) frames
	stereo := make([]float64, frames*frame*channels)
	for n := 0; n < frames*frame; n++ {
		t0 := float64(n) / float64(outRate)
		f0 := 200.0 + 30.0*t0
		l := 0.5*math.Sin(2*math.Pi*f0*t0*(1+0.3*t0)) + 0.15*math.Sin(2*math.Pi*2500*t0)
		r := 0.4*math.Sin(2*math.Pi*f0*t0*(1+0.3*t0)+0.6) + 0.2*math.Sin(2*math.Pi*1700*t0)
		stereo[n*channels] = l
		stereo[n*channels+1] = r
	}

	// Encode HE-AAC v2: WithObjectType(AOTPS) selects the mono-core + SBR + PS
	// chain. The encoder takes the OUTPUT (post-SBR) sample rate and the stereo
	// input; internally it downsamples to a mono AAC-LC core and layers SBR+PS.
	enc, err := aaclib.NewEncoder(outRate, channels,
		aaclib.WithObjectType(aaclib.AOTPS),
		aaclib.WithBitrate(32000),
	)
	if err != nil {
		if errors.Is(err, aaclib.ErrEngineRequiresFDK) {
			log.Printf("HE-AAC v2 requires the FDK engine; rebuild with -tags aacfdk to run this example")
			return
		}
		log.Fatal(err)
	}

	asc := enc.Config() // the explicit-hierarchical ASC the decoder needs
	rate, ch := asc.Output()
	log.Printf("encoder ASC: %s, output %d Hz, %d ch (mono core + SBR + PS upmix), %d samples/frame",
		asc.ObjectType, rate, ch, asc.FrameSamples)

	// Encode one access unit per long frame (FrameSamplesLong samples/channel).
	var packets [][]byte
	for off := 0; off+frame*channels <= len(stereo); off += frame * channels {
		pkt, err := enc.Encode(stereo[off : off+frame*channels])
		if err != nil {
			log.Fatal(err)
		}
		if len(pkt) > 0 {
			packets = append(packets, pkt)
		}
	}
	log.Printf("encoded %d stereo samples -> %d HE-AAC v2 access units", len(stereo), len(packets))

	// Decode back. The decoder reports a 2-channel output (PS upmix) even
	// though the core is mono — Output() above already projected that.
	dec, err := aaclib.NewDecoder(asc)
	if err != nil {
		log.Fatal(err)
	}
	pcm := make([]float64, aaclib.FrameSamplesLong*dec.Channels())
	var decoded int
	var peak float64
	for _, pkt := range packets {
		n, err := dec.Decode(pkt, pcm)
		if err != nil {
			log.Fatal(err)
		}
		decoded += n
		for i := 0; i < n*dec.Channels(); i++ {
			if a := math.Abs(pcm[i]); a > peak {
				peak = a
			}
		}
	}
	log.Printf("decoded %d samples/channel at %d Hz, %d ch, peak %.4f",
		decoded, dec.SampleRate(), dec.Channels(), peak)

	// Stereo-only guard: encoding a MONO input as AOT-29 is rejected with the
	// clear ErrPSRequiresStereo sentinel — parametric stereo has no meaning
	// without two channels to model.
	if _, err := aaclib.NewEncoder(outRate, 1, aaclib.WithObjectType(aaclib.AOTPS)); errors.Is(err, aaclib.ErrPSRequiresStereo) {
		log.Printf("mono AOT-29 correctly rejected: %v", err)
	}
}
