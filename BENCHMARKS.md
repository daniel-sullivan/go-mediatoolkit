# Audio codec benchmarks — pure-Go (native) vs C (cgo)

This document compares each audio codec's **pure-Go 1:1 port** (the
`NewNative…` path) against its **vendored C reference** (the cgo backend), for
decode and — where an encoder exists — encode, on this host.

## Methodology

| | |
|---|---|
| **Host** | Apple M3 Pro (`machdep.cpu.brand_string`), `darwin/arm64` |
| **Toolchain** | `go1.26.3 darwin/arm64` |
| **CPU label in raw output** | `cpu: Apple M3 Pro`, `-12` (GOMAXPROCS=12) |
| **Measurement** | `go test -bench … -benchmem -count=6`; figures below are the **median** of the 6 samples |
| **Date** | 2026-06-11 |

### What "native" and "cgo" mean here

* **native** — the pure-Go 1:1 port (`internal/native{flac,mp3,aac}` /
  `libraries/opus` Go decoder/encoder), reached through the public
  `NewNative{Decoder,Encoder}` surface (or the internal port package directly
  where a same-binary C reference would otherwise duplicate-symbol clash — see
  per-codec notes). This is the **default build**: FMA-fused arithmetic and the
  arm64 **NEON** fast paths are *enabled*. It is **not** the `*_strict`
  bit-exact build (that build disables FMA/SIMD for parity and is slower; it is
  not what an application would ship).
* **cgo** — the vendored C library the port mirrors, compiled and linked via
  cgo with `CGO_ENABLED=1`: **libFLAC** (FLAC), **minimp3** decode +
  **libmp3lame** encode (MP3), **libopus** (Opus), **Fraunhofer FDK-AAC**
  (AAC). The C is built `-O2` (production-style), so this is a
  native-vs-production-C comparison, not native-vs-scalar-oracle.

### Throughput metrics

* **MB/s** is `b.SetBytes`-derived: for **decode** it is the **compressed
  stream** bytes/sec; for **encode** it is the **input PCM** bytes/sec. (FLAC
  encode bytes = int32 PCM; MP3/AAC encode bytes = int16 PCM.) MB/s columns are
  therefore comparable *within* an operation, not across decode↔encode.
* **x-realtime** = audio-seconds processed per wall-second =
  `audio_seconds / (ns_per_op × 1e-9)`. A value of 100× means the codec runs
  100× faster than playback. Audio-second basis per op:
  * FLAC / MP3: a **1.000 s** buffer (44 100 frames @ 44.1 kHz, stereo).
  * AAC: a **0.998 s** buffer (43 × 1024-sample AAC-LC access units @ 44.1 kHz, stereo).
  * Opus: a **0.020 s** (20 ms) frame — 960 samples @ 48 kHz, mono.

### Ratio convention

`native ÷ cgo` on **ns/op**. **> 1 ⇒ cgo is faster** (native takes that many ×
longer); **< 1 ⇒ native is faster**.

---

## Results

### FLAC — libFLAC reference

1 s stereo 16-bit signal; encode at compression levels 0/5/8, decode of a
level-5 stream.

| Operation | native ns/op | cgo ns/op | native/cgo | native MB/s | cgo MB/s | native ×RT | cgo ×RT |
|---|--:|--:|--:|--:|--:|--:|--:|
| Encode L0 | 1 591 616 | 720 218 | 2.21 | 265 | 500 | 628× | 1389× |
| Encode L5 | 1 891 168 | 1 134 676 | 1.67 | 187 | 311 | 529× | 881× |
| Encode L8 | 4 581 916 | 2 937 066 | 1.56 |  77 | 120 | 218× | 341× |
| Decode    |   795 816 |   496 954 | 1.60 | 138 | 221 | 1257× | 2012× |

### Opus — libopus reference

20 ms frame @ 48 kHz mono; CELT (`RESTRICTED_LOWDELAY`, 64 kbps) and SILK
(`VOIP`, 12 kbps) paths.

| Operation | native ns/op | cgo ns/op | native/cgo | native ×RT | cgo ×RT |
|---|--:|--:|--:|--:|--:|
| CELT decode |  18 879 | 14 483 | 1.30 | 1059× | 1381× |
| CELT encode |  80 500 | 71 763 | 1.12 |  248× |  279× |
| SILK decode |  13 879 |  7 008 | 1.98 | 1441× | 2854× |
| SILK encode | 182 058 | 145 645 | 1.25 |  110× |  137× |

