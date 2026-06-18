# Contributing to go-mediatoolkit

Pure-Go audio + video toolkit. Module path: `go-mediatoolkit` (no domain). Go 1.26. Only runtime dep: `github.com/stretchr/testify`.

## Packages

- `codec/` — streaming `Encoder`/`Decoder` interfaces; subpackages per format (`opus`, `pcm`, `flac`, `mp3`, `aac`).
- `containers/` — container parsers; generic `Header[E]` view plus format-specific extras (`wav`, `ogg`, `flac`, `mp3`, `mp4`).
- `mutations/` — sample transforms (format conversion, interleaving, trimming, chunking).
- `resample/` — pure-Go libsamplerate port; sinc/linear/zoh.
- `generators/` — test signal helpers.
- `libraries/` — internal SIMD-optimised codec implementations (not for external import); per-format engines (`opus`, `flac`, `mp3`, `aac`) with cgo + pure-Go 1:1-port backends.
- `inspection/` — ad-hoc analysis utilities.
- `events/` — typed pub-sub bus (generic `Bus[T]`, sync delivery).
- `devices/` — OS audio device enumeration, hotplug, and capture/render streams (CoreAudio, WASAPI, PulseAudio).
- `buffers/` — lock-free SPSC ring of `float64` samples for bridging audio callbacks across goroutines.
- `consts/` — shared numeric constants (sample rates, channel counts, equal-temperament note frequencies).
- `timeline/` — Cue/Source playback engine; clips, fades, transforms, and nested timelines.
- `mixer/` — sums multiple `timeline.Source` streams onto an SPSC ring for a `devices.Stream` callback.
- `tools/` — example-only helpers that pull from multiple top-level packages (e.g. `audioio` bridges devices+timeline, `devicepicker` is a TUI). Not for production import.

## Style rules

- **Interface-first.** Core abstractions in `<pkg>.go` or `types.go`. Implementations in separate files organised by feature, not per-type.
- **Errors in `errors.go`.** Package-level sentinel vars only: `var ErrX = errors.New("pkg: message")`. Every message prefixed with `pkg:`. No typed errors. No `fmt.Errorf` wrapping in primary packages.
- **Doc comments.** Package docs explain purpose, concurrency constraints, and data layout (interleaved vs. split, sample format). Type/func docs imperative, start with the name.
- **Tests.** Table-driven via testify (`require` for setup, `assert` for logic). Tests live alongside impl as `*_test.go`; use external `_test` package only when deliberate.
- **Examples.** Standalone binaries at `<pkg>/examples/<concept>/main.go`. stdlib `log`, `log.Fatal` on error.
- **Logging.** stdlib `log` only. No slog/zap.
- **Build tags for SIMD/platform.** Existing shape: `//go:build arm64 && !opus_nosimd`; `opus_strict` disables fast paths. Follow the same pattern for new platform-specific code.
- **CGo.** Largely avoided. When used, guard with `//go:build cgo`. Prefer pure-Go or `purego` dlopen for new code. Automatic CGo paths activate via the built-in `cgo` build tag (no custom flag).

## Commit style

Imperative, specific, no scoped prefix. Describe what *and* why. Examples from the log:

- `Port celt_float2int16 + opus_limit2_checkwithin1 to arm64 NEON`
- `Wire NSQ SoA multi-kernel fusion into silk_NSQ_del_dec — 15% SILK encode`

## CI

GitHub Actions (`.github/workflows/`): `ci.yml` runs `-race` unit + integration
tests across ubuntu/macos/windows; `tests.yml` builds pure-Go (`CGO_ENABLED=0`)
and runs `go test ./...`; `lint.yml` enforces `gofmt`/`go vet`; and one
`blackbox-<codec>.yml` per codec (opus/flac/mp3/aac, via the reusable
`blackbox-reusable.yml`) builds a version-matched upstream reference CLI and runs
that codec's `parity:blackbox` mise task — each surfaces its own README badge.

Locally, run `go test ./...` for the pure-Go packages.

