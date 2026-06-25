package mixer

import (
	"io"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

// adaptSource wraps src with rate and channel adapters as needed to
// match targetRate and targetChannels. The adapter chain is:
// src → [resampler] → [channel adapter]. Order matters: resampling is
// cheaper on the source's native channel count, so we resample first.
//
// Adapter insertion is logged so a glance at the example output makes
// rate/channel mismatches obvious — silent resampling has historically
// been the source of speed-of-playback bugs that take a long time to
// diagnose.
func adaptSource(src timeline.Source, targetRate, targetChannels int) (timeline.Source, error) {
	if src.SampleRate() != targetRate {
		log.Printf("mixer: inserting resampler %dHz → %dHz on track", src.SampleRate(), targetRate)
		src = newResampledSource(src, targetRate)
	}
	if src.Channels() != targetChannels {
		log.Printf("mixer: inserting channel adapter %dch → %dch on track", src.Channels(), targetChannels)
		adapted, err := newChannelAdapter(src, targetChannels)
		if err != nil {
			return nil, err
		}
		src = adapted
	}
	return src, nil
}

// newChannelAdapter wraps src to output targetChannels. Only
// mono↔stereo is supported; other combinations return
// ErrUnsupportedChannels.
func newChannelAdapter(src timeline.Source, targetChannels int) (timeline.Source, error) {
	switch {
	case src.Channels() == targetChannels:
		return src, nil
	case src.Channels() == 1 && targetChannels == 2:
		return &monoToStereo{src: src}, nil
	case src.Channels() == 2 && targetChannels == 1:
		return &stereoToMono{src: src}, nil
	default:
		return nil, ErrUnsupportedChannels
	}
}

// monoToStereo duplicates each mono sample to both L and R, delegating
// the per-frame copy to mutations.UpmixMonoToStereo.
type monoToStereo struct {
	src     timeline.Source
	scratch []float64
}

func (a *monoToStereo) Pull(dst []float64) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	a.scratch = mutations.ResizeScratch(a.scratch, frames)
	n, err := a.src.Pull(a.scratch)
	return mutations.UpmixMonoToStereo(a.scratch[:n], dst), err
}

func (a *monoToStereo) SampleRate() int         { return a.src.SampleRate() }
func (a *monoToStereo) Channels() int           { return 2 }
func (a *monoToStereo) Duration() time.Duration { return a.src.Duration() }
func (a *monoToStereo) Live() bool              { return a.src.Live() }

// stereoToMono averages L and R into a mono sample, delegating to
// mutations.DownmixStereoToMono.
type stereoToMono struct {
	src     timeline.Source
	scratch []float64
}

func (a *stereoToMono) Pull(dst []float64) (int, error) {
	frames := len(dst)
	if frames == 0 {
		return 0, nil
	}
	a.scratch = mutations.ResizeScratch(a.scratch, frames*2)
	n, err := a.src.Pull(a.scratch)
	return mutations.DownmixStereoToMono(a.scratch[:n], dst), err
}

func (a *stereoToMono) SampleRate() int         { return a.src.SampleRate() }
func (a *stereoToMono) Channels() int           { return 1 }
func (a *stereoToMono) Duration() time.Duration { return a.src.Duration() }
func (a *stereoToMono) Live() bool              { return a.src.Live() }

// resampledSource wraps a Source with a resample.Converter to produce
// output at a different sample rate. It buffers converter output
// between Pull calls when the caller's dst is smaller than what the
// converter produces.
type resampledSource struct {
	src    timeline.Source
	conv   resample.Converter
	ratio  resample.Ratio
	inBuf  []float64
	outBuf []float64
	// [outHead:outLen] is the buffered converter output waiting to
	// be drained to callers.
	outHead int
	outLen  int
	srcEOF  bool
	flushed bool
}

// resampleInputChunk is how many input samples the resampler asks
// the source for per refill. Tuned for live sources: a 4096-sample
// fetch on a 48kHz mic translates to ~85ms between drains, which
// would force any reasonable input ring to either be very large or
// drop samples during the gap. 1024 samples (~21ms at 48kHz) is a
// few mixer chunks of input — enough to amortise per-Process
// overhead, small enough that the mic ring drains evenly.
const resampleInputChunk = 1024

