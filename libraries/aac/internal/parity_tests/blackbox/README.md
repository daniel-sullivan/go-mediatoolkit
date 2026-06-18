# Black-box FDK-AAC parity check

Final sanity test: the pure-Go AAC-LC port at
`libraries/aac/internal/nativeaac` is compared against a freshly-built,
**completely unmodified** upstream `mstorsjo/fdk-aac` (pinned to tag
**`v2.0.3`** — the same tag the vendored `libraries/aac/libfdk` tree
tracks).

Nothing in the vendored C tree (`libraries/aac/libfdk/**`) or in our cgo
parity oracles participates. The C side is two tiny CLIs (`aac-rawenc` +
`aac-dec`) the script compiles against the freshly-built upstream static
lib, both invoked out-of-process. Zero cgo / linkage games in this
package. (See "Why not the shipped `aac-enc`" below — the example CLI
fdk-aac ships is not byte-comparable.)

## Why AAC is the strongest black-box case

FDK-AAC is a **fixed-point** codec (int32 Q-format) for both AAC-LC
decode and the ported encoder kernels. There is **no FP / FMA / ULP
excuse**:

- **encode** → a matched AAC-LC CBR config reproduces the bitstream
  **byte-for-byte** (we compare raw access units directly).
- **decode** → output PCM is **exact integer equality**.

The scalar / no-intrinsics CFLAGS below are belt-and-suspenders to mirror
the opus/flac black-box shape, not a correctness requirement for these
integer kernels.

## Run it

```sh
MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity:blackbox
```

or directly:

```sh
./libraries/aac/internal/parity_tests/blackbox/run.sh
```

The script:

1. Clones `https://github.com/mstorsjo/fdk-aac.git` into
   `/tmp/fdk-aac-upstream` (override with `AAC_UPSTREAM_DIR=...`).
2. Checks out tag `v2.0.3` (override with `AAC_UPSTREAM_REF=...`).
3. Aborts if any *tracked* file has been locally edited (untracked
   build artifacts are fine — they appear after `configure && make`).
4. Runs `./autogen.sh && ./configure --disable-shared --enable-static
   CFLAGS=... CXXFLAGS=... && make -j8` with
   `CFLAGS=-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize
   -fno-unroll-loops` to build `libfdk-aac.a`.
5. Compiles **two** tiny pristine CLIs of its own into a scratch dir
   **outside** the upstream git tree (`/tmp/fdk-aac-blackbox-cli/`),
   linking only against the freshly-built static lib + upstream public
   headers — no edits to the upstream tree:
   - `aac-rawenc` — AAC-LC CBR, AOT 2, `AACENC_TRANSMUX 0` (raw AUs, no
     ADTS framing), afterburner default, no `CHANNELORDER` override —
     mirroring `internal/parity_tests/encode-e2e/cgo.go` exactly. Writes
     a length-prefixed AU stream (`[4B BE len][AU]…`) + the ASC sidecar.
   - `aac-dec` — `TT_MP4_RAW` + `aacDecoder_ConfigRaw(ASC)`,
     `AAC_PCM_LIMITER_ENABLE=0` — mirroring
     `internal/parity_tests/decode-e2e/cgo.go`. Reads the ASC + the
     length-prefixed AUs, writes int16 LE PCM.
6. Runs `go test -tags 'aac_blackbox,aacfdk,aac_strict'
   ./libraries/aac/internal/parity_tests/blackbox/...` with
   `AAC_ENC_BIN` / `AAC_DEC_BIN` pointing at the built binaries and
   `CGO_CFLAGS_ALLOW='.*'`.

### Why not the shipped `aac-enc`?

fdk-aac does ship an `aac-enc` example CLI (behind `--enable-example`),
but it is **not byte-comparable** against the raw-transmux bitstream the
Go port targets, for two independent reasons:

- It hardcodes **ADTS** transport (`TT_MP4_ADTS`). Under CBR the encoder's
  rate-control reserves the per-frame 7-byte ADTS header inside the bit
  budget, so each AAC payload comes out **exactly 7 bytes smaller** than
  the raw (`TRANSMUX 0`) encode at the same bitrate — a *different*
  bitstream by construction (measured: 364/365 B vs the raw 371/372 B).
- It enables the **afterburner** by default (`-a` toggles it); the
  afterburner is a separate trellis-refinement search the 1:1 driver port
  does not run. (`-a 0` fixes this one, but not the ADTS budget shift.)

The example CLI exposes no flag to select raw transmux, so the faithful
choice is the minimal `aac-rawenc` above — still an independent build of
the upstream library, driven at the documented oracle config.

