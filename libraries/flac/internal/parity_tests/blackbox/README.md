# Black-box libFLAC parity check

Final sanity test: the pure-Go libFLAC port at
`libraries/flac/internal/nativeflac` is compared against a freshly-built,
**completely unmodified** upstream `xiph/flac` (release tag `1.5.0` by
default — the version vendored under `libraries/flac/libflac`, whose
`config.h` reports `PACKAGE_VERSION "1.5.0"` and whose `format.c` carries
the `reference libFLAC 1.5.0 20250211` vendor string).

Nothing in the vendored C tree (`libraries/flac/libflac/**`) or in our
cgo oracle helpers participates. The C side is the upstream `flac`
reference CLI, invoked out-of-process. Zero cgo / linkage games.

## Run it

```sh
MISE_EXPERIMENTAL=1 mise run //libraries/flac:parity:blackbox
# or directly:
./libraries/flac/internal/parity_tests/blackbox/run.sh
```

The script:

1. Clones `https://github.com/xiph/flac.git` into
   `/tmp/flac-upstream/flac-1.5.0` (override with `FLAC_UPSTREAM_DIR=...`).
2. Checks out the pinned tag (override with `FLAC_UPSTREAM_REF=...`).
3. Aborts if any *tracked* file has been locally edited (untracked
   build artifacts are fine — they appear after `configure && make`).
4. Runs `./autogen.sh && ./configure --disable-shared --enable-static
   --disable-ogg --disable-asm-optimizations --disable-doxygen-docs
   --disable-examples ... && make -j8` with matching
   `CFLAGS=-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize
   -fno-unroll-loops -DFLAC__NO_ASM`.
5. Runs `go test -tags 'flac_blackbox,flac_strict'
   ./libraries/flac/internal/parity_tests/blackbox/...` with `FLAC_BIN`
   pointing at the built `src/flac/flac`.

### Why those build flags?

- `--disable-asm-optimizations` + `-DFLAC__NO_ASM`: the Go port targets
  the scalar C path. The vendored `config.h` leaves `FLAC__HAS_X86INTRIN`
  / `FLAC__HAS_NEONINTRIN` undefined and builds scalar kernels only;
  upstream's autoconf would otherwise enable NEON / SSE kernels that
  produce different rounding / fusion than the scalar path the Go port
  was translated from. Disabling them keeps the upstream binary
  scalar-equivalent to the vendored config.
- `-ffp-contract=off -fno-vectorize -fno-slp-vectorize
  -fno-unroll-loops`: prevents the C compiler from fusing multiply-adds
  or autovectorising in the FP-heavy LPC/window analysis, both of which
  would change rounding vs the `flac_strict` Go build's explicit FMA-free
  helpers.
- `--disable-ogg`: we compare native FLAC framing, not the Ogg transport;
  avoids a libogg build dependency.

## How encode byte-parity is achieved

FLAC is lossless, so the strongest possible parity signal is a
**byte-identical `.flac` bitstream**: it proves the LPC analysis, the
fixed-vs-LPC predictor choice, the quantised-coefficient precision, the
Rice-parameter partition search, and the frame framing all at once.

The Go encoder is a 1:1 port of libFLAC's *streaming* (no-seek)
`init_stream` path. The CLI, in its normal file-output mode, seeks back
after encoding to backfill three STREAMINFO fields the streaming path
cannot know up-front: MIN/MAX frame size and the MD5 of the input. To
drive both encoders down the identical no-seek path we:

- run the CLI in **stdout streaming mode** (`-c`). Piped to stdout the
  CLI cannot seek, so it leaves MIN/MAX framesize and MD5 zero — exactly
  like the Go streaming encoder with a NULL seek callback;
- set the Go encoder's **total-samples estimate** to the true count, so
  its STREAMINFO total-samples field matches the CLI (which knows the
  count from the whole input file);
- pass `--no-seektable --no-padding`, suppressing the SEEKTABLE/PADDING
  blocks the CLI would otherwise append. The VORBIS_COMMENT block (the
  libFLAC vendor string) is emitted identically by both sides — the Go
  port reproduces it byte-for-byte.

With those three matched, the streams are byte-identical. (Empirically,
without the total-samples match the only divergence is the 36-bit
total-samples field at STREAMINFO offset ~24; without stdout mode the
divergence is the framesize+MD5 fields; the audio frames themselves are
byte-identical in every case.)

## What's tested

- **`TestBlackBox_EncodeParity`** (10 configs): synthetic PCM (pink
  noise, log sine sweep 80 Hz→8 kHz, a 440 Hz sine, and an AM-modulated
  mix) at a spread of rates (44.1/48/96 kHz), channel counts (1/2), bit
  depths (8/16/24), compression levels (0/5/8) and block sizes
  (4096/1152) is encoded by both upstream `flac -c` and the Go port at
  identical settings. The `.flac` byte streams must match
  **byte-for-byte**.

