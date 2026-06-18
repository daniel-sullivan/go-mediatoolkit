// Package resample provides sample rate conversion for audio data.
//
// It is a pure-Go port of libsamplerate (https://github.com/libsndfile/libsamplerate),
// supporting five conversion algorithms at different quality/speed tradeoffs.
//
// A Converter is created with New, called repeatedly with Process for streaming
// conversion, and released with Close. For one-shot conversion, use Simple.
//
// All audio data is represented as interleaved float64 samples. For stereo audio,
// samples alternate left/right: [L0, R0, L1, R1, ...]. A "frame" is one sample
// per channel (e.g., one L+R pair for stereo).
//
// A single Converter is not safe for concurrent use. Use Clone to create
// independent copies for concurrent processing.
//go:generate go run gen_coeffs.go

package resample

import (
	"errors"
	"math"
	"sync"
)

// MaxRatio is the maximum supported conversion ratio (output rate / input rate).
// The minimum is 1/MaxRatio.
const MaxRatio = 256

// minRatioDiff is the threshold below which ratio changes are ignored.
const minRatioDiff = 1e-20

// ConverterType identifies the resampling algorithm.
type ConverterType int

const (
	SincBestQuality   ConverterType = iota // Band-limited sinc, best quality (144 dB SNR)
	SincMediumQuality                      // Band-limited sinc, medium quality (121 dB SNR)
	SincFastest                            // Band-limited sinc, fastest (97 dB SNR)
	ZeroOrderHold                          // Zero-order hold (sample-and-hold)
	Linear                                 // Linear interpolation
)

// String returns the name of the converter type.
func (ct ConverterType) String() string {
	switch ct {
	case SincBestQuality:
		return "Best Quality Sinc Interpolator"
	case SincMediumQuality:
		return "Medium Quality Sinc Interpolator"
	case SincFastest:
		return "Fastest Sinc Interpolator"
	case ZeroOrderHold:
		return "ZOH Interpolator"
	case Linear:
		return "Linear Interpolator"
	default:
		return "Unknown"
	}
}

// Ratio represents a sample rate conversion ratio as input and output sample rates.
type Ratio struct {
	InputRate  int
	OutputRate int
}

// Float64 returns the conversion ratio as a float64 (outputRate / inputRate).
func (r Ratio) Float64() float64 {
	return float64(r.OutputRate) / float64(r.InputRate)
}

// Data carries input/output buffers and metadata for a single Process call.
//
// The caller provides DataIn (input samples) and DataOut (output buffer).
// After Process returns, InputFramesUsed and OutputFramesGen report how many
// frames were consumed and produced. Remaining input can be passed in the
// next call. Set EndOfInput on the final buffer to flush the converter.
type Data struct {
	DataIn  []float64 // Input samples, interleaved by channel.
	DataOut []float64 // Output buffer, interleaved by channel.

	InputFramesUsed int // Frames consumed from DataIn (set by Process).
	OutputFramesGen int // Frames written to DataOut (set by Process).

	EndOfInput bool  // True on the final input buffer.
	Ratio      Ratio // Conversion ratio.
}

// Converter performs streaming sample rate conversion.
type Converter interface {
	// Process converts audio from DataIn to DataOut. Call repeatedly for
	// streaming. The converter maintains internal state between calls.
	Process(d *Data) error

	// Reset clears internal state. The converter can be reused for a new stream.
	Reset()

	// Clone returns an independent deep copy preserving all internal state.
	Clone() Converter

	// Close releases internal resources. Do not use the converter after Close.
	Close()

	// Channels returns the channel count this converter was created for.
	Channels() int

	// SetRatio updates the conversion ratio for a step change between calls.
	SetRatio(ratio Ratio) error
}

var (
	ErrBadSrcRatio      = errors.New("resample: ratio must be in [1/256, 256]")
	ErrBadChannelCount  = errors.New("resample: channel count must be >= 1")
	ErrBadConverterType = errors.New("resample: unknown converter type")
	ErrBadData          = errors.New("resample: nil data")
	ErrBadInternalState = errors.New("resample: corrupted internal state")
)

// New creates a Converter of the specified type for the given channel count.
func New(converterType ConverterType, channels int) (Converter, error) {
	if channels < 1 {
		return nil, ErrBadChannelCount
	}
	switch converterType {
	case ZeroOrderHold:
		return newZOH(channels), nil
	case Linear:
		return newLinear(channels), nil
	case SincFastest, SincMediumQuality, SincBestQuality:
		return newSinc(converterType, channels)
	default:
		return nil, ErrBadConverterType
	}
}

// simplePool pools scratch output buffers used by Simple.
var simplePool = sync.Pool{
	New: func() any {
		// Start with a reasonable default; will grow as needed.
		buf := make([]float64, 0, 8192)
		return &buf
	},
}

// Simple performs a one-shot conversion of the entire input buffer.
// The returned slice is newly allocated and owned by the caller.
func Simple(in []float64, converterType ConverterType, channels int, ratio Ratio) ([]float64, error) {
	if channels < 1 {
		return nil, ErrBadChannelCount
	}
	r := ratio.Float64()
	if !isValidRatioF(r) {
		return nil, ErrBadSrcRatio
	}

	inputFrames := len(in) / channels
	outputFrames := int(math.Ceil(float64(inputFrames)*r)) + 32

	c, err := New(converterType, channels)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	// Get a scratch buffer from the pool.
	bufPtr := simplePool.Get().(*[]float64)
	scratch := *bufPtr
	needed := outputFrames * channels
	if cap(scratch) < needed {
		scratch = make([]float64, needed)
	} else {
		scratch = scratch[:needed]
	}

	d := &Data{
		DataIn:     in,
		DataOut:    scratch,
		EndOfInput: true,
		Ratio:      ratio,
	}
	if err := c.Process(d); err != nil {
		*bufPtr = scratch
		simplePool.Put(bufPtr)
		return nil, err
	}

	resultLen := d.OutputFramesGen * channels
	result := make([]float64, resultLen)
	copy(result, scratch[:resultLen])

	*bufPtr = scratch
	simplePool.Put(bufPtr)

	return result, nil
}

// IsValidRatio reports whether the ratio is within the supported range.
func IsValidRatio(ratio Ratio) bool {
	r := ratio.Float64()
	return r >= (1.0/MaxRatio) && r <= MaxRatio
}

// isValidRatioF reports whether a float64 ratio is within the supported range.
func isValidRatioF(r float64) bool {
	return r >= (1.0/MaxRatio) && r <= MaxRatio
}

// validateData checks common preconditions for Process.
func validateData(d *Data, channels int) error {
	if d == nil {
		return ErrBadData
	}
	if !isValidRatioF(d.Ratio.Float64()) {
		return ErrBadSrcRatio
	}
	d.InputFramesUsed = 0
	d.OutputFramesGen = 0
	return nil
}
