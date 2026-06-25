package adts_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/adts"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReaderFeedsCodecAAC wires the ADTS Reader straight into codec/aac, the
// headline integration: the Reader is a codec/aac.PacketReader and ASC()
// supplies the config codec/aac needs (ADTS carries no out-of-band esds).
//
// The AAC engine (FDK-AAC) is fenced behind the aacfdk build tag, so this test
// is engine-agnostic: in a default build codec/aac.NewDecoder surfaces
// ErrEngineRequiresFDK (and the test asserts the wiring reached the engine);
// under -tags aacfdk it builds a real decoder and the test drains it. Either
// way the ADTS framing + config projection is exercised in the default build.
func TestReaderFeedsCodecAAC(t *testing.T) {
	// Build a small ADTS stream with the Writer (real headers, stand-in AUs).
	var buf bytes.Buffer
	w, err := adts.NewWriter(&buf, 44100, 2)
	require.NoError(t, err)
	for _, n := range []int{200, 256, 64} {
		require.NoError(t, w.WritePacket(bytes.Repeat([]byte{0x01}, n)))
	}

	rd, err := adts.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	asc := rd.ASC()
	require.Equal(t, aaclib.AOTAACLC, asc.ObjectType)
	require.Equal(t, 44100, asc.SampleRate)
	require.Equal(t, 2, asc.Channels)

	dec, err := aaccodec.NewDecoder(rd, asc)
	if err != nil {
		// Default build: the FDK engine is not linked. Assert we reached it via
		// the proper config seam, then we are done — the framing is proven.
		assert.ErrorIs(t, err, aaclib.ErrEngineRequiresFDK)
		return
	}

	// aacfdk build: drain the decoder. The stand-in AUs are not valid AAC, so a
	// decode error is acceptable; what matters is the Reader/ASC seam fed the
	// codec without a separate config record.
	require.Equal(t, 2, dec.Channels())
	require.Equal(t, 44100, dec.SampleRate())
	out := make([]float64, 8192)
	for {
		_, err := dec.Read(out)
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
	}
}
