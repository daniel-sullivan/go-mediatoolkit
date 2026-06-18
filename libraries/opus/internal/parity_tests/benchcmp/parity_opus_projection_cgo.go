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
#include <opus_projection.h>
#include <stdlib.h>
#include <string.h>

static int cproj_enc_set_bitrate(OpusProjectionEncoder *e, opus_int32 br) {
    return opus_projection_encoder_ctl(e, OPUS_SET_BITRATE(br));
}
static int cproj_enc_set_complexity(OpusProjectionEncoder *e, opus_int32 c) {
    return opus_projection_encoder_ctl(e, OPUS_SET_COMPLEXITY(c));
}
static int cproj_enc_get_demix_size(OpusProjectionEncoder *e, opus_int32 *sz) {
    return opus_projection_encoder_ctl(e, OPUS_PROJECTION_GET_DEMIXING_MATRIX_SIZE(sz));
}
static int cproj_enc_get_demix_gain(OpusProjectionEncoder *e, opus_int32 *g) {
    return opus_projection_encoder_ctl(e, OPUS_PROJECTION_GET_DEMIXING_MATRIX_GAIN(g));
}
static int cproj_enc_get_demix(OpusProjectionEncoder *e, unsigned char *buf, opus_int32 sz) {
    return opus_projection_encoder_ctl(e, OPUS_PROJECTION_GET_DEMIXING_MATRIX(buf, sz));
}

// Snapshot of scalar projection-encoder state reachable via public API.
struct proj_enc_snap {
    int nb_channels;
    int nb_streams;
    int nb_coupled_streams;
    opus_int32 demix_size;
    opus_int32 demix_gain;
    opus_int32 arena_size;
};

int opus_test_proj_enc_init_and_snapshot(
    opus_int32 Fs, int channels, int mapping_family, int application,
    int *out_streams, int *out_coupled,
    struct proj_enc_snap *snap,
    unsigned char *out_demix, opus_int32 out_demix_cap)
{
    opus_int32 size = opus_projection_ambisonics_encoder_get_size(channels, mapping_family);
    if (size <= 0) return OPUS_INTERNAL_ERROR;

    int streams = 0, coupled = 0;
    int err = 0;
    OpusProjectionEncoder *st = opus_projection_ambisonics_encoder_create(
        Fs, channels, mapping_family, &streams, &coupled, application, &err);
    if (err != OPUS_OK || st == NULL) return err;

    if (out_streams) *out_streams = streams;
    if (out_coupled) *out_coupled = coupled;
    if (snap) {
        memset(snap, 0, sizeof(*snap));
        snap->nb_channels = channels;
        snap->nb_streams = streams;
        snap->nb_coupled_streams = coupled;
        opus_int32 sz = 0;
        cproj_enc_get_demix_size(st, &sz);
        snap->demix_size = sz;
        opus_int32 g = 0;
        cproj_enc_get_demix_gain(st, &g);
        snap->demix_gain = g;
        snap->arena_size = size;
        if (out_demix && out_demix_cap >= sz) {
            cproj_enc_get_demix(st, out_demix, sz);
        }
    }
    opus_projection_encoder_destroy(st);
    return OPUS_OK;
}
*/
import "C"
import "unsafe"

// CProjEncoder wraps a C OpusProjectionEncoder.
type CProjEncoder struct {
	p        *C.OpusProjectionEncoder
	streams  int
	coupled  int
	channels int
}

// NewCProjEncoder creates an ambisonics projection encoder.
func NewCProjEncoder(Fs, channels, mappingFamily, application int) (*CProjEncoder, []byte) {
	var streams, coupled C.int
	var err C.int
	enc := C.opus_projection_ambisonics_encoder_create(
		C.opus_int32(Fs), C.int(channels), C.int(mappingFamily),
		&streams, &coupled, C.int(application), &err)
	if err != 0 || enc == nil {
		return nil, nil
	}
	// Fetch the demixing matrix bytes.
	var sz C.opus_int32
	C.cproj_enc_get_demix_size(enc, &sz)
	buf := make([]byte, int(sz))
	if sz > 0 {
		C.cproj_enc_get_demix(enc, (*C.uchar)(unsafe.Pointer(&buf[0])), sz)
	}
	return &CProjEncoder{p: enc, streams: int(streams), coupled: int(coupled), channels: channels}, buf
}

