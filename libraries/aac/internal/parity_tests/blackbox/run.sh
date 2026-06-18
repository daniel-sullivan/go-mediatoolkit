#!/usr/bin/env bash
# Black-box parity check: build the mstorsjo/fdk-aac v2.0.3 CLI (`aac-enc`,
# plus a tiny `aac-dec` we compile against the freshly-built static lib)
# from pristine upstream sources with no local modifications, then drive
# both them and the pure-Go nativeaac port with identical synthetic PCM
# and assert the encoded raw access units match byte-for-byte and the
# decoded PCM matches int-for-int.
#
# Nothing in the vendored C tree (libraries/aac/libfdk/**) or in our cgo
# helpers participates — the C side is the upstream CLI invoked
# out-of-process. v2.0.3 is the same tag the vendored tree tracks.
#
# Why AAC is the strong case: FDK-AAC is FIXED-POINT (int32 Q-format) for
# both decode and the ported encoder kernels. There is no FP/FMA/ULP
# excuse: a matching encoder config reproduces the bitstream BYTE-FOR-BYTE
# and decode is EXACT integer equality. The scalar/no-intrinsics CFLAGS
# below are belt-and-suspenders to mirror the opus/flac convention, not a
# correctness requirement for these integer kernels.
#
# Safe to re-run. Reuses an existing clone+build if present.
set -euo pipefail

UPSTREAM_URL="https://github.com/mstorsjo/fdk-aac.git"
# Pin to v2.0.3 — the exact tag the vendored libraries/aac/libfdk tree
# tracks. Override with AAC_UPSTREAM_REF=... if the vendored tree moves.
UPSTREAM_REF="${AAC_UPSTREAM_REF:-v2.0.3}"
UPSTREAM_DIR="${AAC_UPSTREAM_DIR:-/tmp/fdk-aac-upstream}"
# CFLAGS mirror the Go port's parity-oracle build (see libraries/aac/mise.toml
# and the cgo.go oracle headers): scalar, no FMA fusion, no autovec. For the
# fixed-point integer kernels this is belt-and-suspenders — the arithmetic is
# bit-identical regardless — but kept to match the opus/flac black-box shape.
CFLAGS_CLEAN="-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }

step "Pristine upstream clone at ${UPSTREAM_DIR} (ref ${UPSTREAM_REF})"
if [[ ! -d "${UPSTREAM_DIR}/.git" ]]; then
  mkdir -p "$(dirname "${UPSTREAM_DIR}")"
  git clone "${UPSTREAM_URL}" "${UPSTREAM_DIR}"
fi
( cd "${UPSTREAM_DIR}" && git fetch --tags origin && git checkout --detach "${UPSTREAM_REF}" )

step "Verify tracked upstream files are pristine"
if ! ( cd "${UPSTREAM_DIR}" && git diff --quiet HEAD ); then
  ( cd "${UPSTREAM_DIR}" && git diff --stat HEAD ) >&2
  echo "ERROR: upstream clone at ${UPSTREAM_DIR} has local edits to tracked files — rerun will not be a black-box check." >&2
  exit 1
fi

LIB_A="${UPSTREAM_DIR}/.libs/libfdk-aac.a"
step "Build upstream libfdk-aac (static) with matching CFLAGS"
if [[ ! -f "${LIB_A}" ]]; then
  ( cd "${UPSTREAM_DIR}" && \
    ./autogen.sh && \
    ./configure --disable-shared --enable-static CFLAGS="${CFLAGS_CLEAN}" CXXFLAGS="${CFLAGS_CLEAN}" && \
    make -j8 )
fi
test -f "${LIB_A}"

