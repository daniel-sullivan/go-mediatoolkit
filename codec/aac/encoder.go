package aac

import (
	aaclib "go-mediatoolkit/libraries/aac"
	"go-mediatoolkit/mutations"
)

// encoder is the streaming [codec.Encoder] adapter over a
// [libraries/aac.Encoder]. It buffers incoming samples with a
// [mutations.StreamChunker] and emits one AAC access unit per full frame —
// mirroring codec/opus/encoder.go.
type encoder struct {
	pw         PacketWriter
	channels   int
	sampleRate int
	enc        aaclib.Encoder
	chunker    *mutations.StreamChunker
}

func (e *encoder) Write(audio mutations.Audio) (int, error) {
	if audio.SampleRate != e.sampleRate || audio.Channels != e.channels {
		return 0, ErrFormatMismatch
	}
	return e.chunker.Write(audio.Data, e.emit)
}

// Close flushes any remaining buffered samples. A partial frame is padded
// with silence (zeros) before encoding.
func (e *encoder) Close() error {
	return e.chunker.Flush(e.emit)
}

// emit encodes one full frame and writes the resulting access unit.
func (e *encoder) emit(chunk []float64) error {
	pkt, err := e.enc.Encode(chunk)
	if err != nil {
		return err
	}
	return e.pw.WritePacket(pkt)
}

func (e *encoder) Channels() int   { return e.channels }
func (e *encoder) SampleRate() int { return e.sampleRate }
