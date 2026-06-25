// Package codec defines shared interfaces for streaming audio encoding and
// decoding. Implementations are in sub-packages (e.g., codec/pcm, codec/opus).
//
// All audio data is represented as interleaved float64 samples normalized to
// [-1.0, 1.0]. For stereo: [L0, R0, L1, R1, ...].
//
// Decoder and Encoder are not safe for concurrent use.
package codec

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Decoder reads interleaved float64 samples from an encoded source.
//
// Read fills the caller's buffer and returns an Audio whose Data
// field aliases buf[:n] — so the decoded samples live in the
// caller-owned buffer (no per-call allocation) while format metadata
// rides along in the returned value. A partial read (len(Audio.Data)
// < len(buf), err == io.EOF) is valid and the returned samples must
// be consumed.
type Decoder interface {
	// Read decodes samples into buf and returns an Audio aliasing
	// the filled portion. Returns io.EOF when the source is
	// exhausted; a partial read (len(audio.Data) > 0 alongside
	// io.EOF) is valid.
	Read(buf []float64) (audio mutations.Audio, err error)

	// Channels returns the number of audio channels.
	Channels() int

	// SampleRate returns the sample rate in Hz.
	SampleRate() int
}

// Encoder writes audio to an encoded destination.
//
// Write takes a mutations.Audio so the encoder can verify that the
// caller's buffer matches the format the encoder was constructed for;
// supplying an Audio whose SampleRate or Channels differs from the
// Encoder's returns an error without consuming any samples.
type Encoder interface {
	// Write encodes audio.Data to the underlying destination and
	// returns the number of samples consumed (total samples, not
	// frames). A short write (n < len(audio.Data)) is valid; retry
	// with audio.Data[n:] wrapped in a fresh Audio to continue.
	Write(audio mutations.Audio) (n int, err error)

	// Channels returns the number of audio channels the encoder was
	// constructed for.
	Channels() int

	// SampleRate returns the sample rate the encoder was constructed
	// for in Hz.
	SampleRate() int

	// Close flushes any buffered data and releases resources.
	Close() error
}

// ReadFull reads exactly len(buf) samples from dec, blocking until
// the buffer is full or an error occurs. It returns an Audio
// aliasing the full buf on success. If dec returns io.EOF before
// filling buf, the error is io.ErrUnexpectedEOF (unless zero samples
// were read, in which case it returns io.EOF).
func ReadFull(dec Decoder, buf []float64) (mutations.Audio, error) {
	n := 0
	var err error
	for n < len(buf) && err == nil {
		var got mutations.Audio
		got, err = dec.Read(buf[n:])
		n += len(got.Data)
	}
	if n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return mutations.Audio{
		Data:       buf[:n],
		SampleRate: dec.SampleRate(),
		Channels:   dec.Channels(),
	}, err
}
