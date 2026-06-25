package opus

import (
	"io"

	opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

type decoder struct {
	pr         PacketReader
	channels   int
	sampleRate int
	dec        opuslib.Decoder
	frameBuf   []float64 // reusable decode target
	remainder  []float64 // separate buffer for leftover samples
	remOff     int       // read offset into remainder
	remLen     int       // valid sample count in remainder
	eof        bool
}

func (d *decoder) Read(outBuf []float64) (mutations.Audio, error) {
	if len(outBuf) == 0 {
		return d.wrap(outBuf, 0), nil
	}

	buf := outBuf
	written := 0

	// Drain leftover samples from a previous decode.
	if d.remOff < d.remLen {
		n := copy(buf, d.remainder[d.remOff:d.remLen])
		d.remOff += n
		written += n
		buf = buf[n:]
		if len(buf) == 0 {
			return d.wrap(outBuf, written), nil
		}
	}

	if d.eof {
		if written > 0 {
			return d.wrap(outBuf, written), nil
		}
		return d.wrap(outBuf, 0), io.EOF
	}

	// Decode packets until buf is full or we run out of packets.
	for len(buf) > 0 {
		pkt, err := d.pr.ReadPacket()
		if err == io.EOF {
			d.eof = true
			if written > 0 {
				return d.wrap(outBuf, written), nil
			}
			return d.wrap(outBuf, 0), io.EOF
		}
		if err != nil {
			return d.wrap(outBuf, written), err
		}

		samplesPerCh, err := d.dec.Decode(pkt, d.frameBuf)
		if err != nil {
			return d.wrap(outBuf, written), err
		}

		total := samplesPerCh * d.channels
		n := copy(buf, d.frameBuf[:total])
		written += n
		buf = buf[n:]

		// Store any leftover decoded samples.
		if n < total {
			leftover := total - n
			copy(d.remainder[:leftover], d.frameBuf[n:total])
			d.remOff = 0
			d.remLen = leftover
			break
		}
	}

	return d.wrap(outBuf, written), nil
}

func (d *decoder) wrap(buf []float64, n int) mutations.Audio {
	return mutations.Audio{Data: buf[:n], SampleRate: d.sampleRate, Channels: d.channels}
}

func (d *decoder) Channels() int   { return d.channels }
func (d *decoder) SampleRate() int { return d.sampleRate }
