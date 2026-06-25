package opus

import (
	opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

type encoder struct {
	pw         PacketWriter
	channels   int
	sampleRate int
	enc        opuslib.Encoder
	chunker    *mutations.StreamChunker
	maxPktSize int
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

// emit encodes one full frame and writes the resulting packet.
func (e *encoder) emit(chunk []float64) error {
	pkt, err := e.enc.Encode(chunk, e.maxPktSize)
	if err != nil {
		return err
	}
	return e.pw.WritePacket(pkt)
}

func (e *encoder) Channels() int   { return e.channels }
func (e *encoder) SampleRate() int { return e.sampleRate }
