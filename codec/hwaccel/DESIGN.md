# Hardware codec offload (`codec/hwaccel`)

A backend-pluggable framework for **hardware-accelerated video encode/decode**
of H.264 and H.265, plus the per-platform accelerator bindings. The goal is the
NVR / transcode use case: take raw frames (or an incoming H.264 stream) and
re-encode to HEVC on whatever fixed-function silicon the host exposes, falling
back loudly — never silently — when no hardware path exists.

This is **video**, and structurally independent of the audio codecs in
`codec/{opus,flac,…}`. It only borrows their *model*: a software tier exists
conceptually, but the framework's reason to exist is the hardware tier.

The whole subsystem is **`CGO_ENABLED=0`**. Pure-syscall where the kernel ABI
is stable (V4L2); `purego` dlopen for the vendor userspace libraries
(VideoToolbox, VAAPI, NVENC). No cgo, ever.

## Package layout

```
video/                     shared types: Frame (NV12/I420 planar) + Packet (H264/H265 AU) + Codec/PixelFormat enums
codec/hwaccel/
  DESIGN.md                this file
  hwaccel.go               Encoder/Decoder + Backend interfaces, Registry, Open* entry points
  capabilities.go          Capabilities struct + CodecCapability
  config.go                functional-options Config (codec, resolution, bitrate, framerate, profile)
  policy.go                Policy (PreferHardware | RequireHardware | SoftwareOnly) + selection logic
  events.go                HardwareFallbackEvent + the package events.Bus[HardwareFallbackEvent]
  errors.go                sentinel errors (hwaccel: prefix)
  backend_darwin.go        registers the VideoToolbox backend (build tag darwin)
  backend_other.go         no-op registration (build tag !darwin … as backends land, tags tighten)
  videotoolbox_darwin.go   the VT backend: Backend + Encoder + Decoder impls
  vtbindings_darwin.go     purego dlopen of VideoToolbox/CoreMedia/CoreVideo/CoreFoundation
  videotoolbox_darwin_test.go   end-to-end encode test (runs on macOS arm64)
```

`video` is a separate top-level package (not under `codec/`) because the raw
`Frame` type is useful to capture/render code too and must not drag in the
backend machinery. `codec/hwaccel` depends on `video` and `events`; backends
depend on `purego` (dlopen) or `golang.org/x/sys/unix` (V4L2) only.

## Interfaces

```go
// Backend is one accelerator family (one per platform/vendor).
type Backend interface {
    Name() string                                  // "videotoolbox", "v4l2", "vaapi", "nvenc"
    Available() bool                               // cheap: can we dlopen / open the device node at all?
    Probe() (Capabilities, error)                  // detailed: which codecs, encode/decode, profiles
    NewEncoder(cfg Config) (video.Encoder, error)  // see video Encoder below
    NewDecoder(cfg Config) (video.Decoder, error)
}

// Encoder consumes raw frames, produces encoded packets.
type Encoder interface {
    Encode(f video.Frame) ([]video.Packet, error) // 0..n packets (latency / parameter sets)
    Flush() ([]video.Packet, error)               // drain reorder/lookahead buffers
    Close() error
}

// Decoder consumes encoded packets, produces raw frames.
type Decoder interface {
    Decode(p video.Packet) ([]video.Frame, error)
    Flush() ([]video.Frame, error)
    Close() error
}
```

`Encode`/`Decode` return **slices** because hardware codecs are pipelined: a
single `Encode` call may yield zero packets (frame buffered) or several (a
parameter-set packet plus the frame). `Flush` drains the pipeline at end of
stream. This differs from the audio `codec.Encoder` (synchronous, sample-count
return) on purpose — video frames are discrete and the hardware is async.

## Backend matrix

| Backend          | Platform / target            | Binding mechanism (CGO_ENABLED=0)            | Encode | Decode | Notes |
|------------------|------------------------------|----------------------------------------------|:------:|:------:|-------|
| **videotoolbox** | macOS / iOS (Apple Silicon)  | `purego` dlopen VideoToolbox+CoreMedia+CoreVideo+CoreFoundation | ✔ | ✔ | Implemented this pass. `VTCompressionSession` / `VTDecompressionSession`. HEVC where silicon supports it. |
| **v4l2**         | Linux SoC (RPi, Rockchip, Amlogic) | **pure syscall** via `golang.org/x/sys/unix` (`ioctl` on `/dev/videoN`) — no userspace lib at all | ✔ | ✔ | Stateful M2M (`V4L2_BUF_TYPE_VIDEO_{OUTPUT,CAPTURE}_MPLANE`), `VIDIOC_*` ioctls, MMAP/DMABUF buffers. Pure-Go because the ioctl ABI is stable kernel UAPI. |
| **vaapi**        | Linux desktop (Intel/AMD GPU)| `purego` dlopen `libva.so` + `libva-drm.so`  | ✔ | ✔ | Open DRM render node (`/dev/dri/renderD128`), `vaInitialize`, query `VAProfileHEVC*`/`VAEntrypointEncSlice`. |
| **nvenc**        | NVIDIA GPU (Linux + Windows) | `purego` dlopen `libnvidia-encode.so` / `nvEncodeAPI64.dll` + CUDA driver `libcuda.so` | ✔ | (NVDEC) | NVENC is a versioned C struct-API; fill `NV_ENC_*` structs, `nvEncOpenEncodeSessionEx`. Decode is a separate lib (`libnvcuvid`). |

