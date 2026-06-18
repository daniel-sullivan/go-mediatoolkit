# codec/hwaccel

A backend-pluggable framework for **hardware-accelerated video encode and
decode** — the ffmpeg model in pure Go. Take raw frames (or an incoming stream)
and re-encode on whatever fixed-function silicon the host exposes, **preferring
hardware and falling back to software loudly — never silently**. The headline
use case is the NVR / transcode path: decode an H.264 stream and re-encode it as
H.265 entirely on the GPU/ASIC.

This README is the **framework overview and the verified backend status**. For
the design rationale (why slice returns, why `video` is its own package, the
probe-to-API mapping per platform) read [`DESIGN.md`](DESIGN.md); for the shared
`Frame`/`Packet`/`Codec` types read [`../../video/README.md`](../../video). Once
present, performance numbers live in `BENCHMARKS.md` (owned separately).

## The model

* **Prefer hardware, fall back loudly.** `OpenEncoder` / `OpenDecoder` walk the
  registry of compiled-in backends in preference order and pick the first that
  advertises the requested codec + direction. Under the default
  `PreferHardware` policy, if none works it degrades to software **and** fires a
  `HardwareFallbackEvent` on your bus *and* logs a heavy multi-line `WARNING` —
  a silent CPU-melting software fallback in an NVR is an operational trap, so
  the framework refuses to let it happen quietly.
* **`CGO_ENABLED=0`, always.** No cgo, no packaged libraries. Pure syscall where
  the kernel ABI is stable (raw V4L2 `ioctl` on `/dev/videoN`), `purego` dlopen
  for the vendor userspace libraries (VideoToolbox / CoreMedia / CoreVideo on
  macOS, `libva` on Linux, `libcuda` + `libnvidia-encode` + `libnvcuvid` for
  NVIDIA). The whole subsystem links into a static, cgo-free binary.
* **Pipelined.** Hardware codecs are asynchronous, so `Encode`/`Decode` return a
  **slice** of `[]video.Packet` / `[]video.Frame` (zero, one, or several) and
  `Flush` drains the pipeline at end of stream. This differs from the
  synchronous audio `codec.Encoder` on purpose.

### Building

No C toolchain, no `pkg-config`, no ffmpeg or vendor SDK headers are needed to
build — the whole subsystem is cgo-free, so `CGO_ENABLED=0 go build ./...`
works:

```sh
CGO_ENABLED=0 go build ./codec/hwaccel/...
```

The vendor userspace libraries (VideoToolbox / CoreMedia / CoreVideo, `libva`,
`libcuda` + `libnvidia-encode` + `libnvcuvid`) are `dlopen`'d at **runtime** via
`purego` — they are a **runtime** dependency of the host, not a build-time one.
A binary builds anywhere; a given backend simply reports `Available() == false`
on a host that lacks its library or device node.

## Backend × codec matrix

Status reflects the committed code **and the committed test results** — it does
**not** overclaim. "verified" means a real round-trip test passed on the named
hardware; "spec-correct" means the path is implemented to the ABI/spec but no
matching device was available to run it; "gated" means the path is intentionally
refused with a sentinel error.

| Backend | Platform / target | H.264 | H.265 | VP9 | AV1 | Status |
|---|---|---|---|---|---|---|
| **videotoolbox** | macOS (Apple silicon) | enc + dec ✅ | enc + dec ✅ | — (unsupported) | **dec** ✅ | encode/decode H.264 & H.265 **verified**; AV1 **decode verified on M3**; VP9 has no Apple-silicon hardware path (Probe reports it unsupported) and there is no VP9/AV1 **encoder** on Apple silicon. |
| **vaapi** | Linux Intel Arc / AMD GPU | enc + dec ✅ | enc + dec ✅ | **dec** ✅ / enc 🚫 | **dec** ✅ / enc 🚫 | H.264 & H.265 encode+decode **verified** (incl. **H.264→H.265 transcode**); VP9/AV1 **decode verified**; VP9/AV1 **encode gated** — `NewEncoder` returns `ErrEncodeUnsupportedOnDriver` (the Intel iHD raw-VAAPI encode kernels reject the otherwise spec-conformant submission; the parameter builders are layout-verified and light up on a driver that fixes the entrypoint). |
| **v4l2** | Linux SoC (Raspberry Pi, Rockchip, Amlogic) | dec (stateful) | **dec** ✅ (stateless) + dec/enc (stateful) | — | — | Stateless HEVC decode (Request API) **verified bit-exact on Pi 5** (`rpi-hevc-dec`); the stateful M2M decode/encode state machine is **spec-correct but unverified** (no Pi 4 / Rockchip board on hand). Pure syscall — no userspace lib at all. |
| **nvenc / nvdec** | NVIDIA GPU (Linux) | enc + dec | enc + dec | **dec** | **enc + dec** | H.264/H.265/AV1 **encode** + H.264/H.265/VP9/AV1 **decode**, written against the Video Codec SDK 13.0 ABI with every marshalled struct's layout pinned in `nvenc_abi_linux_test.go`. **UNVERIFIED on hardware** — developed on a machine with no NVIDIA device; the live round-trip test `t.Skip`s when `Available()` is false. (AV1 encode needs Ada-class silicon; NVENC reports the GUID only on capable hardware.) |