// Destroy releases the C encoder.
func (e *CProjEncoder) Destroy()            { C.opus_projection_encoder_destroy(e.p) }
func (e *CProjEncoder) SetBitrate(br int)   { C.cproj_enc_set_bitrate(e.p, C.opus_int32(br)) }
func (e *CProjEncoder) SetComplexity(c int) { C.cproj_enc_set_complexity(e.p, C.opus_int32(c)) }
func (e *CProjEncoder) Streams() int        { return e.streams }
func (e *CProjEncoder) Coupled() int        { return e.coupled }
func (e *CProjEncoder) Channels() int       { return e.channels }

// EncodeFloat encodes frameSize samples of interleaved float32 PCM.
func (e *CProjEncoder) EncodeFloat(pcm []float32, frameSize int, pkt []byte) int {
	n := C.opus_projection_encode_float(e.p,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// EncodeInt16 encodes frameSize samples of interleaved int16 PCM.
func (e *CProjEncoder) EncodeInt16(pcm []int16, frameSize int, pkt []byte) int {
	n := C.opus_projection_encode(e.p,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// CProjDecoder wraps a C OpusProjectionDecoder.
type CProjDecoder struct {
	p        *C.OpusProjectionDecoder
	channels int
}

// NewCProjDecoder creates a projection decoder given the encoder-side demixing matrix bytes.
func NewCProjDecoder(Fs, channels, streams, coupled int, demixingMatrix []byte) *CProjDecoder {
	var err C.int
	dec := C.opus_projection_decoder_create(
		C.opus_int32(Fs), C.int(channels), C.int(streams), C.int(coupled),
		(*C.uchar)(unsafe.Pointer(&demixingMatrix[0])), C.opus_int32(len(demixingMatrix)), &err)
	if err != 0 || dec == nil {
		return nil
	}
	return &CProjDecoder{p: dec, channels: channels}
}

func (d *CProjDecoder) Destroy() { C.opus_projection_decoder_destroy(d.p) }

// DecodeFloat returns n samples per channel decoded.
func (d *CProjDecoder) DecodeFloat(pkt []byte, pcm []float32, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_projection_decode_float(d.p, pktPtr, pktLen,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// DecodeInt16 decodes int16 PCM.
func (d *CProjDecoder) DecodeInt16(pkt []byte, pcm []int16, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_projection_decode(d.p, pktPtr, pktLen,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// ProjEncInitSnap mirrors the C `struct proj_enc_snap`.
type ProjEncInitSnap struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	DemixSize        int32
	DemixGain        int32
	ArenaSize        int32
	DemixBytes       []byte
}

// CProjEncInitAndSnapshot drives opus_projection_ambisonics_encoder_init
// in C and returns the post-init scalar snapshot plus the serialized
// demixing matrix.
func CProjEncInitAndSnapshot(Fs int32, channels, mappingFamily, application int) (ProjEncInitSnap, int) {
	var csnap C.struct_proj_enc_snap
	var streams, coupled C.int
	// Generous buf — the largest supported layout (fifthoa, 38 cols) has
	// nb_input_streams * channels * 2 bytes, bounded well under 65K.
	buf := make([]byte, 65536)
	ret := C.opus_test_proj_enc_init_and_snapshot(
		C.opus_int32(Fs), C.int(channels), C.int(mappingFamily), C.int(application),
		&streams, &coupled, &csnap,
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.opus_int32(len(buf)))
	var out ProjEncInitSnap
	out.NbChannels = int(csnap.nb_channels)
	out.NbStreams = int(csnap.nb_streams)
	out.NbCoupledStreams = int(csnap.nb_coupled_streams)
	out.DemixSize = int32(csnap.demix_size)
	out.DemixGain = int32(csnap.demix_gain)
	out.ArenaSize = int32(csnap.arena_size)
	if out.DemixSize > 0 {
		out.DemixBytes = append(out.DemixBytes, buf[:out.DemixSize]...)
	}
	return out, int(ret)
}
