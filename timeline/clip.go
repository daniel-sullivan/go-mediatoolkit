package timeline

import (
	"io"
	"time"

	"go-mediatoolkit/codec"
	"go-mediatoolkit/mutations"
)

// CachedClip is fully decoded PCM held in memory. It is a library
// object, not itself a Source: call Playhead to obtain a Source with
// its own cursor. Many Playheads may read from the same CachedClip
// concurrently; the underlying sample buffer is immutable after
// construction.
//
// Use LoadClip for short, frequently-replayed audio (game SFX, ambient
// loops, UI stingers). For long-form material where decode cost is
// amortised over playback, prefer OpenClip which streams from the
// decoder on demand.
type CachedClip struct {
	samples    []float64
	sampleRate int
	channels   int
}

// LoadClip drains dec fully and returns a CachedClip holding the
// decoded samples. dec is consumed; callers typically close its
// underlying reader afterwards.
func LoadClip(dec codec.Decoder) (*CachedClip, error) {
	if dec.SampleRate() <= 0 {
		return nil, ErrBadSampleRate
	}
	if dec.Channels() <= 0 {
		return nil, ErrBadChannels
	}
	var samples []float64
	buf := make([]float64, 4096)
	for {
		got, err := dec.Read(buf)
		if len(got.Data) > 0 {
			samples = append(samples, got.Data...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return &CachedClip{
		samples:    samples,
		sampleRate: dec.SampleRate(),
		channels:   dec.Channels(),
	}, nil
}

// LoadClipFromAudio wraps an already-decoded mutations.Audio buffer
// in a CachedClip. The input Data slice is copied so the caller
// retains ownership of its buffer.
func LoadClipFromAudio(a mutations.Audio) (*CachedClip, error) {
	if a.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if a.Channels <= 0 {
		return nil, ErrBadChannels
	}
	cp := make([]float64, len(a.Data))
	copy(cp, a.Data)
	return &CachedClip{samples: cp, sampleRate: a.SampleRate, channels: a.Channels}, nil
}

// LoadClipFromPCM is a lower-level alternative to LoadClipFromAudio
// for callers that already have a bare []float64 and format ints.
// Prefer LoadClipFromAudio in new code.
func LoadClipFromPCM(samples []float64, sampleRate, channels int) (*CachedClip, error) {
	return LoadClipFromAudio(mutations.Audio{Data: samples, SampleRate: sampleRate, Channels: channels})
}

// MustCacheClip is the panic-on-error convenience wrapper around
// LoadClipFromAudio. Intended for example / init-time code where a
// generator or decoder is known to produce well-formed Audio; not
// suitable for runtime paths where a caller-supplied buffer might
// be invalid.
func MustCacheClip(audio mutations.Audio) *CachedClip {
	c, err := LoadClipFromAudio(audio)
	if err != nil {
		panic(err)
	}
	return c
}

// Audio returns a mutations.Audio view of this clip's decoded samples.
// The returned Audio aliases the clip's internal buffer — do not
// mutate. Use Clone on the returned value if you need to modify.
func (c *CachedClip) Audio() mutations.Audio {
	return mutations.Audio{Data: c.samples, SampleRate: c.sampleRate, Channels: c.channels}
}

// SampleRate reports the clip's sample rate.
func (c *CachedClip) SampleRate() int { return c.sampleRate }

// Channels reports the clip's channel count.
func (c *CachedClip) Channels() int { return c.channels }

// Duration reports the clip's total length.
func (c *CachedClip) Duration() time.Duration {
	frames := int64(len(c.samples) / c.channels)
	return mutations.FramesToDuration(frames, c.sampleRate)
}

// Frames reports the clip's length in frames.
func (c *CachedClip) Frames() int64 {
	return int64(len(c.samples) / c.channels)
}

// Playhead returns a new Source that reads from the start of the clip.
// Each Playhead maintains its own independent cursor.
func (c *CachedClip) Playhead() Source {
	return &cachedPlayhead{clip: c}
}

type cachedPlayhead struct {
	clip   *CachedClip
	cursor int // sample index (not frame) into clip.samples
}

func (p *cachedPlayhead) Pull(dst []float64) (int, error) {
	remaining := len(p.clip.samples) - p.cursor
	if remaining <= 0 {
		return 0, io.EOF
	}
	n := len(dst)
	if n > remaining {
		n = remaining
	}
	copy(dst[:n], p.clip.samples[p.cursor:p.cursor+n])
	p.cursor += n
	if p.cursor >= len(p.clip.samples) {
		return n, io.EOF
	}
	return n, nil
}

func (p *cachedPlayhead) SampleRate() int         { return p.clip.sampleRate }
func (p *cachedPlayhead) Channels() int           { return p.clip.channels }
func (p *cachedPlayhead) Duration() time.Duration { return p.clip.Duration() }
func (p *cachedPlayhead) Live() bool              { return false }

// Seek moves the playhead by frames. Positive values fast-forward;
// negative values rewind. Seeking past the end clamps to the end and
// returns io.EOF; seeking past the start clamps to frame 0 and returns
// nil.
func (p *cachedPlayhead) Seek(frames int64) error {
	delta := int(frames) * p.clip.channels
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
		return nil
	}
	if p.cursor >= len(p.clip.samples) {
		p.cursor = len(p.clip.samples)
		return io.EOF
	}
	return nil
}

// StreamingClip is a single-use Source backed by a codec.Decoder. It
// decodes on demand and is discarded after playback; use LoadClip if
// the same audio needs to be replayed.
type StreamingClip struct {
	dec      codec.Decoder
	duration time.Duration
}

// OpenClip wraps dec as a single-use streaming Source. Pass -1 for
// duration if the total length is unknown.
func OpenClip(dec codec.Decoder, duration time.Duration) (*StreamingClip, error) {
	if dec == nil {
		return nil, ErrNilSource
	}
	if dec.SampleRate() <= 0 {
		return nil, ErrBadSampleRate
	}
	if dec.Channels() <= 0 {
		return nil, ErrBadChannels
	}
	return &StreamingClip{dec: dec, duration: duration}, nil
}

func (s *StreamingClip) Pull(dst []float64) (int, error) {
	got, err := s.dec.Read(dst)
	return len(got.Data), err
}

func (s *StreamingClip) SampleRate() int         { return s.dec.SampleRate() }
func (s *StreamingClip) Channels() int           { return s.dec.Channels() }
func (s *StreamingClip) Duration() time.Duration { return s.duration }
func (s *StreamingClip) Live() bool              { return false }
