//go:build cgo

package benchcmp

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/ogg"
)

func makeTestData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 37)
	}
	return data
}

// ── Encode benchmarks ───────────────────────────────────────────────

func BenchmarkEncode4K_C(b *testing.B) {
	data := makeTestData(4000)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc := NewCEncoder(1)
		enc.PacketIn(data, true, true, 960, 0)
		for {
			_, _, ok := enc.Flush()
			if !ok {
				break
			}
		}
		enc.Destroy()
	}
}

func BenchmarkEncode4K_Go(b *testing.B) {
	data := makeTestData(4000)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := ogg.NewEncoder(1)
		enc.PacketIn(&ogg.Packet{Data: data, BOS: true, EOS: true, GranulePos: 960})
		for {
			_, ok := enc.Flush()
			if !ok {
				break
			}
		}
	}
}

func BenchmarkEncode64K_C(b *testing.B) {
	data := makeTestData(64000)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc := NewCEncoder(1)
		enc.PacketIn(data, true, true, 960, 0)
		for {
			_, _, ok := enc.Flush()
			if !ok {
				break
			}
		}
		enc.Destroy()
	}
}

func BenchmarkEncode64K_Go(b *testing.B) {
	data := makeTestData(64000)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := ogg.NewEncoder(1)
		enc.PacketIn(&ogg.Packet{Data: data, BOS: true, EOS: true, GranulePos: 960})
		for {
			_, ok := enc.Flush()
			if !ok {
				break
			}
		}
	}
}

// ── Sync benchmarks ────────────────────────────────────────────────

func BenchmarkSync4K_C(b *testing.B) {
	benchSyncC(b, 4000)
}

func BenchmarkSync64K_C(b *testing.B) {
	benchSyncC(b, 64000)
}

func benchSyncC(b *testing.B, pktSize int) {
	// Pre-encode pages.
	enc := NewCEncoder(1)
	data := makeTestData(pktSize)
	enc.PacketIn(data, true, true, 960, 0)
	var raw []byte
	for {
		h, bd, ok := enc.Flush()
		if !ok {
			break
		}
		raw = append(raw, h...)
		raw = append(raw, bd...)
	}
	enc.Destroy()

	b.SetBytes(int64(len(raw)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s := NewCSync()
		s.Write(raw)
		for {
			_, _, ret := s.PageOut()
			if ret <= 0 {
				break
			}
		}
		s.Destroy()
	}
}

func BenchmarkSync4K_Go(b *testing.B) {
	benchSyncGo(b, 4000)
}

func BenchmarkSync64K_Go(b *testing.B) {
	benchSyncGo(b, 64000)
}

func benchSyncGo(b *testing.B, pktSize int) {
	enc, _ := ogg.NewEncoder(1)
	data := makeTestData(pktSize)
	enc.PacketIn(&ogg.Packet{Data: data, BOS: true, EOS: true, GranulePos: 960})
	var raw []byte
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
	}

	b.SetBytes(int64(len(raw)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s := ogg.NewSync()
		s.Write(raw)
		for {
			_, ret, _ := s.PageOut()
			if ret <= 0 {
				break
			}
		}
	}
}

// ── Decode benchmarks ───────────────────────────────────────────────

func BenchmarkDecode4K_C(b *testing.B) {
	benchDecodeC(b, 4000)
}

func BenchmarkDecode4K_Go(b *testing.B) {
	benchDecodeGo(b, 4000)
}

func benchDecodeC(b *testing.B, pktSize int) {
	// Pre-encode pages and extract header/body pairs.
	enc := NewCEncoder(1)
	data := makeTestData(pktSize)
	enc.PacketIn(data, true, true, 960, 0)
	type pageParts struct{ h, b []byte }
	var pages []pageParts
	for {
		h, bd, ok := enc.Flush()
		if !ok {
			break
		}
		pages = append(pages, pageParts{h, bd})
	}
	enc.Destroy()

	b.SetBytes(int64(pktSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dec := NewCDecoder(1)
		for _, p := range pages {
			dec.PageIn(p.h, p.b)
		}
		for {
			_, _, _, _, _, ret := dec.PacketOut()
			if ret <= 0 {
				break
			}
		}
		dec.Destroy()
	}
}

func benchDecodeGo(b *testing.B, pktSize int) {
	enc, _ := ogg.NewEncoder(1)
	data := makeTestData(pktSize)
	enc.PacketIn(&ogg.Packet{Data: data, BOS: true, EOS: true, GranulePos: 960})
	var pages []ogg.Page
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		pages = append(pages, page)
	}

	b.SetBytes(int64(pktSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dec, _ := ogg.NewDecoder(1)
		for j := range pages {
			dec.PageIn(&pages[j])
		}
		for {
			_, ret, _ := dec.PacketOut()
			if ret <= 0 {
				break
			}
		}
	}
}