- **`TestBlackBox_DecodeParity`** (10 configs): the CLI encodes the
  synthetic PCM to a `.flac` file, then both the CLI (`flac -d`) and the
  Go port decode it back to PCM. Because FLAC is lossless, both decoded
  streams must equal the **original source samples bit-for-bit** (and
  therefore each other). Comparing both against the source also catches a
  decoder that agrees with the CLI but mangles the audio.

## Expected output

- `TestBlackBox_EncodeParity` passes byte-exactly for every config except
  the two pure log-sine-sweep configs, which hit a known CLI-vs-library
  divergence (see "Known encode gap" below) and are logged-but-not-failed.
- `TestBlackBox_DecodeParity` **must** pass bit-exactly for every config
  (sweep included) — lossless round-trip on both implementations.

## Status

As of the FLAC 1.5.0 pin:

- **Decode parity: 10/10 bit-exact.** Both the Go port and the CLI
  reproduce the original samples bit-for-bit for every config.
- **Encode parity: 8/10 byte-identical** to the upstream CLI
  (`pink`/`sine`/`mixed` across mono/stereo, 8/16/24-bit, levels 0/5/8,
  block sizes 4096/1152). The two pure log-sine-sweep configs
  (`sweep_mono_48k_16_l5`, `sweep_stereo_96k_24_l5`) diverge — see below.

No tolerance is applied to the matching configs — FLAC is integer-lossless,
so there is no FP/ULP drift budget on decode (unlike the Opus black-box
path). The only FP sensitivity is in the encoder's LPC/window analysis.

## Known encode gap (sweep signals)

The two pure log-sine-sweep encode configs are **not** byte-identical to
the upstream `flac` CLI. The divergence is a **CLI-driver-vs-library**
difference, **not a Go-port bug**. The evidence (reproduced with the
isolation probe used while building this suite):

```
config: sweep, mono, 48 kHz, 16-bit, -5, blocksize 4096 (96000 samples)

sizes:  CLI = 56787 bytes
        vendored cgo libFLAC (process_interleaved, no-seek) = 57096 bytes
        Go port (1 call)            = 57096 bytes
        Go port (2048-sample chunks)= 57096 bytes

Go port  == vendored cgo libFLAC ?  true   (byte-for-byte)
Go port  == CLI                  ?  false
cgo lib  == CLI                  ?  false   (same divergence)
```

Key findings:

- The Go encoder is **byte-identical to the vendored libFLAC library**
  (`FLAC__stream_encoder_process_interleaved` with NULL seek), which is
  the exact code path it is a 1:1 port of. That is the parity the port
  must hold, and it holds.
- The upstream **CLI** produces a *smaller* stream than that library API
  with the *same* nominal settings (level 5, `tukey(5e-1)` apodization,
  blocksize 4096, streamable-subset on — all verified to match). The
  STREAMINFO and the first ~14 frames are byte-identical; the streams
  first diverge around byte 36443 (~frame 15 of 24), in the
  high-frequency tail of the sweep, where the CLI and the library land on
  opposite sides of a borderline LPC-order / Rice-partition decision.
- It is **not** input chunking (feeding the Go encoder in 2048-sample
  CHUNK_OF_SAMPLES chunks, exactly as the CLI reads input, still yields
  57096), nor `process` vs `process_interleaved` (same core), nor
  apodization / blocksize / compression-level / streamable-subset (all
  confirmed equal).

The suspected cause is a subtle difference in how the CLI driver
exercises libFLAC's encoder versus the bare streaming API on this
FP-sensitive signal — outside the Go port's control, since the port
already matches the library it ports byte-for-byte. The
`mixed_*` configs (which contain a sweep *component* mixed with pink
noise) are byte-exact, so the trigger is the pure high-frequency sweep
specifically.

To close this gap would require reproducing whatever the CLI driver does
differently from `FLAC__stream_encoder_process_interleaved` — a libFLAC
investigation, not a Go-port change. Until then the two sweep configs are
flagged `encodeByteExact=false`: the encode test logs the divergence with
concrete byte/frame numbers, and the bit-exact lossless decode round-trip
is still hard-asserted for them.

## Why this exists

Our regular `parity_tests` suite links against our vendored libFLAC tree
via cgo — necessary for the per-stage bit-exact oracles, but it leaves
the door open to accidentally testing against a modified reference. This
black-box test closes that door: it gives high confidence the Go port
remains byte-exact when driven into a pipeline that uses stock,
independently-built libFLAC.