Bit-exact cgo parity suites (libFLAC oracle vs the pure-Go port) are **not**
covered by a bare `go test`: they require the `flac_strict` build tag and a
clang `-ffp-contract=off` oracle supplied via the mise env. Run them through
the mise tasks (mirrors the `opus_strict` convention):

```
MISE_EXPERIMENTAL=1 mise run //libraries/flac:parity   # bit-exact parity gate
MISE_EXPERIMENTAL=1 mise run //libraries/flac:test     # full flac package suite, strict
```

A bare `go test` of `libraries/flac/internal/parity_tests/...` without that env
and tag will diverge on the FP-heavy packages (window, lpc_encode, encode_e2e)
by design — the default build fuses FMA and is not a bit-exact target. See the
"FP parity convention" section atop `libraries/flac/internal/nativeflac/nativeflac.go`.

The MP3 port follows the same convention, with one extra axis: the encoder is a
1:1 port of **LAME** (LGPL-2.0-or-later) and is fenced behind the `mp3lame`
build tag — a default `go build ./...` links **zero** LGPL code and is
decode-only (minimp3, CC0). The bit-exact parity suite needs both the
`mp3_strict` FP tag *and* (for the encoder oracles) `mp3lame`, plus the same
`-ffp-contract=off` cgo env:

```
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:parity         # decoder oracles, -tags=mp3_strict
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:encode-parity  # LAME encoder oracles, -tags='mp3lame mp3_strict'
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:test           # full mp3 package suite, strict
```

Encoding (any encode path, parity or not) requires `-tags mp3lame`; without it
`NewEncoder` / `NewNativeEncoder` return `ErrEncoderRequiresLAME`. The encoder
parity slices can also be driven directly:
`go test -tags 'mp3lame mp3_strict' ./libraries/mp3/internal/parity_tests/...`
under the FP env. As with FLAC, a bare `go test` without the env/tags diverges
on the FP-heavy slices (mdct-analysis, psychoacoustic-model, the ATH shaping in
quantize-pvt) by design; the integer slices match either way. The pow/log10 ATH
shaping carries an accepted ≤ 2 ULP residual — see `libraries/mp3/README.md`.
See the "FP parity convention" / per-slice status atop
`libraries/mp3/internal/nativemp3/nativemp3.go`, and `LICENSING.md` for the
LGPL fence map.

The AAC/M4A port (`libraries/aac`, `codec/aac`, `containers/mp4`) is **not** an
FP target. FDK-AAC is the only AAC engine and it is **fixed-point** (int32
Q-format) for **both** decode and the ported encoder kernels, so parity is
**EXACT integer equality** on decode and a **byte-identical bitstream** on
encode — there is no FMA/ULP/`aac_strict`-FP concern. Do **not** add
`*_fp_strict`/`*_fp_default` splits or float shims here; `aac_strict` only
flips `nativeaac.StrictMode` to un-skip the integer-parity assertions. The
whole engine (vendored C, cgo backends, and the `internal/nativeaac` port —
decode **and** encode) is FDK-derived and fenced behind the `aacfdk` build tag,
so a default `go build ./...` links **zero** FDK-AAC and the `!aacfdk` seams
return `ErrEngineRequiresFDK` (`containers/mp4` + the `codec/aac` adapter stay
MIT/untagged and compile in the default build). The gate needs **both**
`aac_strict` *and* `aacfdk` so the cgo parity slices actually compile + run
(not vacuously skip), plus the same `-ffp-contract=off` cgo env (here
belt-and-suspenders for integer kernels):

```
MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity   # exact-integer parity gate, -tags 'aac_strict aacfdk'
MISE_EXPERIMENTAL=1 mise run //libraries/aac:test     # full aac package suite, strict + aacfdk
```

A bare `go test` of `libraries/aac/internal/parity_tests/...` without the tags
builds none of the FDK-gated slices. See the "Integer parity convention"
section atop `libraries/aac/internal/nativeaac/nativeaac.go`, and `LICENSING.md`
for the FDK-AAC fence map.
