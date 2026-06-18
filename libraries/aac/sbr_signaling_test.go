package aac

import "testing"

// TestSbrParamsFromASC verifies the explicit HE-AAC (AOT-5) AudioSpecificConfig
// SBR signalling parse that the native decoder uses to route a stream to the
// SBR-upsampling engine and to recover the AAC-LC core rate and the SBR-doubled
// output rate. The ASC byte strings are the exact ones libfdk-aac emits for the
// listed configurations (verified against the encoder).
func TestSbrParamsFromASC(t *testing.T) {
	cases := []struct {
		name     string
		raw      []byte
		channels int
		wantSBR  bool
		wantCore int
		wantOut  int
	}{
		// audioObjectType=5, sfi (core), chCfg, extSfi (output), coreAOT=2.
		{"mono-44100", []byte{0x2b, 0x8a, 0x08, 0x00}, 1, true, 22050, 44100},
		{"stereo-44100", []byte{0x2b, 0x92, 0x08, 0x00}, 2, true, 22050, 44100},
		{"mono-48000", []byte{0x2b, 0x09, 0x88, 0x00}, 1, true, 24000, 48000},
		{"stereo-32000", []byte{0x2c, 0x12, 0x88, 0x00}, 2, true, 16000, 32000},
		{"mono-24000", []byte{0x2c, 0x8b, 0x08, 0x00}, 1, true, 12000, 24000},
		// AAC-LC (AOT=2): not SBR.
		{"aaclc-44100", []byte{0x12, 0x10}, 2, false, 44100, 44100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ot := AOTAACLC
			if tc.wantSBR {
				ot = AOTSBR
			}
			asc := AudioSpecificConfig{
				ObjectType: ot,
				SampleRate: tc.wantOut, // resolved extension/output rate
				Channels:   tc.channels,
				Raw:        tc.raw,
			}
			sbr, core, out := sbrParamsFromASC(asc)
			if sbr != tc.wantSBR {
				t.Fatalf("sbr = %v, want %v", sbr, tc.wantSBR)
			}
			if !tc.wantSBR {
				return
			}
			if core != tc.wantCore {
				t.Errorf("coreRate = %d, want %d", core, tc.wantCore)
			}
			if out != tc.wantOut {
				t.Errorf("outRate = %d, want %d", out, tc.wantOut)
			}
			if out != 2*core {
				t.Errorf("outRate %d != 2*coreRate %d", out, core)
			}
		})
	}
}

// TestSbrParamsFromASCFallback verifies that when the raw ASC bytes are missing
// or unparseable but the object type is AOT-5, the parser falls back to treating
// asc.SampleRate as the output rate (core = output/2).
func TestSbrParamsFromASCFallback(t *testing.T) {
	asc := AudioSpecificConfig{ObjectType: AOTSBR, SampleRate: 44100, Channels: 2}
	sbr, core, out := sbrParamsFromASC(asc)
	if !sbr || out != 44100 || core != 22050 {
		t.Fatalf("fallback: sbr=%v core=%d out=%d, want true/22050/44100", sbr, core, out)
	}
}

// TestSbrParamsFromASCPS verifies an AOT-29 (PS / HE-AAC v2) config is also
// recognised as SBR and yields the SBR-doubled output rate. The PS vector is
// AOT-29, core sfi 22050 (mono core), ext sfi 44100, coreAOT=2.
func TestSbrParamsFromASCPS(t *testing.T) {
	asc := AudioSpecificConfig{ObjectType: AOTPS, SampleRate: 44100, Channels: 2, Raw: []byte{0xeb, 0x8a, 0x08, 0x00}}
	sbr, core, out := sbrParamsFromASC(asc)
	if !sbr || core != 22050 || out != 44100 {
		t.Fatalf("ps: sbr=%v core=%d out=%d, want true/22050/44100", sbr, core, out)
	}
}

// TestAudioSpecificConfigOutput verifies the public Output projection: AOT-5
// reports the SBR-doubled rate, AOT-29 additionally widens a mono core to a
// stereo output, and plain AAC-LC is unchanged.
func TestAudioSpecificConfigOutput(t *testing.T) {
	cases := []struct {
		name     string
		asc      AudioSpecificConfig
		wantRate int
		wantChan int
	}{
		{
			"sbr mono 44100",
			AudioSpecificConfig{ObjectType: AOTSBR, SampleRate: 44100, Channels: 1, Raw: []byte{0x2b, 0x8a, 0x08, 0x00}},
			44100, 1,
		},
		{
			"sbr stereo 48000",
			AudioSpecificConfig{ObjectType: AOTSBR, SampleRate: 48000, Channels: 2, Raw: []byte{0x2b, 0x11, 0x88, 0x00}},
			48000, 2,
		},
		{
			"ps mono-core -> stereo 44100",
			AudioSpecificConfig{ObjectType: AOTPS, SampleRate: 44100, Channels: 1, Raw: []byte{0xeb, 0x8a, 0x08, 0x00}},
			44100, 2,
		},
		{
			"aac-lc stereo 44100 unchanged",
			AudioSpecificConfig{ObjectType: AOTAACLC, SampleRate: 44100, Channels: 2, Raw: []byte{0x12, 0x10}},
			44100, 2,
		},
		{
			"ps fallback no raw -> stereo",
			AudioSpecificConfig{ObjectType: AOTPS, SampleRate: 32000, Channels: 1},
			32000, 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rate, ch := tc.asc.Output()
			if rate != tc.wantRate || ch != tc.wantChan {
				t.Fatalf("Output() = (%d, %d), want (%d, %d)", rate, ch, tc.wantRate, tc.wantChan)
			}
		})
	}
}

// TestAudioObjectTypeStringPS verifies the AOT-29 String mapping.
func TestAudioObjectTypeStringPS(t *testing.T) {
	if AOTPS.String() != "PS" {
		t.Fatalf("AOTPS.String() = %q, want PS", AOTPS.String())
	}
	if AOTPS != 29 {
		t.Fatalf("AOTPS = %d, want 29", AOTPS)
	}
}
