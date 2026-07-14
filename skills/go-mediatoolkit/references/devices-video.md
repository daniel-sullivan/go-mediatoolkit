# Devices, runtime, and hardware video — devices, events, consts, inspection, codec/hwaccel, video

## devices — OS audio I/O (pure Go, no cgo)

One backend per platform: CoreAudio (macOS, purego dlopen), WASAPI (Windows, COM via `golang.org/x/sys`), PulseAudio (Linux, native protocol). Hotplug is native on Windows/Linux; polled on macOS (native listeners only when built with `CGO_ENABLED=1`).

```go
import "github.com/daniel-sullivan/go-mediatoolkit/devices"

sys, err := devices.GetSystem() // process-wide singleton; error means no backend, retrying won't help
defer sys.Close()

for _, d := range sys.List() {
    // d.ID (opaque, host-local), d.Name, d.Direction (Output/Input),
    // d.IsDefault, d.SampleRate/Channels (0 if unknown)
}
out, ok := sys.DefaultOutput() // (Device, false) if the OS reports no default
_ = ok

// Snapshot atomically returns the current list AND subscribes to changes —
// no event is missed or double-counted.
snap, sub := sys.Snapshot(func(ev devices.Event) {
    // ev.Kind: Added / Removed / DefaultChanged / PropertyChanged
})
defer sub.Cancel()
_ = snap

stream, err := sys.OpenOutput(out,
    devices.StreamFormat{SampleRate: 48000, Channels: 2}, // Frames: 0 → backend default
    func(buf []float64) {
        // REALTIME THREAD: no allocation, no contended locks, no I/O.
        // Zero every sample you don't fill; len(buf) == Frames*Channels.
        for i := range buf {
            buf[i] = 0
        }
    })
defer stream.Close()
actual := stream.Format() // the backend may negotiate a different format — always check
_ = actual
stream.Start() // stream begins idle; Stop() halts; Close() releases
```

- Capture mirrors output: `sys.OpenInput(dev, format, func(buf []float64) { ... })` — the input buffer is **reused across calls**; copy out anything that must outlive the callback. Bridge to processing code with `buffers.Ring` or `timeline.NewInputSource`.
- The idiomatic playback wiring is `mixer.Mixer.Fill` as the `OutputCallback` (a pure memcpy — realtime-safe by construction).
- Errors: `ErrBackendUnavailable`, `ErrNotSupported`, `ErrWrongDirection`, `ErrNilCallback`, `ErrDeviceNotFound`, `ErrInvalidFormat`, `ErrStreamClosed`.
- Only the `list` example is hardware-free; anything opening a stream needs real devices.

## events — typed pub-sub bus

`events.New[T]()` returns a `*Bus[T]`; `Subscribe(cb)` returns a `*Subscription[T]` with `Cancel()`. Delivery is **synchronous, in registration order, on the publisher's goroutine** — callbacks must be fast and non-blocking (spawn a goroutine for real work). Safe for concurrent use; callback panics are recovered and logged. Used by `devices` (hotplug), `vad` (`SpeechEvent`), `mixer` (track events), and `hwaccel` (fallback events).

## consts — named numbers

- `consts.SampleRate8000` ... `SampleRate192000` (`int`), and `consts.CommonSampleRates []int` (the matrix-tested set: 22050, 32000, 44100, 48000, 88200, 96000, 192000).
- `consts.FreqNoteC2` ... `FreqNoteB6` (`float64` Hz, equal temperament, A4 = 440; `S` marks sharps — `FreqNoteCS4` is C♯4); `consts.FreqNoteA` aliases `FreqNoteA4`.
- No channel-count constants — pass plain `1` / `2`.

## inspection — analysis utilities

Ad-hoc signal analysis (FFT, MDCT, autocorrelation, similarity) used mainly by the toolkit's own tests and tools. Reach for it for spectrum checks in tests; it is not a stable DSP-library surface like the packages above.

## Hardware video — codec/hwaccel + video

The ffmpeg model in pure Go: encode/decode on fixed-function silicon, **preferring hardware and falling back loudly, never silently**. Entirely `CGO_ENABLED=0` — vendor libraries are `dlopen`'d via purego at **runtime** (V4L2 is raw ioctl), so binaries build anywhere and backends report `Available() == false` on hosts that lack them.

### Data carriers (`video` package)