# fdk-aac's shipped `aac-enc` CLI is NOT usable for a byte-exact parity check:
# it hardcodes ADTS transport (TT_MP4_ADTS) and defaults the afterburner on. The
# pure-Go port mirrors the in-tree cgo encode oracle, which is RAW transmux
# (TRANSMUX 0, no ADTS framing) with the afterburner OFF (default). Driving
# `aac-enc -a 0` still differs by exactly 7 bytes/frame (the CBR rate-control
# reserves the per-frame ADTS header in its bit budget) and produces a
# different bitstream by construction. So instead we compile two tiny pristine
# CLIs of our own, kept in a scratch dir OUTSIDE the upstream git tree and
# linked only against the freshly-built static lib + upstream public headers:
#
#   aac-rawenc  — AAC-LC CBR, AOT 2, TRANSMUX 0 (raw AUs), afterburner default,
#                 mirroring internal/parity_tests/encode-e2e/cgo.go exactly.
#                 Emits a length-prefixed AU stream + the ASC to a sidecar.
#   aac-dec     — TT_MP4_RAW + aacDecoder_ConfigRaw(ASC), PCM limiter disabled,
#                 mirroring internal/parity_tests/decode-e2e/cgo.go. Reads the
#                 same length-prefixed AU stream, writes int16 LE PCM.
#
# These are still a faithful black box: an independent build of the upstream
# library, driven out-of-process by a minimal driver that matches the
# documented oracle config — no edits to the upstream tree.
SCRATCH="${UPSTREAM_DIR}/../fdk-aac-blackbox-cli"
mkdir -p "${SCRATCH}"
ENC_SRC="${SCRATCH}/aac-rawenc.c"
ENC_BIN="${SCRATCH}/aac-rawenc"
DEC_SRC="${SCRATCH}/aac-dec.c"
DEC_BIN="${SCRATCH}/aac-dec"

step "Generate + build out-of-tree aac-rawenc CLI (raw transmux)"
cat > "${ENC_SRC}" <<'AACENC_EOF'
/* Raw-transmux AAC-LC CBR encoder CLI for the go-mediatoolkit AAC black-box
 * parity suite. Mirrors the in-tree cgo encode oracle
 * (internal/parity_tests/encode-e2e/cgo.go) EXACTLY: AOT 2, BITRATEMODE 0
 * (CBR), TRANSMUX 0 (raw access units, no ADTS framing), afterburner left at
 * its default 0, no AACENC_CHANNELORDER override. Reads a 16-bit PCM WAV,
 * encodes frame by frame, and writes a length-prefixed AU stream:
 *   [4 bytes big-endian AU length][AU bytes]   (repeated)
 * The ASC (AudioSpecificConfig, from aacEncInfo confBuf) is written to a
 * sidecar path so the raw-transport decoder can be configured. Links only
 * against the freshly-built libfdk-aac.a — no source edits to the upstream
 * tree. */
#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "aacenc_lib.h"
#include "wavreader.h"

