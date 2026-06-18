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
#include "opus_private.h"
#include <stdlib.h>
#include <string.h>

// Snapshot of the scalar fields in OpusMSEncoder (as defined in
// opus_private.h). The C struct exposes its layout in the private
// header, so we can directly read each field.
struct ms_enc_snap {
    int nb_channels;
    int nb_streams;
    int nb_coupled_streams;
    unsigned char mapping[256];
    int arch;
    int lfe_stream;
    int application;
    opus_int32 Fs;
    int variable_duration;
    int mapping_type;
    opus_int32 bitrate_bps;
    opus_int32 arena_size;
};

static void ms_enc_fill_snap(const OpusMSEncoder *st, opus_int32 arena_size,
                             struct ms_enc_snap *snap) {
    memset(snap, 0, sizeof(*snap));
    snap->nb_channels = st->layout.nb_channels;
    snap->nb_streams = st->layout.nb_streams;
    snap->nb_coupled_streams = st->layout.nb_coupled_streams;
    memcpy(snap->mapping, st->layout.mapping, sizeof(snap->mapping));
    snap->arch = st->arch;
    snap->lfe_stream = st->lfe_stream;
    snap->application = st->application;
    snap->Fs = st->Fs;
    snap->variable_duration = st->variable_duration;
    snap->mapping_type = (int)st->mapping_type;
    snap->bitrate_bps = st->bitrate_bps;
    snap->arena_size = arena_size;
}

int opus_test_ms_encoder_init_and_snapshot(
    opus_int32 Fs, int channels, int streams, int coupled_streams,
    const unsigned char *mapping, int application,
    struct ms_enc_snap *snap)
{
    opus_int32 size = opus_multistream_encoder_get_size(streams, coupled_streams);
    if (size <= 0) return OPUS_INTERNAL_ERROR;
    OpusMSEncoder *st = (OpusMSEncoder *)malloc(size);
    if (!st) return OPUS_ALLOC_FAIL;
    int ret = opus_multistream_encoder_init(st, Fs, channels, streams, coupled_streams,
                                            mapping, application);
    if (ret == OPUS_OK && snap) {
        ms_enc_fill_snap(st, size, snap);
    }
    free(st);
    return ret;
}

int opus_test_ms_surround_encoder_init_and_snapshot(
    opus_int32 Fs, int channels, int mapping_family, int application,
    int *out_streams, int *out_coupled,
    unsigned char *out_mapping,
    struct ms_enc_snap *snap)
{
    opus_int32 size = opus_multistream_surround_encoder_get_size(channels, mapping_family);
    if (size <= 0) return OPUS_INTERNAL_ERROR;
    OpusMSEncoder *st = (OpusMSEncoder *)malloc(size);
    if (!st) return OPUS_ALLOC_FAIL;
    int streams = 0, coupled = 0;
    int ret = opus_multistream_surround_encoder_init(st, Fs, channels, mapping_family,
                                                     &streams, &coupled, out_mapping,
                                                     application);
    if (ret == OPUS_OK) {
        if (snap) ms_enc_fill_snap(st, size, snap);
        if (out_streams) *out_streams = streams;
        if (out_coupled) *out_coupled = coupled;
    }
    free(st);
    return ret;
}
*/
import "C"
import "unsafe"

// MSEncInitSnap mirrors the C `struct ms_enc_snap`.
type MSEncInitSnap struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	Mapping          [256]byte
	Arch             int
	LfeStream        int
	Application      int
	Fs               int32
	VariableDuration int
	MappingType      int
	BitrateBps       int32
	ArenaSize        int32
}

func cSnapToGo(csnap *C.struct_ms_enc_snap) MSEncInitSnap {
	var out MSEncInitSnap
	out.NbChannels = int(csnap.nb_channels)
	out.NbStreams = int(csnap.nb_streams)
	out.NbCoupledStreams = int(csnap.nb_coupled_streams)
	out.Arch = int(csnap.arch)
	out.LfeStream = int(csnap.lfe_stream)
	out.Application = int(csnap.application)
	out.Fs = int32(csnap.Fs)
	out.VariableDuration = int(csnap.variable_duration)
	out.MappingType = int(csnap.mapping_type)
	out.BitrateBps = int32(csnap.bitrate_bps)
	out.ArenaSize = int32(csnap.arena_size)
	for i := 0; i < 256; i++ {
		out.Mapping[i] = byte(csnap.mapping[i])
	}
	return out
}

// CMSEncoderInitAndSnapshot drives opus_multistream_encoder_init in C
// and returns the post-init scalar snapshot.
func CMSEncoderInitAndSnapshot(Fs int32, channels, streams, coupled int,
	mapping []byte, application int) (MSEncInitSnap, int) {
	var csnap C.struct_ms_enc_snap
	ret := C.opus_test_ms_encoder_init_and_snapshot(
		C.opus_int32(Fs), C.int(channels), C.int(streams), C.int(coupled),
		(*C.uchar)(unsafe.Pointer(&mapping[0])), C.int(application),
		&csnap)
	return cSnapToGo(&csnap), int(ret)
}

// CMSSurroundEncoderInitAndSnapshot drives opus_multistream_surround_encoder_init.
func CMSSurroundEncoderInitAndSnapshot(Fs int32, channels, mappingFamily, application int) (MSEncInitSnap, []byte, int, int, int) {
	var csnap C.struct_ms_enc_snap
	var streams, coupled C.int
	mapping := make([]byte, 256)
	ret := C.opus_test_ms_surround_encoder_init_and_snapshot(
		C.opus_int32(Fs), C.int(channels), C.int(mappingFamily), C.int(application),
		&streams, &coupled,
		(*C.uchar)(unsafe.Pointer(&mapping[0])),
		&csnap)
	return cSnapToGo(&csnap), mapping[:channels], int(streams), int(coupled), int(ret)
}