### MP3 — minimp3 (decode) / libmp3lame (encode) reference

1 s stereo @ 44.1 kHz; CBR 192 kbps. Decode input is a libmp3lame-encoded
stream replayed by both decoders.

| Operation | native ns/op | cgo ns/op | native/cgo | native MB/s | cgo MB/s | native ×RT | cgo ×RT |
|---|--:|--:|--:|--:|--:|--:|--:|
| Decode | 1 597 940 |   832 230 | 1.92 | 16 | 33 | 626× | 1202× |
| Encode | 19 568 324 | 7 580 813 | 2.58 | 9.3 | 24 |  51× |  132× |

Plus the pre-existing **bit-reader micro-benchmark** (`BenchmarkGetBits`,
minimp3 `get_bits` vs the Go port; not a full codec op, no x-realtime):
steady-state **native ≈ 371 µs/op**, **cgo ≈ 225 µs/op** (native/cgo ≈ 1.65).
The first 1–2 native samples ramp from cache/JIT warmup (~2 ms → ~360 µs) and
are excluded from the steady figure.

### AAC — Fraunhofer FDK-AAC reference

~1 s stereo @ 44.1 kHz; AAC-LC CBR 128 kbps, raw access units. The cgo encoder
runs with the FDK **afterburner** on (its highest-quality mode); the cgo
decoder runs with the PCM peak-limiter disabled (the deterministic fixed-point
chain the port mirrors).

| Operation | native ns/op | cgo ns/op | native/cgo | native MB/s | cgo MB/s | native ×RT | cgo ×RT |
|---|--:|--:|--:|--:|--:|--:|--:|
| Encode | 4 888 556 | 6 559 174 | **0.75** | 36 | 27 | 204× | 152× |
| Decode | 3 739 702 | 1 440 564 | 2.60 | 4.2 | 11 | 267× | 693× |

---

## Analysis

**Every native path runs comfortably faster than real time** — the slowest,
MP3 LAME-port encode, still sustains ~51× realtime, and decoders are 600–1400×.
For any normal playback / file-processing workload the pure-Go ports are fast
enough that the native-vs-cgo gap is latency headroom, not a bottleneck.

**Where the pure-Go port is competitive or wins**

* **AAC encode — native is *faster* than C (0.75×).** This is the standout
  result: the pure-Go FDK-AAC encoder port beats the production fdk encoder.
  The C reference is configured with the afterburner (extra trellis/quantizer
  search) for best quality, which costs it CPU; the port reproduces the FDK
  *fixed-point* integer kernels (no FP/FMA divergence to fight) and the Go
  compiler schedules them well on the M3. AAC's encoder is the one place the
  port is unambiguously ahead.
* **Opus is close across the board.** CELT encode is within **12%** and SILK
  encode within **25%** of libopus; CELT decode within 30%. The Opus port has
  the most mature hand-written **arm64 NEON** Go-assembly fast paths in the
  tree (NSQ, biquad, inner-product, float2int16, pitch), and it shows — this is
  the codec where the 1:1 port most nearly matches optimized C. The native
  paths also allocate almost nothing per op (0–46 small allocs).
* **FLAC and MP3 decode** land at **1.6–1.9×** of C — a single-digit-x gap that
  the FLAC optimization roadmap (CRC slice-by-8, word-accumulator bit-reader,
  shared int32 LPC-MAC NEON kernel) is expected to narrow further.

**Where C wins, and why**

* **SILK decode (1.98×)** and **AAC decode (2.6×)** are the widest decode gaps.
  Both lean on tight fixed-point inner loops (LSF/LPC synthesis; IMDCT +
  overlap-add) where the C compilers' auto-vectorization and the references'
  hand-tuned scalar code still beat the literal port. The port's decode side
  also carries 1:1-fidelity overhead (per-frame state objects, bounds-checked
  slice indexing, `[][]int32` channel buffers) — visible in the higher
  `allocs/op` and `B/op` of the native decode columns vs the near-zero-alloc C.