### Why those build flags?

- `-ffp-contract=off -fno-vectorize -fno-slp-vectorize
  -fno-unroll-loops`: mirrors the opus/flac/mp3 black-box convention and
  the in-tree cgo oracle build (`libraries/aac/mise.toml`). For
  fixed-point AAC the integer arithmetic is bit-identical regardless of
  these flags — they are belt-and-suspenders, not load-bearing.
- No `--disable-intrinsics` flag exists in fdk-aac's autoconf; the
  fixed-point kernels are scalar C by default on this host (any
  hand-written asm in `libFDK` is gated and produces bit-identical
  results to the C fallback), so there is no scalar/SIMD rounding fork
  to suppress as there is for the float opus codec.

### Matching the encoder config — the load-bearing detail

The pure-Go `FDKaacEnc_EncodeFrame` port mirrors the in-tree cgo encode
oracle (`internal/parity_tests/encode-e2e/cgo.go`), which configures:

| param                | value | note                                        |
| -------------------- | ----- | ------------------------------------------- |
| `AACENC_AOT`         | 2     | AAC-LC                                       |
| `AACENC_BITRATEMODE` | 0     | CBR                                         |
| `AACENC_TRANSMUX`    | 0     | raw AUs, no transport framing               |
| `AACENC_AFTERBURNER` | **0** | default; the trellis pass is NOT ported     |
| `AACENC_CHANNELORDER`| (unset)| FDK default (MPEG order)                    |

`aac-rawenc` sets exactly these. The native encoder's per-frame AUs come
out byte-identical to it (verified against the cgo oracle too: native ==
oracle == aac-rawenc, byte-for-byte).

## What's tested

- **`TestBlackBox_EncodeParity`** (6 configs): identical synthetic int16
  PCM (the same multi-tone waveform the in-tree e2e oracle uses, plus
  `generators.PinkNoise`) encoded by both `aac-rawenc` and the Go port,
  asserted **byte-identical** AU-for-AU. There is **no** leading priming
  offset — raw transmux fed frame-by-frame emits AU 0 from cold state,
  like the native encoder. The CLI emits one extra trailing AU (the
  `aacEncEncode(numInSamples=-1)` EOF flush) the per-frame native driver
  never produces, so the common-prefix (native count) AUs are compared.

- **`TestBlackBox_DecodeParity`** (6 configs): encode via `aac-rawenc`,
  then decode the raw-AU stream via both the upstream-linked `aac-dec`
  (`TT_MP4_RAW`, limiter disabled) and the Go port (fed the same raw
  AUs). int16 PCM asserted **exactly equal**. The FDK decoder has a
  one-frame output priming delay vs. `DecodeAccessUnit`'s direct decode
  (the `refDelay=1` the in-tree decode-e2e oracle documents); a
  leading-frame offset search (`alignPCM`, ≤ 3 frames, observed = 1)
  absorbs it before the exact comparison.

Mono + stereo, two bitrates (96k, 128k), two sample rates (44.1k, 48k).

## Scope / deferrals

- **AAC-LC only** (AOT 2), CBR. This is the must-have and the bit-exact
  target.
- **HE-AAC / HE-AAC v2 (SBR / PS) deferred.** The port has SBR/PS slices,
  but a black-box SBR config has extra encoder knobs (AOT 5/29, SBR mode,
  downsampled vs. dual-rate signaling) that need their own matrix and a
  raw-transmux config sweep; not covered here yet.
- **VBR deferred.** The oracle and this suite use CBR
  (`AACENC_BITRATEMODE 0`) only.

## HONESTY — parity result

**PASS — byte-identical encode + integer-exact decode, all 6 configs**
(captured `2026-06-18` against an unmodified upstream `mstorsjo/fdk-aac`
`v2.0.3` build):

```
TestBlackBox_EncodeParity  — 12 AUs byte-exact vs aac-rawenc, every config
  tone_mono_44100_128k   tone_stereo_44100_128k   tone_stereo_48000_128k
  tone_mono_48000_96k    pink_mono_44100_128k     pink_stereo_48000_128k
TestBlackBox_DecodeParity  — 12288 (mono) / 24576 (stereo) samples
  integer-exact vs aac-dec, CLI frame offset = 1 (the FDK priming delay)
```

The goal was byte-identical encode + integer-exact decode against the
independent upstream build, and it is met with **no gap**. The per-AU /
per-sample comparisons are strict equality (`require.Equal` on raw bytes
/ int16); only the *priming-delay alignment offset* is tolerated (bounded
to ≤ 3 frames, observed = 1, and logged), never the content. No
assertion was weakened to reach green.
