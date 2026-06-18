# Black-box libopus parity check

Final sanity test: the pure-Go libopus port at
`libraries/opus/internal/libopus` is compared against a freshly-built,
**completely unmodified** upstream `xiph/opus` (main branch, pinned to
`788cc89ce4f2c42025d8c70ec1b4457dc89cd50f` by default).

Nothing in the vendored C tree (`libraries/opus/libopus/**`) or in our
cgo amalgamation helpers participates. The C side is the upstream
`opus_demo` CLI, invoked out-of-process. Zero cgo / linkage games.

## Run it

```sh
./libraries/opus/blackbox/run.sh
```

The script:

1. Clones `https://gitlab.xiph.org/xiph/opus.git` into
   `/tmp/opus-upstream/opus-main` (override with `OPUS_UPSTREAM_DIR=...`).
2. Checks out the pinned commit (override with `OPUS_UPSTREAM_REF=...`).
3. Aborts if any *tracked* file has been locally edited (untracked
   build artifacts are fine — they appear after `configure && make`).
4. Runs `./autogen.sh && ./configure --disable-shared --enable-static
   --disable-intrinsics --disable-dred --disable-osce --disable-bwe
   --disable-deep-plc ... && make -j8` with matching
   `CFLAGS=-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize
   -fno-unroll-loops -DENABLE_RES24=1`.
5. Runs `go test -tags opus_blackbox ./libraries/opus/blackbox/...`
   with `OPUS_DEMO_BIN` pointing at the built `opus_demo`.

### Why those build flags?

- `--disable-intrinsics`: the Go port targets scalar-equivalent C.
  Upstream's autoconf defaults to enabling NEON + DOTPROD on aarch64
  (and SSE on x86-64). Those kernels produce bit-different rounding
  than the scalar path. We disable to compare apples-to-apples.
- `-ffp-contract=off -fno-vectorize -fno-slp-vectorize
  -fno-unroll-loops`: prevents the C compiler from fusing multiply-
  adds or autovectorising, both of which change FP rounding behavior
  vs the Go port's explicit `fma_add`/`mul_f32` helpers.
- `-DENABLE_RES24=1`: our vendored config targets the 24-bit internal
  resolution codec variant. For the float build this only affects a
  handful of helper definitions (e.g. `smooth_fade`); for the fixed-
  point build it changes internal precision. Passed via CFLAGS rather
  than a source edit so the upstream tree remains pristine.

## What's tested

- **`TestBlackBox_EncodeParity`** (10 configs × 250 frames = 2500
  frames): each frame encoded by both upstream `opus_demo -e` and the
  Go port at identical settings from identical synthetic PCM (pink
  noise, log sine sweep 80 Hz→8 kHz, AM-modulated mix). Packet bytes
  must match byte-for-byte.

- **`TestBlackBox_ReferenceSelfConsistency`** (5 configs × 250
  frames): decoding the same packets twice via `opus_demo -d` must
  produce bit-identical PCM. Rules out any non-determinism in the C
  reference before we compare Go against it.

- **`TestBlackBox_DecodeParity`** (5 configs × 250 frames): Go
  encodes, then decodes via both `opus_demo -d` and the Go port.
  Compares int16 PCM and reports objective similarity stats — match %,
  max abs diff, RMS, SNR/PSNR dB, abs-diff histogram. Gate: max abs
  diff ≤ 1 and PSNR ≥ 85 dB.

- **`TestBlackBox_DecodeInt24Parity`** (5 configs × 250 frames): same
  pipeline but decoded via `opus_demo -d -24` and Go `opus_decode24`,
  which route through `FLOAT2INT24` (1/8388608 resolution) rather than
  `FLOAT2INT16` + soft-clip. If this is bit-exact but the int16 test
  shows drift, the drift is float→int16 rounding-tie only; if this
  diverges, the decoder math itself is diverging.

## Expected output

- `TestBlackBox_EncodeParity` **must** pass — a byte-exact bitstream
  is the strongest parity signal because encoder and decoder math are
  both covered by the single bit-exact check.
- `TestBlackBox_ReferenceSelfConsistency` **must** pass — control.
- `TestBlackBox_DecodeInt24Parity` **must** pass bit-exactly. This is
  the decoder-math correctness gate — as of this pin, all 1.68M
  samples across the five configs match byte-for-byte.
- `TestBlackBox_DecodeParity` passes within tolerance (max abs diff
  1 ULP on int16, PSNR ≈ 117 dB, ≈99.78% of samples identical).
  See the note below for why this last 0.2% isn't zero.

### Why int16 sees a handful of 1-ULP drifts despite int24 being bit-exact

int24 output matches bit-exactly, which means the float decoder output
agrees between the two builds to within 1/8388608 (~1.19e-7). But
int16 quantisation is 256× coarser. Whenever the true float value lies
within ~1/(2·8388608) of an int16 half-LSB boundary, a sub-int24-level
difference between the two build's floating-point results can push one
side below the boundary and the other above — producing a 1-ULP int16
drift even though both agree at int24 precision.

### The Go port is not the source of the drift

The three-way test in `libraries/opus/benchcmp/parity_threeway_test.go`
compares **three** decoders on one Go-encoded bitstream:

- `C_demo`: the upstream `opus_demo` subprocess (this README's focus)
- `C_cgo`: the vendored libopus linked via cgo amalgamation
- `Go`:    the pure-Go port

Observed consistently across all five configs:

    int16:  C_cgo vs Go    → 0 diffs (bit-exact)
    int16:  C_demo vs C_cgo → ~500 diffs, 1 ULP, PSNR ~117 dB
    int16:  C_demo vs Go    → same ~500 diffs, same PSNR as above
    int24:  all three pairs → 0 diffs (bit-exact)

`C_demo vs C_cgo` produces *exactly* the same drift pattern as
`C_demo vs Go`. The Go port carries zero drift against its cgo oracle;
the drift is entirely between two C builds of the same source (upstream
autotools + system libm vs cgo amalgamation). A port change cannot
close this last 0.2% — only matching toolchains would.

If you want to eliminate the drift entirely, use 24-bit output
(`DecodeInt24`) — it has bit-exact parity with every implementation
and preserves 8 extra bits of headroom besides.

## Why this exists

Our regular `benchcmp` parity suite links against our vendored libopus
tree via cgo amalgamation — necessary for cross-TU harness tricks
(private statics, symbol-renamed includes) but leaves the door open
to accidentally testing against a modified reference. This black-box
test closes that door: it gives high confidence the Go port remains
byte-exact when driven into a pipeline that uses stock libopus.