int main(int argc, char **argv) {
    if (argc < 5) {
        fprintf(stderr, "usage: %s bitrate in.wav out.aus out.asc\n", argv[0]);
        return 1;
    }
    int bitrate = atoi(argv[1]);
    void *wav = wav_read_open(argv[2]);
    if (!wav) { fprintf(stderr, "open wav %s\n", argv[2]); return 1; }
    int format, channels, sample_rate, bps;
    if (!wav_get_header(wav, &format, &channels, &sample_rate, &bps, NULL)) {
        fprintf(stderr, "bad wav header\n"); return 1;
    }
    HANDLE_AACENCODER enc;
    if (aacEncOpen(&enc, 0, channels) != AACENC_OK) { fprintf(stderr, "aacEncOpen\n"); return 1; }
    int mode = (channels == 1) ? MODE_1 : MODE_2;
    if (aacEncoder_SetParam(enc, AACENC_AOT, 2) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_SAMPLERATE, sample_rate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_CHANNELMODE, mode) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_BITRATE, bitrate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_BITRATEMODE, 0) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_TRANSMUX, 0) != AACENC_OK) {
        fprintf(stderr, "SetParam\n"); return 1;
    }
    if (aacEncEncode(enc, NULL, NULL, NULL, NULL) != AACENC_OK) { fprintf(stderr, "init\n"); return 1; }
    AACENC_InfoStruct info; memset(&info, 0, sizeof(info));
    if (aacEncInfo(enc, &info) != AACENC_OK) { fprintf(stderr, "info\n"); return 1; }

    FILE *fasc = fopen(argv[4], "wb");
    if (!fasc) { perror(argv[4]); return 1; }
    fwrite(info.confBuf, 1, info.confSize, fasc);
    fclose(fasc);

    FILE *out = fopen(argv[3], "wb");
    if (!out) { perror(argv[3]); return 1; }

    int per = channels * info.frameLength;
    int16_t *buf = (int16_t *)malloc((size_t)per * 2);
    int frames = 0;
    for (;;) {
        int rd = wav_read_data(wav, (uint8_t *)buf, per * 2);
        int got = rd / 2; /* int16 samples read (wav is LE, host LE) */

        AACENC_BufDesc inDesc;  memset(&inDesc, 0, sizeof(inDesc));
        AACENC_BufDesc outDesc; memset(&outDesc, 0, sizeof(outDesc));
        AACENC_InArgs inArgs;   memset(&inArgs, 0, sizeof(inArgs));
        AACENC_OutArgs outArgs; memset(&outArgs, 0, sizeof(outArgs));

        void *inPtr = buf;
        INT inId = IN_AUDIO_DATA, inSize = got * (INT)sizeof(int16_t), inElem = (INT)sizeof(int16_t);
        inDesc.numBufs = 1; inDesc.bufs = &inPtr; inDesc.bufferIdentifiers = &inId;
        inDesc.bufSizes = &inSize; inDesc.bufElSizes = &inElem;

        uint8_t ob[20480];
        void *outPtr = ob;
        INT outId = OUT_BITSTREAM_DATA, outSize = (INT)sizeof(ob), outElem = 1;
        outDesc.numBufs = 1; outDesc.bufs = &outPtr; outDesc.bufferIdentifiers = &outId;
        outDesc.bufSizes = &outSize; outDesc.bufElSizes = &outElem;

        inArgs.numInSamples = (got <= 0) ? -1 : got; /* -1 flushes at EOF */
        AACENC_ERROR e = aacEncEncode(enc, &inDesc, &outDesc, &inArgs, &outArgs);
        if (e == AACENC_ENCODE_EOF) break;
        if (e != AACENC_OK) { fprintf(stderr, "encode %d\n", e); return 1; }
        if (outArgs.numOutBytes > 0) {
            uint32_t L = (uint32_t)outArgs.numOutBytes;
            uint8_t h[4] = { (uint8_t)(L >> 24), (uint8_t)(L >> 16), (uint8_t)(L >> 8), (uint8_t)L };
            fwrite(h, 1, 4, out);
            fwrite(ob, 1, L, out);
            frames++;
        }
        if (got <= 0) break;
    }
    fclose(out);
    aacEncClose(&enc);
    wav_read_close(wav);
    free(buf);
    fprintf(stderr, "aac-rawenc: %d AUs\n", frames);
    return 0;
}
AACENC_EOF

if [[ ! -x "${ENC_BIN}" || "${ENC_SRC}" -nt "${ENC_BIN}" || "${LIB_A}" -nt "${ENC_BIN}" ]]; then
  CC_BIN="${CC:-cc}"
  "${CC_BIN}" ${CFLAGS_CLEAN} \
    -I"${UPSTREAM_DIR}" \
    -I"${UPSTREAM_DIR}/libAACenc/include" \
    -I"${UPSTREAM_DIR}/libSYS/include" \
    "${ENC_SRC}" "${UPSTREAM_DIR}/wavreader.c" "${LIB_A}" -lm -o "${ENC_BIN}"
fi
test -x "${ENC_BIN}"