* **MP3 LAME encode (2.58×)** is the largest gap overall. The encoder is a
  literal port of LAME's psychoacoustic model + quantization loops with heavy
  per-frame allocation (~28k allocs/op, ~4.3 MB/op), and none of that path has
  SIMD fast paths yet — it is the clearest optimization target.
* **FLAC encode (1.56–2.21×, worst at L0)** — libFLAC's encoder is a
  fixed-point, near-zero-allocation (1 alloc/op) state machine; the port does
  more allocation (~80 allocs/op) and lacks the L0 fast path's tight loops. The
  gap *narrows* as the level rises (L8 1.56× vs L0 2.21×) because higher levels
  spend proportionally more time in the LPC search both implementations share.

**Headline native/cgo ratios:** AAC encode **0.75×** (native wins) · Opus CELT
encode **1.12×** · Opus SILK encode **1.25×** · Opus CELT decode **1.30×** ·
FLAC decode **1.60×** · FLAC encode **1.56–2.21×** · Opus SILK decode **1.98×**
· MP3 decode **1.92×** · AAC decode **2.60×** · MP3 encode **2.58×**.

### Caveats / variance

* `count=6`, median reported. A few series were noisy on a loaded laptop: AAC
  decode cgo ranged 1.07–2.05 ms, FLAC L0 native 1.3–3.9 ms, MP3 decode cgo
  0.74–3.16 ms. The medians damp the outliers but treat single-digit-percent
  differences as noise, not signal.
* The **native** numbers are the **default FMA/NEON build**, *not* the
  `flac_strict` / `mp3_strict` / `aac_strict` bit-exact build. The strict build
  disables FMA and SIMD for parity and is slower; it is a correctness gate, not
  a shipping configuration.
* All four cgo references compile + link cleanly on this host
  (`mp3lame`, `aacfdk`, FLAC cgo, opus cgo all built; nothing was skipped or
  unmeasurable). The MP3 LAME-port encoder is the partial path in flight, but
  it does run end-to-end here.

---

# Hardware video (hwaccel)

This is a **separate report** from the audio tables above. `go-mediatoolkit`
has **no pure-Go video codec**, so there is nothing to compare a port against —
the `codec/hwaccel` backends drive the platform's **fixed-function** video
engines (Intel Arc VAAPI, Apple VideoToolbox, the Pi 5's V4L2 stateless HEVC
block, NVIDIA NVENC/NVDEC). These numbers are therefore a **hardware
throughput** report (fps + MP/s), *not* a native-vs-C comparison.

## Methodology

* **Benchmarks.** Hardware-gated `Benchmark…` functions in
  `codec/hwaccel/*_bench_test.go` (build-tagged `linux` for VAAPI/V4L2/NVENC,
  `darwin` for VideoToolbox) drive the real encode/decode/transcode paths and
  `b.Skip` cleanly when the backend or codec is unavailable (mirroring the
  round-trip tests). Each iteration pushes a fixed batch of frames through the
  hardware (16 for the synthesised NV12 clips, 18 for the VP9/AV1 IVF clips, 30
  access units for the Pi HEVC stream).
* **MB/s → MP/s.** `b.SetBytes` is set to the **NV12 frame bytes**
  (`w·h·3/2`) summed over the batch, so `go test -bench` reports MB/s of
  decoded output (decode) or input PCM-equivalent raw video (encode). Because
  NV12 is **1.5 bytes/pixel**, **MP/s = MB/s ÷ 1.5** (megapixels of
  luma-resolution per second). **fps = frames ÷ wall-time** per op. Resolution
  is fixed per row so the figures are comparable.
* **What's measured.** Decode benchmarks encode a real hardware-coded stream
  **once before the timer**, then time only the decode loop (a fresh decoder
  per op). The H.264→H.265 transcode times the full decode + re-encode pipeline
  (the NVR path). VP9/AV1 decode is fed an ffmpeg all-intra IVF clip (no
  hardware VP9/AV1 *encoder* exists on the iHD driver — see below).
* **Hosts.** Arc = Intel **Arc A380** (iHD/VAAPI 1.22, host AMD Ryzen 5 9600).
  Mac = Apple **M3 Pro** (VideoToolbox). Pi = **Raspberry Pi 5**
  (`rpi-hevc-dec` V4L2 stateless decoder, Request API). Numbers are a single
  `-benchtime 3s -count 1` run on otherwise-idle boxes; treat single-digit-%
  gaps as noise, not signal. Every figure below is a **real measured run**.

