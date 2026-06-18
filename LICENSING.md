# Licensing

go-mediatoolkit is **MIT-licensed** (see [LICENSE](LICENSE)). A few vendored
third-party engines and the Go code *derived* from them carry their own
licenses. This file maps them so you know exactly what a given build links.

## The default build is 100% MIT + permissive

A plain `go build ./...` (and every package outside the explicit list below)
contains only MIT-licensed go-mediatoolkit code plus permissively-licensed
vendored references:

| Component | License | Notes |
|---|---|---|
| go-mediatoolkit (all original code) | MIT | This repo's own code. |
| `libraries/mp3/libminimp3/**` (minimp3) | CC0-1.0 (public domain) | Vendored MP3 **decoder** reference. |
| `libraries/flac/libflac/**` (libFLAC) | BSD-3-Clause | Vendored FLAC reference. |

## The LGPL island: the MP3 encoder (opt-in only)

The MP3 **encoder** is a 1:1 port of LAME, so both the vendored C and the Go
translation of it are **LGPL-2.0-or-later**. Because a literal translation of
LGPL source is a derivative work, the LGPL covers:

| Component | License | Build tag |
|---|---|---|
| `libraries/mp3/liblame/**` (vendored LAME 3.100 C source) | LGPL-2.0-or-later (see `libraries/mp3/liblame/COPYING.LAME`) | — (only compiled via the tagged cgo wrappers below) |
| `libraries/mp3/mp3_cgo_libmp3lame_*.c` and `libraries/mp3/mp3_cgo_mpglib_*.c` (cgo translation-unit wrappers that `#include` the LAME / bundled-mpglib sources) | LGPL-2.0-or-later | `cgo && mp3lame` |
| `libraries/mp3/encoder_cgo.go` (cgo libmp3lame encoder backend) | LGPL-2.0-or-later | `cgo && mp3lame` |
| `libraries/mp3/encoder.go` + `libraries/mp3/native_encoder.go` (public seams routing to the pure-Go LAME port) | LGPL-2.0-or-later | `!cgo && mp3lame` / `mp3lame` |
| The LAME-derived Go encoder port in `libraries/mp3/internal/nativemp3/`: `fft.go`, `bitstream_encode.go`, `huffman_encode.go`, `frame_encode*.go`, `mdct_analysis*.go`, `psymodel*.go`, and the encoder `parityhooks_*` (`parityhooks_mdct.go`, `parityhooks_psymodel.go`, `parityhooks_huffman_encode.go`) | LGPL-2.0-or-later (per-file SPDX header) | `mp3lame` (composed with `mp3_strict` / `!mp3_strict` on the FP files) |
| The LAME-derived encoder parity slices under `libraries/mp3/internal/parity_tests/`: `frame-encode-dispatch/`, `mdct-analysis/`, `psychoacoustic-model/`, `huffman-encode/` | LGPL-2.0-or-later | `cgo && mp3lame` |

The MIT/CC0 side is unaffected: the cgo **decoder** wrapper
`libraries/mp3/mp3_cgo_minimp3.c`, the pure-Go decoder files in
`libraries/mp3/internal/nativemp3/` (minimp3-derived), and the decoder parity
slices stay untagged and link no LGPL code.

**The LAME-derived files are fenced behind the `mp3lame` build tag.** A default
build (with or without cgo) excludes every one of them and links **no LGPL
code**; `NewEncoder` / `NewNativeEncoder` then return `ErrEncoderRequiresLAME`
telling you to rebuild with the tag. To enable MP3 encoding:

```
go build -tags mp3lame ./...
```

### What enabling `mp3lame` obligates (LGPL, weak copyleft)

If you **distribute a binary** built with `-tags mp3lame` (which statically
links the LAME-derived code), LGPL requires you to, for that combined work:
make the LAME source + your modifications available, carry the LGPL notices,
and allow the recipient to **relink** the application against a modified LAME
(e.g. ship object files / source, or link LAME dynamically). Using the encoder
privately (no distribution) imposes nothing. The rest of your app stays MIT.

*This is a summary, not legal advice — confirm with counsel before shipping an
`mp3lame` binary.*

## The FDK-AAC island: AAC decode + encode (opt-in only)

AAC has a single vendored engine — the **Fraunhofer FDK-AAC** library
(mstorsjo fork, v2.0.3) — used for **both** decode and encode. Its source
license is the Fraunhofer FDK-AAC license (SPDX `FDK-AAC`): non-FOSS but
**permissive** (not copyleft). AAC-LC patents **expired in 2017**, so the
AAC-LC target this island compiles carries no live patent concern. Because
every AAC code path goes through FDK-AAC, the whole island — the vendored C++
source, the cgo translation-unit wrappers, the cgo backends, and (later) the
1:1 Go port — is fenced behind the opt-in `aacfdk` build tag.

| Component | License | Build tag |
|---|---|---|
| `libraries/aac/libfdk/**` (vendored Fraunhofer FDK-AAC v2.0.3 C++ source) | FDK-AAC (see `libraries/aac/libfdk/COPYING`) | — (only compiled via the tagged cgo wrappers below) |
| `libraries/aac/fdk_tu_*.cpp` (per-TU cgo wrappers that `#include` one vendored FDK-AAC source each) | FDK-AAC | `cgo && aacfdk` |
| `libraries/aac/aac_cgo.go` (shared cgo preamble: include/link flags + FDK-AAC public headers) | FDK-AAC | `cgo && aacfdk` |
| `libraries/aac/decoder_cgo.go` + `libraries/aac/encoder_cgo.go` (cgo FDK-AAC decode/encode backends) | FDK-AAC | `cgo && aacfdk` |
| `libraries/aac/decoder.go` + `libraries/aac/encoder.go` (public seams returning `ErrEngineRequiresFDK` in the default build) | MIT | `!aacfdk` |
| The FDK-AAC-derived Go port under `libraries/aac/internal/nativeaac/` (per-file SPDX header) | FDK-AAC | `aacfdk` (the fixed-point kernels are exact-integer; `aac_strict` only toggles the parity assertions, it does not split the build) |
| The FDK-AAC parity slices under `libraries/aac/internal/parity_tests/` (when they land) | FDK-AAC | `cgo && aacfdk` |

**The FDK-AAC-derived files are fenced behind the `aacfdk` build tag.** A
default build (with or without cgo) excludes every one of them and links **no
FDK-AAC code**; `aac.NewDecoder` / `aac.NewEncoder` then return
`ErrEngineRequiresFDK` telling you to rebuild with the tag. The MIT/untagged
side is unaffected: the `containers/mp4` ISOBMFF box parser and the
`codec/aac` streaming adapter compile in the default build and only surface
`ErrEngineRequiresFDK` at use. To enable AAC encode/decode:

```
go build -tags aacfdk ./...   # requires cgo (CGO_ENABLED=1)
```

### What enabling `aacfdk` obligates

The Fraunhofer FDK-AAC license permits redistribution in source and binary
form with attribution and the carried license/patent notices (it is **not**
copyleft, so it imposes no relink/source-availability obligation on the rest
of your application). It does **not** grant patent licenses; for AAC profiles
beyond the patent-expired AAC-LC target, confirm patent status for your use.

*This is a summary, not legal advice — confirm with counsel before shipping an
`aacfdk` binary, and read `libraries/aac/libfdk/COPYING` in full.*
