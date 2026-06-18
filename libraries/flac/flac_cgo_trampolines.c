/* C-side trampolines and accessors for the Go-cgo wrappers.
 *
 * Lives outside the cgo preambles so it can `#include "_cgo_export.h"`
 * — the auto-generated header containing declarations of the //export'd
 * Go callbacks (flacGoReadCallback, flacGoWriteCallback, …). Cgo
 * generates that header during the build; preambles inside `import "C"`
 * comments cannot see it.
 *
 * The libFLAC callback typedefs (FLAC__StreamDecoderReadCallback, …)
 * use array-of-bytes parameters (`FLAC__byte buffer[]`) where the cgo-
 * synthesised Go declarations use plain pointers (`FLAC__byte*`). The
 * casts in flacGoInitStream / flacGoInitStreamEncoder bridge that
 * cosmetic difference; the underlying ABI is identical.
 */

#include <FLAC/stream_decoder.h>
#include <FLAC/stream_encoder.h>
#include <FLAC/format.h>
#include <FLAC/metadata.h>
#include "_cgo_export.h"

FLAC__StreamDecoderInitStatus flacGoInitStream(FLAC__StreamDecoder *dec, void *client_data) {
    return FLAC__stream_decoder_init_stream(
        dec,
        (FLAC__StreamDecoderReadCallback)     flacGoReadCallback,
        NULL,  /* seek_callback   */
        NULL,  /* tell_callback   */
        NULL,  /* length_callback */
        NULL,  /* eof_callback    */
        (FLAC__StreamDecoderWriteCallback)    flacGoWriteCallback,
        (FLAC__StreamDecoderMetadataCallback) flacGoMetadataCallback,
        (FLAC__StreamDecoderErrorCallback)    flacGoErrorCallback,
        client_data);
}

FLAC__int32 flacGoSampleAt(const FLAC__int32 * const buffer[], int ch, int i) {
    return buffer[ch][i];
}

const FLAC__StreamMetadata_StreamInfo *flacGoStreamInfo(const FLAC__StreamMetadata *m) {
    return &m->data.stream_info;
}

uint32_t flacGoFrameBlocksize(const FLAC__Frame *f) { return f->header.blocksize; }
uint32_t flacGoFrameChannels (const FLAC__Frame *f) { return f->header.channels;  }

/* Cgo represents FLAC's open-ended `const char *const X[]` arrays as
 * Go arrays of length zero, so indexing them from Go panics. Wrap the
 * lookups in C helpers that just return the string. */
const char *flacGoEncoderStateString  (FLAC__StreamEncoderState  s) { return FLAC__StreamEncoderStateString[s];  }
const char *flacGoEncoderInitStatusStr(FLAC__StreamEncoderInitStatus s) { return FLAC__StreamEncoderInitStatusString[s]; }
const char *flacGoDecoderStateString  (FLAC__StreamDecoderState  s) { return FLAC__StreamDecoderStateString[s];  }
const char *flacGoDecoderInitStatusStr(FLAC__StreamDecoderInitStatus s) { return FLAC__StreamDecoderInitStatusString[s]; }

/* VORBIS_COMMENT field accessors. */
const FLAC__StreamMetadata_VorbisComment *flacGoVorbisComment(const FLAC__StreamMetadata *m) {
    return &m->data.vorbis_comment;
}
uint32_t flacGoVorbisVendorLength(const FLAC__StreamMetadata_VorbisComment *vc) {
    return vc->vendor_string.length;
}
const char *flacGoVorbisVendorBytes(const FLAC__StreamMetadata_VorbisComment *vc) {
    return (const char*)vc->vendor_string.entry;
}
uint32_t flacGoVorbisCommentCount(const FLAC__StreamMetadata_VorbisComment *vc) {
    return vc->num_comments;
}
uint32_t flacGoVorbisCommentLength(const FLAC__StreamMetadata_VorbisComment *vc, uint32_t i) {
    return vc->comments[i].length;
}
const char *flacGoVorbisCommentBytes(const FLAC__StreamMetadata_VorbisComment *vc, uint32_t i) {
    return (const char*)vc->comments[i].entry;
}

FLAC__StreamEncoderInitStatus flacGoInitStreamEncoder(FLAC__StreamEncoder *enc, void *client_data) {
    return FLAC__stream_encoder_init_stream(
        enc,
        (FLAC__StreamEncoderWriteCallback)    flacGoEncWriteCallback,
        NULL,  /* seek_callback     — non-seekable stream output */
        NULL,  /* tell_callback     */
        NULL,  /* metadata_callback */
        client_data);
}