## Intel Arc A380 — VAAPI (640×480 NV12)

| Codec | Direction | fps | MP/s | MB/s | Host |
|---|---|--:|--:|--:|---|
| H.264 | decode      | 1181 | 363 | 544 | Arc A380 |
| H.265 | decode      | 1031 | 317 | 475 | Arc A380 |
| H.264 | encode      |  826 | 254 | 381 | Arc A380 |
| H.265 | encode      |  840 | 258 | 387 | Arc A380 |
| H.264→H.265 | transcode | 473 | 145 | 218 | Arc A380 |
| VP9   | decode      | 1035 | 318 | 477 | Arc A380 |
| AV1   | decode      | 1065 | 327 | 491 | Arc A380 |

The **H.264→H.265 transcode** is the NVR path: hardware-decode the inbound
H.264 and re-encode it as H.265 in one pipeline, ~473 fps end to end at 480p.
**VP9 and AV1 are decode-only here** — the iHD driver on the A380 exposes no
VP9/AV1 *encode* entrypoint, so those encode benches/round-trips
`Skip`/`ErrEncodeUnsupportedOnDriver` rather than report a number.

## Apple M3 Pro — VideoToolbox

| Codec | Direction | Resolution | fps | MP/s | MB/s | Host |
|---|---|---|--:|--:|--:|---|
| H.264 | encode | 640×480 |  324 |  100 | 149 | M3 Pro |
| H.265 | encode | 640×480 |  257 |   79 | 118 | M3 Pro |
| H.264 | decode | 640×480 |  753 |  231 | 347 | M3 Pro |
| H.265 | decode | 640×480 | 1189 |  365 | 548 | M3 Pro |
| AV1   | decode | 320×240 | 1336 |  103 | 154 | M3 Pro |

VideoToolbox **AV1 is decode-only** (M3+ has an AV1 decoder but no AV1
encoder); it is benched at the 320×240 reference-clip resolution, so its MP/s
is not directly comparable to the 640×480 rows. VP9 has no Apple-silicon
hardware decoder, so `BenchmarkVideoToolboxVP9…`-style paths simply skip.

## Raspberry Pi 5 — V4L2 stateless HEVC (640×480 NV12)

| Codec | Direction | fps | MP/s | MB/s | Host |
|---|---|--:|--:|--:|---|
| H.265 | decode | 835 | 256 | 385 | Pi 5 |

The Pi 5's `rpi-hevc-dec` is a **stateless** (Request API) decoder driven
frame-by-frame: the bench cross-compiles (`CGO_ENABLED=0 GOOS=linux
GOARCH=arm64`), feeds a 30-AU ffmpeg-generated HEVC elementary stream via the
`V4L2_TEST_HEVC` env, and the de-tiled luma was validated bit-exact against an
ffmpeg reference (MAE 0.000) in the companion round-trip test. It is the only
V4L2 codec on the Pi (no hardware HEVC *encoder*).

## NVENC / NVDEC — unmeasured

`BenchmarkNVENCH264Encode` / `BenchmarkNVDECH264Decode` exist and `b.Skip`
cleanly when no NVIDIA device is present. **No NVIDIA hardware was available**
for this report, so these report **no number** — they are written against the
SDK 13.0 ABI and would produce fps/MP/s the same way on a real GPU box. The
NVENC encode/decode source path itself is likewise unverified on hardware.

## Running these

```
# Arc (deploy + native run):
/tmp/hwtest-deploy.sh daniel@<arc> \
  'cd ~/hwtest-go-mediatoolkit && CGO_ENABLED=1 go test -tags cgo \
     -run "^$" -bench BenchmarkVAAPI -benchtime 3s ./codec/hwaccel/'

# Mac (local):
go test -run "^$" -bench BenchmarkVideoToolbox -benchtime 3s ./codec/hwaccel/

# Pi (cross-compile + scp + run with a generated HEVC stream):
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o hw_arm64.test ./codec/hwaccel/
#   …scp hw_arm64.test + a .h265 clip to the Pi, then:
V4L2_TEST_HEVC=/path/test.h265 ./hw_arm64.test \
  -test.run "^$" -test.bench BenchmarkV4L2 -test.benchtime 3s
```
