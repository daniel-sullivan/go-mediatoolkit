//go:build cgo

package ogg

import (
	"bytes"
	"io"
	"testing"
)

// closeOnCleanup releases any libogg C state owned by v (cgo Sync /
// Decoder / Encoder implement io.Closer) when the test finishes, so the
// ASan LeakSanitizer pass at process exit sees no leak. Pure-Go
// implementations don't implement io.Closer and are a no-op here.
func closeOnCleanup(t testing.TB, v any) {
	t.Helper()
	if c, ok := v.(io.Closer); ok {
		t.Cleanup(func() { c.Close() })
	}
}

// ── Cgo round-trip ──────────────────────────────────────────────────

func TestCgoRoundTrip(t *testing.T) {
	const serialNo int32 = 12345

	enc, _ := NewCgoEncoder(serialNo)
	sync := NewCgoSync()
	dec, _ := NewCgoDecoder(serialNo)
	closeOnCleanup(t, enc)
	closeOnCleanup(t, sync)
	closeOnCleanup(t, dec)

	packets := []Packet{
		{Data: []byte("hello"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("world"), GranulePos: 100, PacketNo: 1},
		{Data: []byte("foo bar baz"), GranulePos: 200, PacketNo: 2, EOS: true},
	}

	got := roundTrip(t, enc, sync, dec, packets)

	if len(got) != len(packets) {
		t.Fatalf("got %d packets, want %d", len(got), len(packets))
	}
	for i := range packets {
		if !bytes.Equal(got[i].Data, packets[i].Data) {
			t.Errorf("packet %d data: got %q, want %q", i, got[i].Data, packets[i].Data)
		}
	}
}

func TestCgoLargePacket(t *testing.T) {
	const serialNo int32 = 42

	enc, _ := NewCgoEncoder(serialNo)
	sync := NewCgoSync()
	dec, _ := NewCgoDecoder(serialNo)
	closeOnCleanup(t, enc)
	closeOnCleanup(t, sync)
	closeOnCleanup(t, dec)

	largeData := make([]byte, 70000)
	for i := range largeData {
		largeData[i] = byte(i & 0xff)
	}

	packets := []Packet{
		{Data: largeData, BOS: true, GranulePos: 0, PacketNo: 0, EOS: true},
	}

	got := roundTrip(t, enc, sync, dec, packets)
	if len(got) != 1 {
		t.Fatalf("got %d packets, want 1", len(got))
	}
	if !bytes.Equal(got[0].Data, largeData) {
		t.Errorf("large packet data mismatch")
	}
}

func TestCgoSyncRecovery(t *testing.T) {
	const serialNo int32 = 55

	enc, _ := NewEncoder(serialNo)
	pkt := Packet{Data: []byte("recovery test"), BOS: true, GranulePos: 100, PacketNo: 0, EOS: true}
	enc.PacketIn(&pkt)
	page, _ := enc.Flush()

	var raw []byte
	raw = append(raw, []byte("some garbage data before the real page")...)
	raw = append(raw, page.Header...)
	raw = append(raw, page.Body...)

	sync := NewCgoSync()
	closeOnCleanup(t, sync)
	sync.Write(raw)

	_, ret, _ := sync.PageOut()
	if ret != -1 {
		t.Fatalf("expected hole (-1), got %d", ret)
	}

	gotPage, ret, _ := sync.PageOut()
	if ret != 1 {
		t.Fatalf("expected page (1), got %d", ret)
	}

	dec, _ := NewCgoDecoder(serialNo)
	closeOnCleanup(t, dec)
	dec.PageIn(&gotPage)
	gotPkt, ret, _ := dec.PacketOut()
	if ret != 1 {
		t.Fatalf("expected packet (1), got %d", ret)
	}
	if !bytes.Equal(gotPkt.Data, pkt.Data) {
		t.Errorf("recovered packet: got %q, want %q", gotPkt.Data, pkt.Data)
	}
}

// ── Similarity: native vs Cgo produce byte-identical pages ──────────

func TestSimilarity_PagesBytesIdentical(t *testing.T) {
	const serialNo int32 = 333

	packets := []Packet{
		{Data: []byte("first packet"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("second packet"), GranulePos: 480, PacketNo: 1},
		{Data: []byte("third packet"), GranulePos: 960, PacketNo: 2, EOS: true},
	}

	cgoEnc, _ := NewCgoEncoder(serialNo)
	nativeEnc, _ := NewEncoder(serialNo)
	closeOnCleanup(t, cgoEnc)

	cgoRaw := encodeToRaw(t, cgoEnc, packets)
	nativeRaw := encodeToRaw(t, nativeEnc, packets)

	if !bytes.Equal(cgoRaw, nativeRaw) {
		t.Errorf("page bytes differ: cgo=%d bytes, native=%d bytes", len(cgoRaw), len(nativeRaw))
		minLen := len(cgoRaw)
		if len(nativeRaw) < minLen {
			minLen = len(nativeRaw)
		}
		for i := 0; i < minLen; i++ {
			if cgoRaw[i] != nativeRaw[i] {
				t.Errorf("first diff at byte %d: cgo=0x%02x native=0x%02x", i, cgoRaw[i], nativeRaw[i])
				break
			}
		}
	} else {
		t.Logf("page bytes identical: %d bytes", len(cgoRaw))
	}
}

func TestSimilarity_LargePacketPages(t *testing.T) {
	const serialNo int32 = 444

	data := make([]byte, 70000)
	for i := range data {
		data[i] = byte((i * 7) & 0xff)
	}

	packets := []Packet{
		{Data: data, BOS: true, GranulePos: 0, PacketNo: 0, EOS: true},
	}

	cgoEnc, _ := NewCgoEncoder(serialNo)
	nativeEnc, _ := NewEncoder(serialNo)
	closeOnCleanup(t, cgoEnc)

	cgoRaw := encodeToRaw(t, cgoEnc, packets)
	nativeRaw := encodeToRaw(t, nativeEnc, packets)

	if !bytes.Equal(cgoRaw, nativeRaw) {
		t.Errorf("large packet page bytes differ: cgo=%d, native=%d", len(cgoRaw), len(nativeRaw))
	} else {
		t.Logf("large packet pages identical: %d bytes", len(cgoRaw))
	}
}

func TestSimilarity_PageHeaderFields(t *testing.T) {
	const serialNo int32 = 555

	packets := []Packet{
		{Data: []byte("header field test"), BOS: true, GranulePos: 12345, PacketNo: 0, EOS: true},
	}

	cgoEnc, _ := NewCgoEncoder(serialNo)
	nativeEnc, _ := NewEncoder(serialNo)
	closeOnCleanup(t, cgoEnc)

	cgoPages := encodeToPages(t, cgoEnc, packets)
	nativePages := encodeToPages(t, nativeEnc, packets)

	if len(cgoPages) != len(nativePages) {
		t.Fatalf("page count differs: cgo=%d native=%d", len(cgoPages), len(nativePages))
	}

	for i := range cgoPages {
		cp := &cgoPages[i]
		np := &nativePages[i]

		if cp.Version() != np.Version() {
			t.Errorf("page %d Version: cgo=%d native=%d", i, cp.Version(), np.Version())
		}
		if cp.BOS() != np.BOS() {
			t.Errorf("page %d BOS: cgo=%v native=%v", i, cp.BOS(), np.BOS())
		}
		if cp.EOS() != np.EOS() {
			t.Errorf("page %d EOS: cgo=%v native=%v", i, cp.EOS(), np.EOS())
		}
		if cp.Continued() != np.Continued() {
			t.Errorf("page %d Continued: cgo=%v native=%v", i, cp.Continued(), np.Continued())
		}
		if cp.SerialNo() != np.SerialNo() {
			t.Errorf("page %d SerialNo: cgo=%d native=%d", i, cp.SerialNo(), np.SerialNo())
		}
		if cp.PageNo() != np.PageNo() {
			t.Errorf("page %d PageNo: cgo=%d native=%d", i, cp.PageNo(), np.PageNo())
		}
		if cp.GranulePos() != np.GranulePos() {
			t.Errorf("page %d GranulePos: cgo=%d native=%d", i, cp.GranulePos(), np.GranulePos())
		}
		if cp.Packets() != np.Packets() {
			t.Errorf("page %d Packets: cgo=%d native=%d", i, cp.Packets(), np.Packets())
		}
	}
}

func TestSimilarity_DecodedPackets(t *testing.T) {
	const serialNo int32 = 666

	packets := []Packet{
		{Data: []byte("similarity decode A"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("similarity decode B"), GranulePos: 480, PacketNo: 1},
		{Data: []byte("similarity decode C"), GranulePos: 960, PacketNo: 2, EOS: true},
	}

	enc, _ := NewEncoder(serialNo)
	raw := encodeToRaw(t, enc, packets)

	cgoSync := NewCgoSync()
	nativeSync := NewSync()
	closeOnCleanup(t, cgoSync)
	cgoSync.Write(raw)
	nativeSync.Write(raw)

	cgoDec, _ := NewCgoDecoder(serialNo)
	nativeDec, _ := NewDecoder(serialNo)
	closeOnCleanup(t, cgoDec)

	for {
		page, ret, _ := cgoSync.PageOut()
		if ret == 0 {
			break
		}
		if ret > 0 {
			cgoDec.PageIn(&page)
		}
	}
	for {
		page, ret, _ := nativeSync.PageOut()
		if ret == 0 {
			break
		}
		if ret > 0 {
			nativeDec.PageIn(&page)
		}
	}

	for i := 0; ; i++ {
		cgoPkt, cgoRet, _ := cgoDec.PacketOut()
		nativePkt, nativeRet, _ := nativeDec.PacketOut()

		if cgoRet != nativeRet {
			t.Fatalf("packet %d: cgo ret=%d, native ret=%d", i, cgoRet, nativeRet)
		}
		if cgoRet == 0 {
			break
		}
		if cgoRet < 0 {
			continue
		}

		if !bytes.Equal(cgoPkt.Data, nativePkt.Data) {
			t.Errorf("packet %d data mismatch", i)
		}
		if cgoPkt.BOS != nativePkt.BOS {
			t.Errorf("packet %d BOS: cgo=%v native=%v", i, cgoPkt.BOS, nativePkt.BOS)
		}
		if cgoPkt.EOS != nativePkt.EOS {
			t.Errorf("packet %d EOS: cgo=%v native=%v", i, cgoPkt.EOS, nativePkt.EOS)
		}
		if cgoPkt.GranulePos != nativePkt.GranulePos {
			t.Errorf("packet %d GranulePos: cgo=%d native=%d", i, cgoPkt.GranulePos, nativePkt.GranulePos)
		}
		if cgoPkt.PacketNo != nativePkt.PacketNo {
			t.Errorf("packet %d PacketNo: cgo=%d native=%d", i, cgoPkt.PacketNo, nativePkt.PacketNo)
		}
	}
}

// ── Cross-compatibility: encode with one, decode with the other ─────

func TestCrossCompat_CgoEncode_NativeDecode(t *testing.T) {
	const serialNo int32 = 777

	cgoEnc, _ := NewCgoEncoder(serialNo)
	nativeSync := NewSync()
	nativeDec, _ := NewDecoder(serialNo)
	closeOnCleanup(t, cgoEnc)

	packets := []Packet{
		{Data: []byte("cross-compat test"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("second packet"), GranulePos: 48000, PacketNo: 1, EOS: true},
	}

	got := roundTrip(t, cgoEnc, nativeSync, nativeDec, packets)
	if len(got) != len(packets) {
		t.Fatalf("got %d packets, want %d", len(got), len(packets))
	}
	for i := range packets {
		if !bytes.Equal(got[i].Data, packets[i].Data) {
			t.Errorf("packet %d: got %q, want %q", i, got[i].Data, packets[i].Data)
		}
	}
}

func TestCrossCompat_NativeEncode_CgoDecode(t *testing.T) {
	const serialNo int32 = 777

	nativeEnc, _ := NewEncoder(serialNo)
	cgoSync := NewCgoSync()
	cgoDec, _ := NewCgoDecoder(serialNo)
	closeOnCleanup(t, cgoSync)
	closeOnCleanup(t, cgoDec)

	packets := []Packet{
		{Data: []byte("cross-compat test"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("second packet"), GranulePos: 48000, PacketNo: 1, EOS: true},
	}

	got := roundTrip(t, nativeEnc, cgoSync, cgoDec, packets)
	if len(got) != len(packets) {
		t.Fatalf("got %d packets, want %d", len(got), len(packets))
	}
	for i := range packets {
		if !bytes.Equal(got[i].Data, packets[i].Data) {
			t.Errorf("packet %d: got %q, want %q", i, got[i].Data, packets[i].Data)
		}
	}
}

// ── Cgo benchmarks ──────────────────────────────────────────────────

func BenchmarkEncodeSmall_Cgo(b *testing.B) {
	benchEncodeCgo(b, 100)
}

func BenchmarkEncode4K_Cgo(b *testing.B) {
	benchEncodeCgo(b, 4000)
}

func BenchmarkEncode64K_Cgo(b *testing.B) {
	benchEncodeCgo(b, 64000)
}

func benchEncodeCgo(b *testing.B, pktSize int) {
	const serialNo int32 = 1
	data := make([]byte, pktSize)
	for i := range data {
		data[i] = byte(i)
	}

	b.SetBytes(int64(pktSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := NewCgoEncoder(serialNo)
		pkt := Packet{Data: data, BOS: true, EOS: true, GranulePos: 960, PacketNo: 0}
		enc.PacketIn(&pkt)
		for {
			_, ok := enc.Flush()
			if !ok {
				break
			}
		}
		enc.(io.Closer).Close()
	}
}

func BenchmarkSync4K_Cgo(b *testing.B) {
	benchSyncCgo(b, 4000)
}

func BenchmarkSync64K_Cgo(b *testing.B) {
	benchSyncCgo(b, 64000)
}

func benchSyncCgo(b *testing.B, pktSize int) {
	const serialNo int32 = 1

	enc, _ := NewEncoder(serialNo)
	data := make([]byte, pktSize)
	for i := range data {
		data[i] = byte(i)
	}
	pkt := Packet{Data: data, BOS: true, EOS: true, GranulePos: 960, PacketNo: 0}
	enc.PacketIn(&pkt)
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
		sync := NewCgoSync()
		sync.Write(raw)
		for {
			_, ret, _ := sync.PageOut()
			if ret == 0 {
				break
			}
		}
		sync.(io.Closer).Close()
	}
}

func BenchmarkRoundTrip4K_Cgo(b *testing.B) {
	const serialNo int32 = 1
	data := make([]byte, 4000)
	for i := range data {
		data[i] = byte(i)
	}

	enc, _ := NewEncoder(serialNo)
	pkt := Packet{Data: data, BOS: true, EOS: true, GranulePos: 960, PacketNo: 0}
	enc.PacketIn(&pkt)
	var raw []byte
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := NewCgoEncoder(serialNo)
		sync := NewCgoSync()
		dec, _ := NewCgoDecoder(serialNo)

		enc.PacketIn(&pkt)
		for {
			_, ok := enc.Flush()
			if !ok {
				break
			}
		}

		sync.Write(raw)
		for {
			page, ret, _ := sync.PageOut()
			if ret == 0 {
				break
			}
			if ret > 0 {
				dec.PageIn(&page)
			}
		}

		for {
			_, ret, _ := dec.PacketOut()
			if ret <= 0 {
				break
			}
		}

		enc.(io.Closer).Close()
		sync.(io.Closer).Close()
		dec.(io.Closer).Close()
	}
}