Legend: ✅ verified · 🚫 gated (`ErrEncodeUnsupportedOnDriver`) · — not supported / no hardware path. NVENC/NVDEC cells are implemented-but-unverified (no ✅).

Selection prefers in registration order per platform: Linux registers
`nvenc` → `vaapi` → `v4l2`; macOS registers `videotoolbox`; every other platform
registers nothing (and `PreferHardware` falls back loudly). A host with both an
Intel iGPU and an NVIDIA dGPU registers both and the policy picks the first that
satisfies the requested codec.

## Policy modes

```go
type Policy struct {
    Mode Mode                                 // PreferHardware | RequireHardware | SoftwareOnly
    Bus  *events.Bus[HardwareFallbackEvent]   // optional sink for fallback notices
}
```

| Mode | Behaviour on no hardware backend |
|---|---|
| **`PreferHardware`** (zero value / default) | fall back to the software tier, **loudly**: publish a `HardwareFallbackEvent` on `Policy.Bus` (if set) *and* log a heavy `WARNING`. |
| **`RequireHardware`** | **do not** degrade — return `ErrHardwareUnavailable` wrapping the per-backend rejection reasons, leaving the decision to the caller. No event, no fallback. |
| **`SoftwareOnly`** | skip hardware entirely and go straight to the software tier. |

> **Note on the software tier.** The software seam is *defined* but not yet
> wired — a pure-Go / cgo-free libx264-class encoder can slot in later. Until
> then `openSoftware` returns `ErrNoBackend`, so on a host with no hardware
> backend: `PreferHardware` fires the fallback event/log **and then** returns
> `ErrNoBackend`; `SoftwareOnly` returns `ErrNoBackend` immediately;
> `RequireHardware` returns `ErrHardwareUnavailable`. The loud fallback path is
> fully live regardless — see the `probe` example.

## Opening an encoder / decoder

```go
import (
    "go-mediatoolkit/codec/hwaccel"
    "go-mediatoolkit/video"
)

// Encode: raw NV12 frames → H.265 packets, on hardware if available.
enc, err := hwaccel.OpenEncoder(
    hwaccel.Policy{Mode: hwaccel.PreferHardware},
    hwaccel.NewConfig(
        hwaccel.WithCodec(video.H265),
        hwaccel.WithResolution(1920, 1080),
        hwaccel.WithBitrate(6_000_000),
        hwaccel.WithFrameRate(30, 1),
        hwaccel.WithPixelFormat(video.NV12),
    ),
)
if err != nil { /* ErrNoBackend on a host with no hardware + no software tier */ }
defer enc.Close()

for _, f := range frames {
    pkts, err := enc.Encode(f) // 0..n packets (pipeline latency / parameter sets)
    // mux pkts...
}
tail, err := enc.Flush()       // drain at end of stream
```

A `Decoder` is symmetric: `OpenDecoder` with `WithCodec` (a decoder learns its
geometry from the stream's parameter sets, so resolution/bitrate are not
required), then `Decode(pkt) ([]video.Frame, error)` and `Flush()`.

`Config` is built from functional options (`NewConfig(...)`): `WithCodec`,
`WithResolution`, `WithBitrate`, `WithFrameRate`, `WithProfile`,
`WithKeyframeInterval`, `WithPixelFormat`. An encoder requires a known codec and
a non-zero resolution; a decoder requires only a known codec — anything else
returns `ErrInvalidConfig`.

