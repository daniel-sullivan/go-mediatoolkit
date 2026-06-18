package ogg

import (
	"bytes"
	"testing"
)

// roundTrip encodes packets into pages via an Encoder, then decodes them
// back via Sync + Decoder, and returns the recovered packets.
func roundTrip(t testing.TB, enc Encoder, sync Sync, dec Decoder, packets []Packet) []Packet {
	t.Helper()

	var pageData []byte
	for i := range packets {
		if err := enc.PacketIn(&packets[i]); err != nil {
			t.Fatalf("PacketIn(%d): %v", i, err)
		}
	}
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		pageData = append(pageData, page.Header...)
		pageData = append(pageData, page.Body...)
	}

	n, err := sync.Write(pageData)
	if err != nil {
		t.Fatalf("sync.Write: %v", err)
	}
	if n != len(pageData) {
		t.Fatalf("sync.Write: wrote %d, want %d", n, len(pageData))
	}

	for {
		page, ret, err := sync.PageOut()
		if err != nil {
			t.Fatalf("sync.PageOut: %v", err)
		}
		if ret == 0 {
			break
		}
		if ret < 0 {
			continue
		}
		if err := dec.PageIn(&page); err != nil {
			t.Fatalf("dec.PageIn: %v", err)
		}
	}

	var result []Packet
	for {
		pkt, ret, err := dec.PacketOut()
		if err != nil {
			t.Fatalf("dec.PacketOut: %v", err)
		}
		if ret == 0 {
			break
		}
		if ret < 0 {
			continue
		}
		result = append(result, pkt)
	}
	return result
}

// encodeToRaw encodes packets and returns the concatenated page bytes.
func encodeToRaw(t testing.TB, enc Encoder, packets []Packet) []byte {
	t.Helper()
	for i := range packets {
		if err := enc.PacketIn(&packets[i]); err != nil {
			t.Fatalf("PacketIn(%d): %v", i, err)
		}
	}
	var raw []byte
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
	}
	return raw
}

// encodeToPages encodes packets and returns pages individually.
func encodeToPages(t testing.TB, enc Encoder, packets []Packet) []Page {
	t.Helper()
	for i := range packets {
		if err := enc.PacketIn(&packets[i]); err != nil {
			t.Fatalf("PacketIn(%d): %v", i, err)
		}
	}
	var pages []Page
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		pages = append(pages, page)
	}
	return pages
}

// ── Round-trip tests ────────────────────────────────────────────────

func TestRoundTrip(t *testing.T) {
	const serialNo int32 = 12345

	enc, err := NewEncoder(serialNo)
	if err != nil {
		t.Fatal(err)
	}
	sync := NewSync()
	dec, err := NewDecoder(serialNo)
	if err != nil {
		t.Fatal(err)
	}

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
		if got[i].BOS != packets[i].BOS {
			t.Errorf("packet %d BOS: got %v, want %v", i, got[i].BOS, packets[i].BOS)
		}
		if got[i].EOS != packets[i].EOS {
			t.Errorf("packet %d EOS: got %v, want %v", i, got[i].EOS, packets[i].EOS)
		}
	}

	last := got[len(got)-1]
	if last.GranulePos != 200 {
		t.Errorf("last packet granulepos: got %d, want 200", last.GranulePos)
	}
}

// ── Large packet tests ──────────────────────────────────────────────

func TestLargePacket(t *testing.T) {
	const serialNo int32 = 42

	enc, _ := NewEncoder(serialNo)
	sync := NewSync()
	dec, _ := NewDecoder(serialNo)

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
		t.Errorf("large packet data mismatch: got %d bytes, want %d", len(got[0].Data), len(largeData))
	}
}

// ── Page helper tests ───────────────────────────────────────────────

func TestPageHelpers(t *testing.T) {
	const serialNo int32 = 99

	enc, _ := NewEncoder(serialNo)

	pkt := Packet{Data: []byte("test data"), BOS: true, GranulePos: 42, PacketNo: 0}
	enc.PacketIn(&pkt)

	page, ok := enc.Flush()
	if !ok {
		t.Fatal("expected a page from Flush")
	}

	if page.Version() != 0 {
		t.Errorf("Version: got %d, want 0", page.Version())
	}
	if !page.BOS() {
		t.Error("BOS: expected true")
	}
	if page.EOS() {
		t.Error("EOS: expected false")
	}
	if page.Continued() {
		t.Error("Continued: expected false")
	}
	if page.SerialNo() != serialNo {
		t.Errorf("SerialNo: got %d, want %d", page.SerialNo(), serialNo)
	}
	if page.PageNo() != 0 {
		t.Errorf("PageNo: got %d, want 0", page.PageNo())
	}
	if page.GranulePos() != 0 {
		t.Errorf("GranulePos: got %d, want 0", page.GranulePos())
	}
	if page.Packets() != 1 {
		t.Errorf("Packets: got %d, want 1", page.Packets())
	}
}

// ── Sync recovery tests ────────────────────────────────────────────