- `video.Frame` — raw planar YUV: `PixelFormat` (`video.NV12` = 2 planes, `video.I420` = 3), `Width`/`Height`, `Planes [][]byte` + `Strides []int` (parallel), `PTS time.Duration`. **Stride is not width** — walk rows honouring `Strides[i]`; when building frames to feed an encoder, tightest legal layout is stride == visible row width. NV12 is the hardware lingua franca.
- `video.Packet` — one compressed access unit: `Codec` (`video.H264`, `H265`, `VP9`, `AV1`), `Data []byte`, `Keyframe bool`, `PTS`/`DTS`. Framing of `Data` by codec: H.264/H.265 = **Annex-B** start-code NAL units, keyframes prefixed with parameter sets (SPS/PPS, +VPS for H.265); VP9 = raw frame/superframe (no start codes, no parameter sets); AV1 = one temporal unit of length-delimited OBUs (sequence header on keyframes).
- Ownership: plane/data slices transfer to the callee during a call — don't mutate a `Frame` you've handed to `Encode` until it returns.

### Opening encoders/decoders

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/codec/hwaccel"
    "github.com/daniel-sullivan/go-mediatoolkit/video"
)

enc, err := hwaccel.OpenEncoder(
    hwaccel.Policy{Mode: hwaccel.PreferHardware},
    hwaccel.NewConfig(
        hwaccel.WithCodec(video.H265),
        hwaccel.WithResolution(1920, 1080), // required for encoders
        hwaccel.WithBitrate(6_000_000),
        hwaccel.WithFrameRate(30, 1),
        hwaccel.WithPixelFormat(video.NV12),
    ),
)
defer enc.Close()

for _, f := range frames {
    pkts, err := enc.Encode(f) // 0..n packets — hardware pipelines are asynchronous
    _ = pkts; _ = err
}
tail, err := enc.Flush() // drain the pipeline at end of stream
_ = tail

// Decoder: only the codec is required (geometry comes from the bitstream).
dec, err := hwaccel.OpenDecoder(hwaccel.Policy{}, hwaccel.NewConfig(hwaccel.WithCodec(video.H264)))
// dec.Decode(pkt) ([]video.Frame, error); dec.Flush(); dec.Close()
```

Other options: `WithProfile(string)`, `WithKeyframeInterval(frames)`. Missing codec/resolution → `ErrInvalidConfig`.

### Policy modes

| Mode | On no usable hardware |
|---|---|
| `hwaccel.PreferHardware` (zero value) | falls back to the software tier **loudly**: publishes a `HardwareFallbackEvent` on `Policy.Bus` (create with `hwaccel.NewFallbackBus()`) and logs a heavy WARNING |
| `hwaccel.RequireHardware` | returns `ErrHardwareUnavailable`; never degrades |
| `hwaccel.SoftwareOnly` | skips hardware entirely |

**The software tier is a defined-but-unwired seam** — today, when no hardware backend works, `PreferHardware` fires the fallback event and then returns `ErrNoBackend` (`SoftwareOnly` returns it immediately). Plan for `ErrNoBackend` on hosts without supported silicon.

### Backends (verified status, per the repo's committed test results)

| Backend | Platform | Verified |
|---|---|---|
| `videotoolbox` | macOS Apple silicon | H.264/H.265 enc+dec; AV1 **decode**; no VP9 path, no VP9/AV1 encoder |
| `vaapi` | Linux Intel Arc / AMD | H.264/H.265 enc+dec (incl. H.264→H.265 transcode); VP9/AV1 **decode**; VP9/AV1 encode gated with `ErrEncodeUnsupportedOnDriver` on Intel iHD |
| `v4l2` | Linux SoC (Pi 5 etc.) | stateless HEVC decode bit-exact on Pi 5; stateful M2M paths spec-correct but unverified |
| `nvenc`/`nvdec` | NVIDIA Linux | implemented to Video Codec SDK 13.0 ABI, **unverified on hardware** |

Selection order on Linux: `nvenc` → `vaapi` → `v4l2`. Probe capabilities without opening:

```go
for _, b := range hwaccel.DefaultRegistry().Backends() {
    if !b.Available() { continue }        // cheap: can we dlopen / open the device node?
    caps, err := b.Probe()                // truthful per-codec encode/decode capability
    _ = caps; _ = err                     // caps.Supports(codec, direction)
}
```

There is **no pure-Go software video codec** in the toolkit, and no audio track handling in the video path — `containers/mp4` is audio-only (AAC); video muxing is out of scope.
