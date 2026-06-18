package opus

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

func loadPackets(path string) [][]byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var packets [][]byte
	for len(data) >= 4 {
		pktLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if pktLen > len(data) {
			break
		}
		packets = append(packets, data[:pktLen])
		data = data[pktLen:]
	}
	return packets
}

func BenchmarkDecodeCELT20ms(b *testing.B) {
	packets := loadPackets("testdata/celt_mono_48k_20ms.pkt")
	if len(packets) == 0 {
		b.Skip("no test vectors")
	}
	dec, _ := NewDecoder(48000, 1)
	pcm := make([]float64, MaxFrameSize(48000))
	pkt := packets[2]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, pcm)
	}
}

func BenchmarkDecodeSILK20ms(b *testing.B) {
	packets := loadPackets("testdata/silk_mono_48k_20ms.pkt")
	if len(packets) == 0 {
		b.Skip("no test vectors")
	}
	dec, _ := NewDecoder(48000, 1)
	pcm := make([]float64, MaxFrameSize(48000))
	pkt := packets[2]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.Decode(pkt, pcm)
	}
}

func BenchmarkEncodeCELT20ms(b *testing.B) {
	enc, _ := NewEncoder(48000, 1)
	pcm := make([]float64, 960)
	for i := range pcm {
		pcm[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/48000)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc.Encode(pcm, 1275)
	}
}
