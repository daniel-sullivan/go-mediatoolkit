# devices

Cross-platform OS audio: **enumerate** the host's devices, get **hotplug**
notifications when they change, and open **capture** (input) / **render**
(output) streams that exchange interleaved **`float64`** samples in `[-1, 1]`
with a backend realtime callback. One pure-Go backend per platform — no cgo
required:

| Platform | Backend | Hotplug |
|---|---|---|
| **macOS** | CoreAudio via `purego` dlopen | polled; native property listeners when built with `CGO_ENABLED=1` |
| **Windows** | WASAPI via `golang.org/x/sys/windows` COM | native (MMNotificationClient) |
| **Linux** | PulseAudio via the native protocol | native (subscribe) |

The whole package is `CGO_ENABLED=0`-clean; on macOS, cgo only upgrades hotplug
from polling to real CoreAudio property listeners.

## Model

There is one process-wide [`System`], obtained via [`GetSystem`]. It loads the
initial device list at construction and runs an internal watcher (native events
where the backend supports them, a 2-second polling fallback otherwise) that
keeps a cache current. `System` is safe for concurrent use.

A [`Device`] is a snapshot of an OS endpoint: an opaque platform `ID` (stable
for the device's lifetime on this host, not portable across hosts), a `Name`, a
`Direction` (`Output` / `Input`), `IsDefault`, and the native `SampleRate` /
`Channels` (0 if unknown). [`List`] returns the current set; [`Snapshot`]
returns the set **and** atomically subscribes a callback to future [`Event`]s
(`Added` / `Removed` / `DefaultChanged` / `PropertyChanged`) so no change is
missed or double-counted.

Open a [`Stream`] with [`OpenOutput`] / [`OpenInput`], passing a target
`Device`, a requested [`StreamFormat`], and a callback. The stream starts idle —
call `Start` to begin driving the callback, `Stop` to halt, `Close` to release.
The callback runs on a backend-owned realtime thread and **must not allocate,
take contended locks, or do I/O**; output callbacks must explicitly zero any
samples they don't fill. The backend may negotiate a different format than
requested, so always read [`Stream.Format`] after opening.

## API tour

```go
sys, err := devices.GetSystem()           // process-wide; first call initialises the backend
if err != nil {
    log.Fatal(err)                         // no backend on this platform; retrying won't help
}
defer sys.Close()

for _, d := range sys.List() {             // current devices without subscribing
    fmt.Printf("[%s] %s (default=%v rate=%d ch=%d)\n",
        d.Direction, d.Name, d.IsDefault, d.SampleRate, d.Channels)
}

out, ok := sys.DefaultOutput()             // (Device, false) if the OS reports no default
if !ok {
    log.Fatal("no default output device")
}

// Snapshot atomically captures the list and subscribes to changes.
snap, sub := sys.Snapshot(func(ev devices.Event) {
    log.Printf("%s: %s", ev.Kind, ev.Device.Name) // runs on the publisher goroutine
})
defer sub.Cancel()
_ = snap

// Open a render stream. The callback runs on a realtime thread — no
// allocation, no locks, no I/O; zero any samples you don't fill.
stream, err := sys.OpenOutput(out,
    devices.StreamFormat{SampleRate: 48000, Channels: 2},
    func(buf []float64) {
        for i := range buf {
            buf[i] = 0 // silence; len(buf) == Frames*Channels
        }
    })
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

actual := stream.Format()                  // what the backend actually negotiated
_ = actual
stream.Start()
// ... play ...
stream.Stop()
```

Capture is the mirror image: [`OpenInput`] takes an [`InputCallback`] that
*receives* samples (the buffer is reused across calls — copy out anything that
must outlive the call).

### Surface

- **System** — [`GetSystem`]`() (*System, error)`; `List() []Device`;
  `Snapshot(cb func(Event)) ([]Device, *events.Subscription[Event])`;
  `DefaultOutput() (Device, bool)`; `DefaultInput() (Device, bool)`;
  `OpenOutput(Device, StreamFormat, OutputCallback) (Stream, error)`;
  `OpenInput(Device, StreamFormat, InputCallback) (Stream, error)`;
  `Close() error`.
- **Stream** — `Start() error`; `Stop() error`; `Close() error`;
  `Format() StreamFormat`.
- **Types** — `Device`, `Direction` (`Output`/`Input`), `Event`, `EventKind`,
  `StreamFormat` (`SampleRate`, `Channels`, `Frames` — 0 `Frames` accepts the
  backend default), `OutputCallback`, `InputCallback`.
- **Errors** — `ErrBackendUnavailable`, `ErrNotSupported`, `ErrWrongDirection`,
  `ErrNilCallback`, `ErrDeviceNotFound`, `ErrInvalidFormat`, `ErrStreamClosed`.

## Examples

Runnable, standalone programs under [`examples/`](examples). Only `list` is
hardware-free; the rest open real audio devices.

| Example | Command | Needs | Lifetime |
|---|---|---|---|
| [`list`](examples/list) | `go run ./devices/examples/list` | nothing (safe) | prints the device table and exits |
| [`play`](examples/play) | `go run ./devices/examples/play` | a default **output** device | renders a 440 Hz sine, exits after ~3s |
| [`record`](examples/record) | `go run ./devices/examples/record /tmp/capture.wav` | a default **input** (mic) + a writable path arg | captures ~5s to a WAV, then exits |
| [`watch`](examples/watch) | `go run ./devices/examples/watch` | audio hardware | prints device changes; runs until Ctrl-C |
| [`echo`](examples/echo) | `go run ./devices/examples/echo` | a default **input** + **output** | loops mic → speaker via a ring buffer; runs until Ctrl-C |

`record` requires an output **path argument** (`record <output.wav>`) and exits
with a usage message without one.

## License

This package is **MIT**. It links no codec engines and carries no license fence
— see [`LICENSING.md`](../LICENSING.md). For the streaming `float64` convention
shared with the rest of the toolkit, see the top-level [README](../README.md).