func TestSyncRecovery(t *testing.T) {
	const serialNo int32 = 55

	enc, _ := NewEncoder(serialNo)
	pkt := Packet{Data: []byte("recovery test"), BOS: true, GranulePos: 100, PacketNo: 0, EOS: true}
	enc.PacketIn(&pkt)
	page, _ := enc.Flush()

	var raw []byte
	raw = append(raw, []byte("some garbage data before the real page")...)
	raw = append(raw, page.Header...)
	raw = append(raw, page.Body...)

	sync := NewSync()
	sync.Write(raw)

	_, ret, _ := sync.PageOut()
	if ret != -1 {
		t.Fatalf("expected hole (-1), got %d", ret)
	}

	gotPage, ret, _ := sync.PageOut()
	if ret != 1 {
		t.Fatalf("expected page (1), got %d", ret)
	}

	dec, _ := NewDecoder(serialNo)
	dec.PageIn(&gotPage)
	gotPkt, ret, _ := dec.PacketOut()
	if ret != 1 {
		t.Fatalf("expected packet (1), got %d", ret)
	}
	if !bytes.Equal(gotPkt.Data, pkt.Data) {
		t.Errorf("recovered packet: got %q, want %q", gotPkt.Data, pkt.Data)
	}
}

// ── CRC-32 test ─────────────────────────────────────────────────────

func TestCRC32(t *testing.T) {
	data := []byte("OggS")
	crc := oggCRC32(0, data)
	if crc == 0 {
		t.Error("CRC of 'OggS' should not be zero")
	}

	crc = oggCRC32(0, nil)
	if crc != 0 {
		t.Errorf("CRC of nil: got 0x%08x, want 0", crc)
	}

	// Verify slicing-by-8 matches byte-at-a-time for a longer buffer.
	longData := make([]byte, 1024)
	for i := range longData {
		longData[i] = byte(i * 37)
	}
	crc1 := oggCRC32(0, longData)
	var crc2 uint32
	for _, b := range longData {
		crc2 = (crc2 << 8) ^ crcTable[0][(crc2>>24)^uint32(b)]
	}
	if crc1 != crc2 {
		t.Errorf("slicing-by-8 vs byte-at-a-time: 0x%08X != 0x%08X", crc1, crc2)
	}
}

// ── Many-packet stress test ─────────────────────────────────────────

func TestManyPackets(t *testing.T) {
	const serialNo int32 = 888
	const numPackets = 200

	enc, _ := NewEncoder(serialNo)
	sync := NewSync()
	dec, _ := NewDecoder(serialNo)

	packets := make([]Packet, numPackets)
	for i := range packets {
		data := make([]byte, 100+i*3)
		for j := range data {
			data[j] = byte((i + j) & 0xff)
		}
		packets[i] = Packet{
			Data:       data,
			BOS:        i == 0,
			EOS:        i == numPackets-1,
			GranulePos: int64(i * 960),
			PacketNo:   int64(i),
		}
	}

	got := roundTrip(t, enc, sync, dec, packets)

	if len(got) != numPackets {
		t.Fatalf("got %d packets, want %d", len(got), numPackets)
	}
	for i := range packets {
		if !bytes.Equal(got[i].Data, packets[i].Data) {
			t.Errorf("packet %d: data mismatch (%d vs %d bytes)", i, len(got[i].Data), len(packets[i].Data))
		}
	}
}

// ── Packet sizes test (various edge cases) ──────────────────────────

func TestPacketSizes(t *testing.T) {
	const serialNo int32 = 999

	sizes := []int{0, 1, 254, 255, 256, 510, 4080, 65025}

	for _, size := range sizes {
		enc, _ := NewEncoder(serialNo)
		sync := NewSync()
		dec, _ := NewDecoder(serialNo)

		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i)
		}

		packets := []Packet{
			{Data: data, BOS: true, GranulePos: int64(size), PacketNo: 0, EOS: true},
		}

		got := roundTrip(t, enc, sync, dec, packets)
		if len(got) != 1 {
			t.Errorf("size %d: got %d packets, want 1", size, len(got))
			continue
		}
		if !bytes.Equal(got[0].Data, data) {
			t.Errorf("size %d: data mismatch", size)
		}
	}
}

// ── Benchmarks ──────────────────────────────────────────────────────

func BenchmarkEncodeSmall(b *testing.B) {
	benchEncode(b, 100)
}

func BenchmarkEncode4K(b *testing.B) {
	benchEncode(b, 4000)
}

func BenchmarkEncode64K(b *testing.B) {
	benchEncode(b, 64000)
}

func benchEncode(b *testing.B, pktSize int) {
	const serialNo int32 = 1
	data := make([]byte, pktSize)
	for i := range data {
		data[i] = byte(i)
	}

	b.SetBytes(int64(pktSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := NewEncoder(serialNo)
		pkt := Packet{Data: data, BOS: true, EOS: true, GranulePos: 960, PacketNo: 0}
		enc.PacketIn(&pkt)
		for {
			_, ok := enc.Flush()
			if !ok {
				break
			}
		}
	}
}

func BenchmarkSync4K(b *testing.B) {
	benchSync(b, 4000)
}

func BenchmarkSync64K(b *testing.B) {
	benchSync(b, 64000)
}

func benchSync(b *testing.B, pktSize int) {
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
		sync := NewSync()
		sync.Write(raw)
		for {
			_, ret, _ := sync.PageOut()
			if ret == 0 {
				break
			}
		}
	}
}

func BenchmarkRoundTrip4K(b *testing.B) {
	benchRoundTrip(b, 4000)
}

func benchRoundTrip(b *testing.B, pktSize int) {
	const serialNo int32 = 1

	data := make([]byte, pktSize)
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

	b.SetBytes(int64(pktSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		enc, _ := NewEncoder(serialNo)
		sync := NewSync()
		dec, _ := NewDecoder(serialNo)

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
	}
}

func BenchmarkCRC32_4K(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		oggCRC32(0, data)
	}
}

func BenchmarkCRC32_64K(b *testing.B) {
	data := make([]byte, 65536)
	for i := range data {
		data[i] = byte(i)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		oggCRC32(0, data)
	}
}