func newResampledSource(src timeline.Source, targetRate int) *resampledSource {
	// SincFastest gives near-linear-quality output with significantly
	// better antialiasing than Linear, at a cost the mixer can absorb
	// at typical chunk sizes. The round-to-even position-advance bug
	// that previously made sinc unusable was fixed in resample/sinc.go;
	// the libsamplerate parity matrix (resample/cgo_test.go) covers
	// all converter × ratio combinations.
	conv, _ := resample.New(resample.SincFastest, src.Channels())
	return &resampledSource{
		src:   src,
		conv:  conv,
		ratio: resample.Ratio{InputRate: src.SampleRate(), OutputRate: targetRate},
	}
}

func (r *resampledSource) Pull(dst []float64) (int, error) {
	written := 0
	for written < len(dst) {
		// 1. Drain any previously-buffered output into dst.
		if r.outHead < r.outLen {
			n := copy(dst[written:], r.outBuf[r.outHead:r.outLen])
			r.outHead += n
			written += n
			if r.outHead == r.outLen {
				r.outHead, r.outLen = 0, 0
			}
			continue
		}
		if r.flushed {
			break
		}

		// 2. Top up inBuf, preserving any input the converter didn't
		// consume on the previous call. Without this the source
		// playhead would advance faster than the resampler actually
		// used and audio would play back at the wrong speed.
		if len(r.inBuf) < resampleInputChunk && !r.srcEOF {
			oldLen := len(r.inBuf)
			if cap(r.inBuf) >= resampleInputChunk {
				r.inBuf = r.inBuf[:resampleInputChunk]
			} else {
				newBuf := make([]float64, resampleInputChunk)
				copy(newBuf, r.inBuf)
				r.inBuf = newBuf
			}
			nIn, err := r.src.Pull(r.inBuf[oldLen:])
			r.inBuf = r.inBuf[:oldLen+nIn]
			if err == io.EOF {
				r.srcEOF = true
			} else if err != nil {
				return written, err
			}
			// Live source backpressure: nothing arrived. Returning
			// what we have lets the mixer treat the unfilled tail as
			// silence and try again later.
			if nIn == 0 && !r.srcEOF {
				break
			}
		}

		// 3. Resample. Size outBuf so the converter consumes as much
		// of inBuf as it can; surplus output buffers for next call.
		maxOut := len(r.inBuf)*r.ratio.OutputRate/r.ratio.InputRate + 64
		if maxOut < 64 {
			maxOut = 64
		}
		r.outBuf = mutations.ResizeScratch(r.outBuf, maxOut)
		d := &resample.Data{
			DataIn:     r.inBuf,
			DataOut:    r.outBuf,
			EndOfInput: r.srcEOF,
			Ratio:      r.ratio,
		}
		if err := r.conv.Process(d); err != nil {
			return written, err
		}
		consumed := d.InputFramesUsed * r.src.Channels()
		r.inBuf = r.inBuf[consumed:]
		r.outLen = d.OutputFramesGen * r.src.Channels()
		r.outHead = 0

		if r.srcEOF && r.outLen == 0 && len(r.inBuf) == 0 {
			r.flushed = true
			break
		}
		if r.outLen == 0 {
			// Converter is buffering internally without producing yet
			// — only happens with stateful (sinc) converters at start
			// of stream. Return whatever we have so far.
			break
		}
	}
	if written == 0 && r.flushed {
		return 0, io.EOF
	}
	return written, nil
}

func (r *resampledSource) SampleRate() int { return r.ratio.OutputRate }
func (r *resampledSource) Channels() int   { return r.src.Channels() }
func (r *resampledSource) Duration() time.Duration {
	d := r.src.Duration()
	if d < 0 {
		return d
	}
	return d * time.Duration(r.ratio.OutputRate) / time.Duration(r.ratio.InputRate)
}
func (r *resampledSource) Live() bool { return r.src.Live() }
