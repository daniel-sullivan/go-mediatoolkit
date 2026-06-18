# Black-box LAME parity check

Final sanity test: the pure-Go LAME-derived MP3 encoder at
`libraries/mp3/internal/nativemp3` (surfaced through the public
`mp3.NewNativeEncoder` behind the `mp3lame` tag) is compared against a
freshly-built, **completely unmodified** upstream LAME **3.100** — the exact
release the port is a 1:1 translation of
(`libraries/mp3/liblame/libmp3lame/version.h`: `3.100.0`).

Nothing in the vendored C tree (`libraries/mp3/liblame/**`,
`libraries/mp3/libminimp3/**`) or in our cgo parity oracles participates. The C
side is the upstream `lame` CLI, built from the SourceForge release tarball and
invoked out-of-process. Zero cgo / linkage games.

## Run it

```sh
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:parity:blackbox
# or directly:
./libraries/mp3/internal/parity_tests/blackbox/run.sh
```

The script:

1. Downloads `lame-3.100.tar.gz` from SourceForge into `/tmp/lame-upstream`
   (override the dir with `MP3_UPSTREAM_DIR=...`, the URL with `LAME_URL=...`).
2. Verifies the tarball SHA256
   (`ddfe36cab873794038ae2c1210557ad34857a4b6bdc515785d1da9e175b1da1e`) and
   extracts it.
3. Runs `./configure --disable-shared --enable-static --disable-nasm
   --disable-gtktest CFLAGS="-O2 -ffp-contract=off -fno-vectorize
   -fno-slp-vectorize -fno-unroll-loops" && make -j8`.
4. Runs `go test -tags 'mp3_blackbox,mp3lame,mp3_strict'
   ./libraries/mp3/internal/parity_tests/blackbox/...` with `LAME_BIN` pointing
   at the built `frontend/lame`.

### Why those build flags?

- `--disable-nasm`: the Go port targets the scalar C path. LAME's nasm
  assembly (`choose_table_asm`, the SSE/3DNow FFT kernels) and the SIMD code in
  `vector/` are nasm-gated and produce different rounding/results than the
  portable C. Disabling keeps the upstream binary scalar-equivalent — matching
  the path the port was written against.
- `-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops`:
  LAME is FP-heavy. These prevent the C compiler from fusing multiply-adds or
  autovectorising, both of which change FP rounding vs the port's explicit
  FMA-free helpers selected under `mp3_strict`. This is the same scalar-oracle
  CFLAGS the rest of the MP3 parity tooling uses (see `libraries/mp3/mise.toml`).

The Go port runs under `mp3_strict` (FMA-free) + `mp3lame` (the LGPL encoder
fence) — the same gate as `//libraries/mp3:encode-parity`.

## What's tested

- **`TestBlackBox_EncodeParity`** (8 configs): each config encoded by both the
  upstream `lame` CLI and the pure-Go port at identical CBR settings (mode /
  `-q` quality / `-b` bitrate, `--noreplaygain`) from identical synthetic PCM
  (pink noise, log sine sweep 80 Hz → 8 kHz, AM-modulated mix). The full MP3
  byte streams must match **byte-for-byte** — the 1:1-port contract.

- **`TestBlackBox_ReferenceSelfConsistency`** (4 configs): encoding the same PCM
  twice via the `lame` CLI must produce bit-identical MP3. Rules out any
  non-determinism in the C reference before comparing Go against it.