Selection prefers in matrix order per platform; a host with both an Intel iGPU
and an NVIDIA dGPU will register `vaapi` and `nvenc` and the policy picks the
first that satisfies the requested codec.

## Capability probe

Each backend implements `Probe() (Capabilities, error)`:

```go
type Capabilities struct {
    Backend string
    Codecs  []CodecCapability
}
type CodecCapability struct {
    Codec    video.Codec
    Encode   bool
    Decode   bool
    Profiles []string   // e.g. "main", "high", "main10"
}
```

Probe is the **truthful, possibly-expensive** query; `Available()` is the cheap
gate (does the lib/device exist) used to skip a backend before paying for a full
probe. Per platform the probe maps to:

- **VideoToolbox** — encode: `VTCopySupportedPropertyDictionaryForEncoderSpecification`
  for each `CMVideoCodecType` returns non-nil ⇒ supported; decode:
  `VTIsHardwareDecodeSupported(codecType)`.
- **V4L2** — `VIDIOC_ENUM_FMT` on the output (coded) queue for the M2M device
  lists supported `V4L2_PIX_FMT_{H264,HEVC}`.
- **VAAPI** — `vaQueryConfigProfiles` + `vaQueryConfigEntrypoints` for
  `VAEntrypointEncSlice` / `VAEntrypointVLD`.
- **NVENC** — `nvEncGetEncodeGUIDs` / `nvEncGetEncodeProfileGUIDs`.

Probe results are cached per backend instance.

## Selection policy

```go
type Mode int
const ( PreferHardware Mode = iota; RequireHardware; SoftwareOnly )

type Policy struct {
    Mode Mode
    Bus  *events.Bus[HardwareFallbackEvent] // optional sink for fallback notices
}
```

`Open{Encoder,Decoder}(p Policy, cfg Config)` walks the registry:

1. **`SoftwareOnly`** — skip all hardware backends; go straight to the software
   tier. (Software tier is out of scope for this pass — currently returns
   `ErrNoBackend`; the seam is defined so a libx264-class pure-Go/cgo-free
   encoder can slot in later.)
2. **`PreferHardware`** (default) — try each registered hardware backend whose
   `Available()` is true and whose `Probe()` advertises the requested codec +
   direction; the first that constructs an encoder wins. If none works, **fall
   back** to software: publish a `HardwareFallbackEvent` on the policy's Bus
   *and* emit a heavy `log.Printf` WARNING, then attempt the software tier.
3. **`RequireHardware`** — same hardware walk, but on exhaustion **do not**
   degrade: return the `hwaccel:`-prefixed sentinel `ErrHardwareUnavailable`
   wrapping the per-backend reasons. No fallback, no silent software path.

## Fallback / warning semantics

A fallback is never silent. On any hardware-unavailable fallback under
`PreferHardware`:

```go
type HardwareFallbackEvent struct {
    Codec      video.Codec
    Direction  Direction          // Encode | Decode
    Mode       Mode
    Attempted  []string           // backend names tried
    Reasons    map[string]error   // why each failed
    FellBackTo string             // "software" or "" if none available
}
```

1. **Structured event** — published on `Policy.Bus` (if non-nil) so an
   application can surface "running on software, expect higher CPU" in its UI or
   metrics.
2. **Loud log** — a single multi-line `log.Printf` at WARNING volume naming the
   codec, the backends tried, and the chosen fallback. Heavy on purpose: a
   silent CPU-melting software fallback in an NVR is an operational trap.
3. **`RequireHardware`** suppresses both (no fallback occurs) and instead returns
   `ErrHardwareUnavailable`, leaving the decision to the caller.

## Why these choices

- **`Frame`/`Packet` in their own package** keeps capture/render free of the
  backend graph and mirrors the repo's interface-first rule.
- **Slice returns on Encode/Decode** model real hardware pipelining instead of
  pretending it's synchronous (the audio interface's assumption).
- **purego over cgo** holds the repo-wide `CGO_ENABLED=0` invariant; V4L2 needs
  no userspace lib so it's pure syscall, the cleanest possible binding.
- **Loud, structured fallback** is the single most important operational
  property for the NVR target and is therefore a first-class part of the API,
  not an afterthought.
