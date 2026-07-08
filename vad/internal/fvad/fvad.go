package fvad

// This file ports src/fvad.c + include/fvad.h — the public libfvad
// API: instance lifecycle, mode and sample-rate selection, and frame
// processing.

import "errors"

// Sentinel errors for the three invalid-input cases the C API signals
// with -1 returns (or cannot signal at all — C's fvad_process asserts
// on unset state; the Go API validates instead).
var (
	// ErrInvalidMode is returned by SetMode for a mode outside 0..3.
	ErrInvalidMode = errors.New("fvad: mode must be in 0..3")
	// ErrInvalidSampleRate is returned by SetSampleRate for a rate
	// other than 8000, 16000, 32000 or 48000 Hz.
	ErrInvalidSampleRate = errors.New("fvad: sample rate must be 8000, 16000, 32000 or 48000")
	// ErrInvalidFrameLength is returned by Process for a frame that is
	// not exactly 10, 20 or 30 ms at the configured sample rate.
	ErrInvalidFrameLength = errors.New("fvad: frame length must be 10, 20 or 30 ms of samples")
)

// validRates mirrors fvad.c's valid_rates (kHz).
var validRates = [4]int{8, 16, 32, 48}

// validFrameTimes mirrors fvad.c's valid_frame_times (ms).
var validFrameTimes = [3]int{10, 20, 30}

// VAD mirrors struct Fvad: a voice-activity detector instance. Not
// safe for concurrent use; drive it from one goroutine (or wrap it the
// way vad.WebRTCDetector does).
type VAD struct {
	core    Inst
	rateIdx int // index into validRates
}

// New creates and initializes a VAD instance (default mode 0 "quality",
// default sample rate 8000 Hz), mirroring fvad_new. The error is
// always nil today (the C's only failure is malloc) and reserved by
// the package convention that constructors return errors.
func New() (*VAD, error) {
	v := new(VAD)
	v.Reset()
	return v, nil
}

// Reset ports fvad_reset: reinitialize the instance, clearing all
// state and resetting mode and sample rate to their defaults (mode 0,
// 8000 Hz).
func (v *VAD) Reset() {
	v.core.InitCore()
	v.rateIdx = 0
}

// SetMode ports fvad_set_mode: select the operating ("aggressiveness")
// mode, 0 ("quality", the default) through 3 ("very aggressive"). A
// higher mode is more restrictive in reporting speech. Returns
// ErrInvalidMode for a mode outside 0..3.
func (v *VAD) SetMode(mode int) error {
	if v.core.SetModeCore(mode) != 0 {
		return ErrInvalidMode
	}
	return nil
}

// SetSampleRate ports fvad_set_sample_rate: set the input rate in Hz.
// Valid values are 8000, 16000, 32000 and 48000 (the default is 8000);
// internally all processing runs at 8000 Hz, higher rates are
// downsampled first. Returns ErrInvalidSampleRate otherwise.
func (v *VAD) SetSampleRate(sampleRate int) error {
	for i, r := range validRates {
		if r*1000 == sampleRate {
			v.rateIdx = i
			return nil
		}
	}
	return ErrInvalidSampleRate
}

// validLength mirrors fvad.c's valid_length.
func validLength(rateIdx, length int) bool {
	samplesPerMs := validRates[rateIdx]
	for _, t := range validFrameTimes {
		if t*samplesPerMs == length {
			return true
		}
	}
	return false
}

// Process ports fvad_process: calculate a VAD decision for one audio
// frame of signed 16-bit samples. Only frames of 10, 20 or 30 ms are
// supported (e.g. 80/160/240 samples at 8 kHz); anything else returns
// ErrInvalidFrameLength. Returns true for active voice.
func (v *VAD) Process(frame []int16) (bool, error) {
	if !validLength(v.rateIdx, len(frame)) {
		return false, ErrInvalidFrameLength
	}

	var rv int
	switch v.rateIdx {
	case 0:
		rv = CalcVad8khz(&v.core, frame)
	case 1:
		rv = CalcVad16khz(&v.core, frame)
	case 2:
		rv = CalcVad32khz(&v.core, frame)
	default:
		rv = CalcVad48khz(&v.core, frame)
	}
	return rv > 0, nil
}

// Core exposes the underlying state block. It exists for the parity
// slices (frame-by-frame state snapshots against the C oracle) and
// must not be touched between Process calls otherwise.
func (v *VAD) Core() *Inst { return &v.core }