## Observing fallback

```go
bus := hwaccel.NewFallbackBus()
bus.Subscribe(func(e hwaccel.HardwareFallbackEvent) {
    log.Printf("hardware %s for %s unavailable — fell back to %q; tried %v",
        e.Direction, e.Codec, e.FellBackTo, e.Attempted)
    // e.Reasons maps each attempted backend name → why it was rejected.
})

enc, err := hwaccel.OpenEncoder(
    hwaccel.Policy{Mode: hwaccel.PreferHardware, Bus: bus}, cfg)
```

The event carries the `Codec`, `Direction`, `Attempted` backend names, per-backend
`Reasons`, and `FellBackTo` (`"software"`, or `""` when even that failed and
`Open*` also returns `ErrNoBackend`). The loud `log` warning fires independently
of whether a bus is attached.

## Probing capabilities

`Available()` is a cheap gate (can we dlopen the lib / open the device node at
all?); `Probe()` is the truthful, possibly-expensive query of exactly which
codecs and directions the host supports, cached per backend.

```go
for _, b := range hwaccel.DefaultRegistry().Backends() {
    if !b.Available() {
        continue
    }
    caps, err := b.Probe()        // hwaccel.Capabilities
    if err != nil {
        continue
    }
    for _, c := range caps.Codecs { // c.Codec, c.Encode, c.Decode, c.Profiles
        // ...
    }
}
// or: hwaccel.DefaultRegistry().Probe() returns []Capabilities for every Available backend.
```

`Capabilities.Supports(codec, direction)` is the predicate the policy walk uses
to match a backend to a request.

## Examples

Standalone runnable binaries in [`examples/`](examples) — each builds on both
`linux` and `darwin` and exits cleanly (logging "no hardware codec available on
this host", exit 0) when the host has no usable hardware backend:

| Example | What it shows |
|---|---|
| [`probe`](examples/probe) | enumerate the registry's backends + their `Capabilities`; the fallback story. **Runs everywhere.** |
| [`encode`](examples/encode) | open an `Encoder`, feed synthetic NV12 frames, report packet count / bytes. |
| [`decode`](examples/decode) | self-encode a short stream, then open a `Decoder` and decode it back to `video.Frame`s, reporting resolution + frame count. |
| [`transcode`](examples/transcode) | the headline NVR path: decode an H.264 stream and re-encode it as H.265, entirely in hardware (input synthesised by the H.264 encoder so it is self-contained). |

```sh
go run ./codec/hwaccel/examples/probe
go run ./codec/hwaccel/examples/transcode
```

## API surface

- `OpenEncoder(p Policy, cfg Config) (Encoder, error)` / `OpenDecoder(...)` — the entry points (use the default registry).
- `Encoder` — `Encode(video.Frame) ([]video.Packet, error)`, `Flush() ([]video.Packet, error)`, `Close() error`.
- `Decoder` — `Decode(video.Packet) ([]video.Frame, error)`, `Flush() ([]video.Frame, error)`, `Close() error`.
- `Backend` — `Name()`, `Available()`, `Probe() (Capabilities, error)`, `NewEncoder`/`NewDecoder`.
- `Registry` — `DefaultRegistry()`, `Backends()`, `Get(name)`, `Probe()`.
- `Config` + `NewConfig(opts...)` and the `With*` options; `Policy` + `Mode`; `Capabilities` + `CodecCapability`; `HardwareFallbackEvent` + `NewFallbackBus()`.

### Errors (`hwaccel:` prefix)

- `ErrNoBackend` — no backend (hardware *or* software) can satisfy the request.
- `ErrHardwareUnavailable` — `RequireHardware` found no usable hardware backend (does not degrade).
- `ErrUnsupportedCodec` — backend asked for a codec it does not implement.
- `ErrInvalidConfig` — missing/contradictory `Config` fields (codec, resolution).
- `ErrEncodeUnsupportedOnDriver` — encode path not drivable on this host driver (VAAPI VP9/AV1 encode on Intel iHD).
- `ErrUnsupportedPixelFormat`, `ErrClosed`, `ErrBackendFailure`, `ErrParameterSetsMissing`, `ErrBitstreamParse` — see [`errors.go`](errors.go).