step "Generate + build out-of-tree aac-dec CLI (raw transmux, limiter off)"
cat > "${DEC_SRC}" <<'AACDEC_EOF'
/* Raw-transmux AAC-LC decoder CLI for the go-mediatoolkit AAC black-box parity
 * suite. fdk-aac ships no aac-dec, so we provide a tiny one mirroring the cgo
 * decode oracle (internal/parity_tests/decode-e2e/cgo.go) EXACTLY: TT_MP4_RAW
 * transport, aacDecoder_ConfigRaw(ASC), AAC_PCM_LIMITER_ENABLE=0. Reads the ASC
 * sidecar + the length-prefixed AU stream emitted by aac-rawenc, decodes each
 * AU, and writes signed 16-bit little-endian interleaved PCM. Links only
 * against the freshly-built libfdk-aac.a — no source edits to the upstream
 * tree. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "aacdecoder_lib.h"

static unsigned char *slurp(const char *path, long *len) {
    FILE *f = fopen(path, "rb");
    if (!f) { perror(path); return NULL; }
    fseek(f, 0, SEEK_END);
    *len = ftell(f);
    fseek(f, 0, SEEK_SET);
    unsigned char *b = (unsigned char *)malloc((size_t)*len);
    if (fread(b, 1, (size_t)*len, f) != (size_t)*len) { fclose(f); free(b); return NULL; }
    fclose(f);
    return b;
}

int main(int argc, char **argv) {
    if (argc < 4) {
        fprintf(stderr, "usage: %s in.asc in.aus out.pcm\n", argv[0]);
        return 1;
    }
    long ascLen = 0, ausLen = 0;
    unsigned char *asc = slurp(argv[1], &ascLen);
    unsigned char *aus = slurp(argv[2], &ausLen);
    if (!asc || !aus || ascLen <= 0 || ausLen <= 0) { fprintf(stderr, "read inputs\n"); return 1; }

    FILE *out = fopen(argv[3], "wb");
    if (!out) { perror(argv[3]); return 1; }

    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (!h) { fprintf(stderr, "aacDecoder_Open\n"); return 1; }
    if (aacDecoder_SetParam(h, AAC_PCM_LIMITER_ENABLE, 0) != AAC_DEC_OK) {
        fprintf(stderr, "disable limiter\n"); return 1;
    }
    UCHAR *ascPtr = (UCHAR *)asc;
    UINT ascSize = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &ascPtr, &ascSize) != AAC_DEC_OK) {
        fprintf(stderr, "ConfigRaw\n"); return 1;
    }

    INT_PCM pcm[8 * 2048];
    int frames = 0;
    long off = 0;
    while (off + 4 <= ausLen) {
        uint32_t L = ((uint32_t)aus[off] << 24) | ((uint32_t)aus[off + 1] << 16) |
                     ((uint32_t)aus[off + 2] << 8) | (uint32_t)aus[off + 3];
        off += 4;
        if (off + (long)L > ausLen) { fprintf(stderr, "truncated AU\n"); return 1; }
        UCHAR *ptr = (UCHAR *)(aus + off);
        UINT valid = L, bufSize = L;
        AAC_DECODER_ERROR e = aacDecoder_Fill(h, &ptr, &bufSize, &valid);
        if (e != AAC_DEC_OK) { fprintf(stderr, "Fill %d\n", e); return 1; }
        e = aacDecoder_DecodeFrame(h, pcm, (INT)(sizeof(pcm) / sizeof(pcm[0])), 0);
        if (e != AAC_DEC_OK) { fprintf(stderr, "DecodeFrame %d\n", e); return 1; }
        CStreamInfo *si = aacDecoder_GetStreamInfo(h);
        if (!si) { fprintf(stderr, "no stream info\n"); return 1; }
        int n = si->frameSize * si->numChannels;
        fwrite(pcm, sizeof(INT_PCM), (size_t)n, out);
        frames++;
        off += L;
    }
    fclose(out);
    aacDecoder_Close(h);
    free(asc);
    free(aus);
    fprintf(stderr, "aac-dec: %d frames\n", frames);
    return 0;
}
AACDEC_EOF

if [[ ! -x "${DEC_BIN}" || "${DEC_SRC}" -nt "${DEC_BIN}" || "${LIB_A}" -nt "${DEC_BIN}" ]]; then
  CC_BIN="${CC:-cc}"
  "${CC_BIN}" ${CFLAGS_CLEAN} \
    -I"${UPSTREAM_DIR}/libAACdec/include" \
    -I"${UPSTREAM_DIR}/libSYS/include" \
    "${DEC_SRC}" "${LIB_A}" -lm -o "${DEC_BIN}"
fi
test -x "${DEC_BIN}"

step "Run black-box Go test driving aac-rawenc + aac-dec"
cd "${REPO_ROOT}"
export AAC_ENC_BIN="${ENC_BIN}"
export AAC_DEC_BIN="${DEC_BIN}"
export CGO_CFLAGS="${CFLAGS_CLEAN}"
export CGO_CFLAGS_ALLOW='.*'

go test -tags 'aac_blackbox,aacfdk,aac_strict' -v -count=1 -timeout 300s ./libraries/aac/internal/parity_tests/blackbox/...

step "Black-box parity complete"