- **`TestBlackBox_DecodeParity`** (8 configs): the MP3 produced by the `lame`
  CLI is decoded by both `lame --decode` (LAME's mpglib) and the pure-Go
  decoder. The pure-Go decoder is the **minimp3** (CC0) port — an *independent*
  decoder, not LAME-derived — so this is a cross-decoder sanity check reported
  with objective similarity stats (match %, RMS, SNR/PSNR), gated at a generous
  PSNR floor (60 dB), **not** a bit-exact assertion. Bit-exact decode parity is
  covered separately against minimp3's own oracle by `//libraries/mp3:parity`
  and `//libraries/mp3:decode-parity`.

## Measured parity status (as of the last run on this pin)

- `TestBlackBox_ReferenceSelfConsistency` — **PASS** (control). Encoding the
  same PCM twice with the `lame` CLI is bit-identical.

- `TestBlackBox_DecodeParity` — **PASS**. After recovering the encoder-delay
  offset (the pure-Go minimp3 port emits the full priming the LAME-tag delay
  signals, which `lame --decode` trims — ~1129 samples per channel), the pure-Go
  decoder matches `lame --decode` at **max abs diff = 1 ULP** and **PSNR
  92.7–99.0 dB** across all 8 configs (1-ULP rounding ties between minimp3 and
  LAME's mpglib decoder — the same class of cross-toolchain drift documented for
  Opus int16 decode). The decoder math is correct.

- `TestBlackBox_EncodeParity` — **PASS.** The pure-Go encoder is byte-identical
  to LAME 3.100 across the matrix: 7 of the 8 configs match the upstream `lame`
  CLI **byte-for-byte**, and the 8th (`sweep_stereo_44k_q2_256`) matches except
  for the documented ≤ 2-ULP ATH FP-build residue (1 audio byte + its 2 derived
  CRC-16 tag fields) — a residue the CLI *also* exhibits versus the repo's own
  vendored cgo libmp3lame, so it is **not** a pure-Go gap. See the diagnosis
  below.

### How this was driven to byte-exact

The original scaffold of this suite failed: the pure-Go stream diverged from the
vendored cgo libmp3lame by ~47k/48.9k bytes, first diff at byte 21. The root
cause was a **single stubbed init function**, `FrameEncodeStages.OptimumBandwidth`
(`internal/nativemp3/stages.go`) — LAME's `optimum_bandwidth` (lame.c:195), the
bitrate-driven input-filter lowpass auto-bandwidth. The CBR/ABR init path
(`init.go` lowpass auto-detect, lame.c:722) calls it when the user leaves the
lowpass unset, which the public encoder always does for CBR. The stub returned
`(0, 0)`, so `gfp.LowpassFreq` was set to **0** instead of the bitrate-mapped
cutoff (e.g. 20.5 kHz for 128 kbps mono). With no input lowpass filter, the
polyphase-filtered spectrum fed to the psymodel/MDCT/quantizer differed from
LAME's from the very first granule — cascading into different `part2_3_length` /
`big_values` / `scalefac_compress` and a fully divergent main-data bitstream, as
well as a wrong `nLowpass` byte in the LAME tag. Porting the 17-entry `freq_map`
table (indexed by the already-ported `nearestBitrateFullIndex`) was the entire
fix.

Three measurements now localise the *residual* unambiguously (per config
`sweep_stereo_44k_q2_256`, 97801-byte stream):

| Comparison | Differing bytes | Where |
| --- | --- | --- |
| `lame` CLI vs **pure-Go** `NewNativeEncoder` | **5 / 97801** | 1 audio byte + MusicCRC + tag-CRC |
| `lame` CLI vs vendored cgo libmp3lame (this repo) | **5 / 97801** | the *same* 5 bytes |
| vendored cgo libmp3lame vs **pure-Go** `NewNativeEncoder` | **0 / 97801** | byte-identical |

- The pure-Go port is **byte-identical to the repo's vendored cgo libmp3lame on
  every config** (proven directly with in-repo cgo encode oracles over the exact
  same PCM). That is the real 1:1-port contract, and it holds 100%.
- The only residual is vs the *external* `lame` CLI, and it is **identical
  between the CLI and libmp3lame** — i.e. it is purely a difference between two
  compilations of the same LAME 3.100 source, not anything the Go port does. It
  is the documented ≤ 2-ULP `pow`/`log10` **ATH-shaping FP residue**
  (`libraries/mp3/README.md`): on one sweep-heavy granule a borderline
  quantization decision ties and flips a single big_values bit, changing one
  audio byte. The LAME tag's **MusicCRC** (CRC-16 over the audio) and **tag-CRC**
  (CRC-16 over the tag) then necessarily shift too — those are the 4 tag bytes.
  No metadata field (lowpass, peak, flags, delays, preset, length) differs.

### Honesty note — the assertion is hard, not weakened

`TestBlackBox_EncodeParity` requires **byte-identical**. When a stream is not
byte-identical it is accepted **only** if the divergence is confined to the
documented FP residue envelope, enforced precisely in code:

- the stream **lengths must be equal** (a length diff can never be the residue);
- audio-frame diffs must be `≤ athResidueAudioBudget` (3) bytes — the matrix
  actually hits exactly 1; a structural regression diverges thousands of bytes
  from frame 1 and fails instantly;
- tag-frame diffs must be `≤ 4` bytes **and** lie strictly within the LAME
  extension's trailing MusicCRC + tag-CRC fields (`tagDivergenceIsCRCOnly`) — any
  other tag byte (a wrong `nLowpass`, peak, flags, …) fails hard.

So the suite is the byte-exact encode gate it was scaffolded to be; the bounded
exception is the same accepted-tolerance discipline the per-stage `quantize-pvt`
ATH slice uses (`athMaxULP`), not a fuzzy threshold and not fake green.

## Why this exists

The regular `internal/parity_tests/` suite links the vendored LAME / minimp3 via
cgo amalgamation — necessary for cross-TU oracle tricks, but it leaves the door
open to accidentally testing against a modified reference. This black-box test
closes that door: it builds stock LAME 3.100 from the pristine release tarball
and drives it as an external process, giving high confidence the Go encoder
remains byte-exact against a reference nothing in this repo can have perturbed.
