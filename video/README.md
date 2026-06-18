# video

The minimal shared types exchanged across the hardware-codec seam in
[`codec/hwaccel`](../codec/hwaccel). It carries **no codec logic** — only the
two data carriers that flow through every backend (VideoToolbox, VAAPI, V4L2,
NVENC/NVDEC) and the enums that tag them:

| Type | Direction | What it carries |
|---|---|---|
| **`Frame`** | raw / uncompressed | a planar YUV image (`NV12` or `I420`) + geometry + a presentation timestamp |
| **`Packet`** | compressed | one encoded access unit (codec-specific framing) + keyframe flag + PTS/DTS |
| `Codec` | — | the compressed bitstream format (`H264`, `H265`, `VP9`, `AV1`) |
| `PixelFormat` | — | the raw plane layout of a `Frame` (`NV12`, `I420`) |

`video` is a **separate top-level package** (not under `codec/`) on purpose: the
raw `Frame` type is useful to capture/render code too and must not drag in the
backend machinery. `codec/hwaccel` depends on `video`; the dependency does not
run the other way.

This is a **pure data-carrier package** — it holds only types and enums and has
**no executable behaviour** (no encode/decode logic, no examples of its own).
For runnable code that produces and consumes these `Frame`/`Packet` values, see
[`codec/hwaccel/examples`](../codec/hwaccel/examples).

## Where it sits in the pipeline

```
            decode                              encode
  Packet ─────────────►  Frame  ──(transform)──►  Frame ─────────────►  Packet
 (compressed)          (raw YUV)                 (raw YUV)            (compressed)
            hwaccel.Decoder                      hwaccel.Encoder
```

An `hwaccel.Decoder` consumes `Packet`s and yields `Frame`s; an
`hwaccel.Encoder` consumes `Frame`s and yields `Packet`s. Both are **pipelined**
(a single call may return zero or several items), so the carriers are plain
values with no lifecycle of their own — see
[`codec/hwaccel/README.md`](../codec/hwaccel/README.md).

## `Frame` — raw YUV

```go
type Frame struct {
    PixelFormat PixelFormat
    Width       int       // visible (coded) luma width
    Height      int       // visible (coded) luma height
    Planes      [][]byte  // one []byte per plane
    Strides     []int     // parallel: row stride (bytes) of each plane
    PTS         time.Duration
}
```

`Planes` and `Strides` are **parallel slices** of length `PixelFormat.Planes()`.
`Planes[i]` is plane *i*'s bytes; `Strides[i]` its row stride in bytes. The plane
count and meaning are fixed by the `PixelFormat`:

| Format | Planes | Layout |
|---|:---:|---|
| `NV12` | 2 | `[0]` = Y (`w×h`), `[1]` = interleaved Cb/Cr (`w×h/2`, one `(Cb,Cr)` byte pair per 2×2 luma block) |
| `I420` | 3 | `[0]` = Y (`w×h`), `[1]` = Cb (`w/2×h/2`), `[2]` = Cr (`w/2×h/2`). Also known as `YUV420P`. |

**Stride is not width.** A backend that requires aligned rows hands back a
`Frame` whose `Strides[i]` exceeds the visible plane width; consumers must walk
row by row honouring the stride, never assuming `stride == width`. When you
*build* a `Frame` to feed an encoder, the tightest legal layout is
`stride == visible row width` — that is what the bundled examples synthesise.

`NV12` is the lingua franca of fixed-function video hardware (it is what
VideoToolbox, VAAPI, NVENC and the V4L2 M2M nodes natively exchange), so it is
the format the examples use throughout.

## `Packet` — compressed access unit

```go
type Packet struct {
    Codec    Codec
    Data     []byte
    Keyframe bool          // IDR / sync sample: decodable without earlier frames
    PTS, DTS time.Duration // equal for codecs without B-frames
}
```

`Data` holds the bitstream in the codec's **native packaging**, which differs by
`Codec` — this is the single most important thing to get right when feeding a
decoder or muxing encoder output:

| Codec | Framing of `Data` |
|---|---|
| **`H264` / `H265`** | **Annex-B**: start-code-prefixed (`00 00 00 01`) NAL units. Keyframe packets are **prefixed with the parameter sets** (SPS/PPS for H.264; VPS/SPS/PPS for H.265) so the stream is decodable from any keyframe. The VideoToolbox encoder converts its length-prefixed AVCC/HVCC output to Annex-B for you. |
| **`VP9`** | one coded VP9 frame, or a **superframe** (a superframe-indexed concatenation of frames). VP9 is **not** Annex-B and has no start codes; `Data` is the raw header + tile bytes exactly as carried in an IVF frame payload or a WebM block. There are **no separate parameter sets** — every keyframe's uncompressed header is self-describing. |
| **`AV1`** | one **Temporal Unit**: a concatenation of length-delimited **OBUs** (`obu_has_size_field` set) for one displayable frame — the sequence-header OBU (on keyframes) plus frame/tile-group OBUs. AV1 is **not** Annex-B; `Data` is the raw OBU stream as carried in an IVF frame payload or a WebM block. Keyframe packets carry the sequence-header OBU so the stream is decodable from any keyframe. |

The `Keyframe` flag lets a muxer or a seeking decoder find resync points without
re-parsing the bitstream.

## Enums

`Codec.String()` → `"h264" | "h265" | "vp9" | "av1" | "unknown"`, and
`PixelFormat.String()` → `"nv12" | "i420" | "unknown"`; both match the tokens
used in capability reporting and configuration. `CodecUnknown` /
`PixelFormatUnknown` are the zero values and never name a real format —
`hwaccel.Config` validation rejects a zero `Codec`.

## Concurrency / ownership

`Frame` and `Packet` are plain value/slice carriers and are **not**
synchronised. Ownership of the backing slices transfers to the callee on an
`Encode`/`Decode`/`Write` call and back to the caller on the return; **do not
mutate a `Frame`'s planes after handing it to an encoder** until the call
returns.
