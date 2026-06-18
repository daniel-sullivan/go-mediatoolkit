# ogg

Pure Go implementation of the Ogg bitstream format ([RFC 3533](https://www.rfc-editor.org/rfc/rfc3533)), with an optional Cgo path wrapping C libogg 1.3.6.

Ogg is a general-purpose container format for framing, error detection, and interleaving logical bitstreams (audio, video, etc.). This package provides the three core components:

- **Encoder** -- takes codec packets and produces Ogg pages
- **Sync** -- reads raw bytes and extracts Ogg pages (demuxing)
- **Decoder** -- reassembles pages into codec packets

## Usage

```go
import "go-mediatoolkit/libraries/ogg"
```

### Encoding packets into pages

```go
enc, _ := ogg.NewEncoder(serialNo)

enc.PacketIn(&ogg.Packet{
    Data:       opusFrame,
    BOS:        true,
    GranulePos: 960,
})

for {
    page, ok := enc.Flush()
    if !ok {
        break
    }
    // page.Header and page.Body contain the raw Ogg page
    out.Write(page.Header)
    out.Write(page.Body)
}
```

### Decoding pages into packets

```go
sync := ogg.NewSync()
sync.Write(rawBytes)

dec, _ := ogg.NewDecoder(serialNo)

for {
    page, ret, _ := sync.PageOut()
    if ret == 0 {
        break // need more data
    }
    if ret < 0 {
        continue // sync loss, try again
    }
    dec.PageIn(&page)
}

for {
    pkt, ret, _ := dec.PacketOut()
    if ret <= 0 {
        break
    }
    // pkt.Data contains the original codec packet
}
```

## Implementation

The default constructors (`NewSync`, `NewDecoder`, `NewEncoder`) are the pure Go implementation. When the toolchain has cgo enabled, the C libogg 1.3.6 path is available via `NewCgoSync`, `NewCgoDecoder`, and `NewCgoEncoder` — useful as a source-of-truth oracle for the Go port. No custom build tag is needed; the cgo discipline alone gates it.

| Constructor | Cgo enabled | Cgo disabled |
|---|---|---|
| `NewSync` / `NewDecoder` / `NewEncoder` | Native Go | Native Go |
| `NewCgoSync` / `NewCgoDecoder` / `NewCgoEncoder` | C libogg 1.3.6 | _unavailable_ |

To force the pure-Go path:

```sh
CGO_ENABLED=0 go build ./libraries/ogg/
```

## Runnable examples

Standalone runnable programs under [`examples/`](examples) — pure Go, no C toolchain needed:

| Example | What it shows |
|---|---|
| [roundtrip](examples/roundtrip/main.go) | encode three packets into pages, then sync + decode them back, printing per-page and per-packet fields |
| [multistream](examples/multistream/main.go) | interleave two logical bitstreams (audio + video serials) into one Ogg file, then demux them back by serial number |
| [largefile](examples/largefile/main.go) | stream 500 packets through incremental `PageOut`, reporting page count and header overhead |

```sh
go run ./libraries/ogg/examples/roundtrip
go run ./libraries/ogg/examples/multistream
go run ./libraries/ogg/examples/largefile
```

The parity and benchmark commands below additionally require cgo and a C compiler (they build the vendored C libogg oracle); the examples above do not.

### Benchmarks (Apple M3 Pro, arm64)

Native Go vs C libogg (called via Cgo), side-by-side. Sources: `ogg_test.go`, `ogg_cgo_test.go` (top-level package), and `benchcmp/bench_test.go` (independent harness). Numbers vary per machine:

| Operation | Native Go | C libogg (via Cgo) | Go/C |
|---|---|---|---|
| Encode small | 670 ns, 149 MB/s, 1 alloc | 4.6 µs, 22 MB/s, 7 allocs | **6.9×** |
| Encode 4K | 3.34 µs, 1198 MB/s, 1 alloc | 6.07 µs, 660 MB/s, 7 allocs | **1.8×** |
| Encode 64K | 43.2 µs, 1483 MB/s, 2 allocs | 52.6 µs, 1216 MB/s, 7 allocs | **1.2×** |
| Sync 4K | 3.05 µs, 1327 MB/s, 2 allocs | 4.59 µs, 881 MB/s, 5 allocs | **1.5×** |
| Sync 64K | 46.2 µs, 1391 MB/s, 2 allocs | 46.4 µs, 1387 MB/s, 5 allocs | **1.0×** |
| RoundTrip 4K | 7.53 µs, 531 MB/s, 4 allocs | 15.8 µs, 253 MB/s, 18 allocs | **2.1×** |
| CRC-32 4K | 2.14 µs, 1918 MB/s, 0 allocs | n/a | — |
| CRC-32 64K | 34.2 µs, 1915 MB/s, 0 allocs | n/a | — |

Native Go is faster across every operation. The advantage is largest for small pages, where the Cgo call overhead dominates the actual work.

Reproduce:

```sh
go test ./libraries/ogg/ -bench=. -benchmem -benchtime=500ms
go test ./libraries/ogg/benchcmp/ -bench=. -benchmem -benchtime=500ms
```

### Native ↔ C parity

C libogg is the oracle. `libraries/ogg/benchcmp/compare_test.go` compares both implementations bit-for-bit on representative inputs:

```sh
go test ./libraries/ogg/benchcmp/ -count=1
go test ./libraries/ogg/ -run='Cgo|Similarity|CrossCompat' -count=1
```

| Test | Path | Result |
|---|---|---|
| `TestBitExact_PageOutput` | Native encode vs C encode (small, large, zero-length packets) | **Bit-identical** page bytes |
| `TestBitExact_PacketDecode` | Same pages → Native decode vs C decode | **Identical** data, BOS, EOS, granulepos, packetno |
| `TestGoEncode_CDecode` | Native encode → C sync + decode | **Valid**, all packets recovered |
| `TestCgoRoundTrip` (top-level) | C enc → C sync → C dec | **Round-trip stable** |
| 70 KB packet pages | Native vs C, multi-page packet | **Bit-identical** |
| Page header fields | version, BOS, EOS, continued, serial, pageno, granulepos, packet count | **All match** |

## API

### Types

- `Page` -- raw Ogg page with `Header` and `Body` byte slices. Helper methods: `Version()`, `BOS()`, `EOS()`, `Continued()`, `GranulePos()`, `SerialNo()`, `PageNo()`, `Packets()`.
- `Packet` -- logical data packet with `Data`, `BOS`, `EOS`, `GranulePos`, `PacketNo`.

### Interfaces

- `Sync` -- `Write([]byte)`, `PageOut()`, `Reset()`
- `Decoder` -- `PageIn(*Page)`, `PacketOut()`, `PacketPeek()`, `SerialNo()`, `EOS()`, `Reset()`
- `Encoder` -- `PacketIn(*Packet)`, `PageOut()`, `Flush()`, `SerialNo()`, `EOS()`, `Reset()`, `ResetSerialNo(int32)`, `GranulePos()`

### Errors

- `ErrBadArg` -- invalid argument
- `ErrInternalError` -- internal library error
- `ErrStreamMismatch` -- page serial number doesn't match decoder
- `ErrBadVersion` -- unsupported Ogg stream version

## License

The vendored C libogg source (`libogg/`) is copyright Xiph.org Foundation under the BSD 3-clause license. See [`libogg/COPYING`](libogg/COPYING).
