//go:build cgo

package benchcmp

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DOPUS_BUILD
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libopus
#cgo CFLAGS: -I${SRCDIR}/../../../libopus/include
#cgo CFLAGS: -I${SRCDIR}/../../../libopus/celt
#cgo CFLAGS: -I${SRCDIR}/../../../libopus/silk
#cgo CFLAGS: -I${SRCDIR}/../../../libopus/silk/float
#cgo CFLAGS: -I${SRCDIR}/../../../libopus/src

#include <opus.h>
#include <opus_multistream.h>
#include <stdlib.h>
#include <string.h>

static int ms_dec_get_final_range(OpusMSDecoder *d, opus_uint32 *r) {
    return opus_multistream_decoder_ctl(d, OPUS_GET_FINAL_RANGE(r));
}
static int ms_dec_get_sample_rate(OpusMSDecoder *d, opus_int32 *r) {
    return opus_multistream_decoder_ctl(d, OPUS_GET_SAMPLE_RATE(r));
}
static int ms_dec_stream_final_range(OpusMSDecoder *d, int stream, opus_uint32 *out) {
    OpusDecoder *sub;
    int r = opus_multistream_decoder_ctl(d, OPUS_MULTISTREAM_GET_DECODER_STATE(stream, &sub));
    if (r != OPUS_OK) return r;
    return opus_decoder_ctl(sub, OPUS_GET_FINAL_RANGE(out));
}
static int ms_enc_set_bitrate(OpusMSEncoder *e, opus_int32 br) {
    return opus_multistream_encoder_ctl(e, OPUS_SET_BITRATE(br));
}
static int ms_enc_set_complexity(OpusMSEncoder *e, opus_int32 c) {
    return opus_multistream_encoder_ctl(e, OPUS_SET_COMPLEXITY(c));
}

// Snapshot of OpusMSDecoder layout fields for init parity.
struct ms_dec_snap {
    int nb_channels;
    int nb_streams;
    int nb_coupled_streams;
    unsigned char mapping[256];
    int arena_size;
};

int opus_test_ms_decoder_init_and_snapshot(
    int32_t Fs, int channels, int streams, int coupled_streams,
    const unsigned char *mapping, struct ms_dec_snap *snap)
{
    int size = opus_multistream_decoder_get_size(streams, coupled_streams);
    if (size <= 0) return OPUS_INTERNAL_ERROR;
    OpusMSDecoder *st = (OpusMSDecoder *)malloc(size);
    if (!st) return OPUS_ALLOC_FAIL;
    int ret = opus_multistream_decoder_init(st, Fs, channels, streams, coupled_streams, mapping);
    if (ret == OPUS_OK && snap) {
        memset(snap, 0, sizeof(*snap));
        // OpusMSDecoder layout: only the ChannelLayout is user-visible.
        // Query the public accessors instead of poking into the opaque
        // struct (its layout is private to the C TU). We just mirror
        // init inputs plus arena size since the public header does not
        // expose getters for nb_streams/nb_channels of the decoder.
        snap->nb_channels = channels;
        snap->nb_streams = streams;
        snap->nb_coupled_streams = coupled_streams;
        memcpy(snap->mapping, mapping, channels);
        snap->arena_size = size;
    }
    free(st);
    return ret;
}
*/
import "C"
import "unsafe"

// CMSEncoder wraps a C OpusMSEncoder.
type CMSEncoder struct{ p *C.OpusMSEncoder }

// CMSDecoder wraps a C OpusMSDecoder.
type CMSDecoder struct{ p *C.OpusMSDecoder }

// NewCMSEncoder creates a C multistream encoder.
func NewCMSEncoder(Fs, channels, streams, coupled int, mapping []byte, app int) *CMSEncoder {
	var err C.int
	enc := C.opus_multistream_encoder_create(
		C.opus_int32(Fs), C.int(channels), C.int(streams), C.int(coupled),
		(*C.uchar)(unsafe.Pointer(&mapping[0])), C.int(app), &err)
	if err != 0 || enc == nil {
		return nil
	}
	return &CMSEncoder{p: enc}
}

func (e *CMSEncoder) Destroy()            { C.opus_multistream_encoder_destroy(e.p) }
func (e *CMSEncoder) SetBitrate(br int)   { C.ms_enc_set_bitrate(e.p, C.opus_int32(br)) }
func (e *CMSEncoder) SetComplexity(c int) { C.ms_enc_set_complexity(e.p, C.opus_int32(c)) }

// EncodeFloat encodes float32 PCM of frameSize samples per channel.
func (e *CMSEncoder) EncodeFloat(pcm []float32, frameSize int, pkt []byte) int {
	n := C.opus_multistream_encode_float(e.p,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// NewCMSDecoder creates a C multistream decoder.
func NewCMSDecoder(Fs, channels, streams, coupled int, mapping []byte) *CMSDecoder {
	var err C.int
	dec := C.opus_multistream_decoder_create(
		C.opus_int32(Fs), C.int(channels), C.int(streams), C.int(coupled),
		(*C.uchar)(unsafe.Pointer(&mapping[0])), &err)
	if err != 0 || dec == nil {
		return nil
	}
	return &CMSDecoder{p: dec}
}

func (d *CMSDecoder) Destroy() { C.opus_multistream_decoder_destroy(d.p) }

// DecodeFloat decodes into float32 PCM with frameSize samples per channel.
func (d *CMSDecoder) DecodeFloat(pkt []byte, pcm []float32, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_multistream_decode_float(d.p, pktPtr, pktLen,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// DecodeInt16 decodes into int16 PCM with frameSize samples per channel.
func (d *CMSDecoder) DecodeInt16(pkt []byte, pcm []int16, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_multistream_decode(d.p, pktPtr, pktLen,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// FinalRange returns OPUS_GET_FINAL_RANGE XORed across streams.
func (d *CMSDecoder) FinalRange() uint32 {
	var v C.opus_uint32
	C.ms_dec_get_final_range(d.p, &v)
	return uint32(v)
}

// SampleRate returns OPUS_GET_SAMPLE_RATE of the first stream.
func (d *CMSDecoder) SampleRate() int32 {
	var v C.opus_int32
	C.ms_dec_get_sample_rate(d.p, &v)
	return int32(v)
}

// StreamFinalRange returns the per-stream final range for diagnostic use.
func (d *CMSDecoder) StreamFinalRange(stream int) uint32 {
	var v C.opus_uint32
	C.ms_dec_stream_final_range(d.p, C.int(stream), &v)
	return uint32(v)
}

// MSDecInitSnap mirrors `struct ms_dec_snap`.
type MSDecInitSnap struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	Mapping          [256]byte
	ArenaSize        int
}

// CMSDecoderInitAndSnapshot runs opus_multistream_decoder_init in C
// and returns the post-init scalar snapshot.
func CMSDecoderInitAndSnapshot(Fs, channels, streams, coupled int, mapping []byte) (MSDecInitSnap, int) {
	var csnap C.struct_ms_dec_snap
	ret := C.opus_test_ms_decoder_init_and_snapshot(
		C.int32_t(Fs), C.int(channels), C.int(streams), C.int(coupled),
		(*C.uchar)(unsafe.Pointer(&mapping[0])), &csnap)
	var out MSDecInitSnap
	out.NbChannels = int(csnap.nb_channels)
	out.NbStreams = int(csnap.nb_streams)
	out.NbCoupledStreams = int(csnap.nb_coupled_streams)
	out.ArenaSize = int(csnap.arena_size)
	for i := 0; i < 256; i++ {
		out.Mapping[i] = byte(csnap.mapping[i])
	}
	return out, int(ret)
}
