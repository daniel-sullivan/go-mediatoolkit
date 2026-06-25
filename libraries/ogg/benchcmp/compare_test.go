//go:build cgo

package benchcmp

import (
	"bytes"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/ogg"
)

// ── Bit-exact page output: C encoder vs Go encoder ──────────────────

func TestBitExact_PageOutput(t *testing.T) {
	const serialNo = 12345

	// Same packets encoded by C and Go must produce identical bytes.
	testData := []struct {
		name string
		pkts []struct {
			data       []byte
			bos, eos   bool
			granulePos int64
		}
	}{
		{
			name: "small packets",
			pkts: []struct {
				data       []byte
				bos, eos   bool
				granulePos int64
			}{
				{[]byte("hello"), true, false, 0},
				{[]byte("world"), false, false, 480},
				{[]byte("end"), false, true, 960},
			},
		},
		{
			name: "single large packet",
			pkts: []struct {
				data       []byte
				bos, eos   bool
				granulePos int64
			}{
				{make([]byte, 5000), true, true, 48000},
			},
		},
		{
			name: "zero-length packet",
			pkts: []struct {
				data       []byte
				bos, eos   bool
				granulePos int64
			}{
				{[]byte{}, true, true, 0},
			},
		},
	}

	for _, tc := range testData {
		t.Run(tc.name, func(t *testing.T) {
			// C encoder.
			cEnc := NewCEncoder(serialNo)
			defer cEnc.Destroy()
			for i, p := range tc.pkts {
				cEnc.PacketIn(p.data, p.bos, p.eos, p.granulePos, int64(i))
			}
			var cRaw []byte
			for {
				h, b, ok := cEnc.Flush()
				if !ok {
					break
				}
				cRaw = append(cRaw, h...)
				cRaw = append(cRaw, b...)
			}

			// Go encoder.
			goEnc, _ := ogg.NewEncoder(serialNo)
			for i, p := range tc.pkts {
				goEnc.PacketIn(&ogg.Packet{
					Data:       p.data,
					BOS:        p.bos,
					EOS:        p.eos,
					GranulePos: p.granulePos,
					PacketNo:   int64(i),
				})
			}
			var goRaw []byte
			for {
				page, ok := goEnc.Flush()
				if !ok {
					break
				}
				goRaw = append(goRaw, page.Header...)
				goRaw = append(goRaw, page.Body...)
			}

			if !bytes.Equal(cRaw, goRaw) {
				t.Errorf("pages differ: C=%d bytes, Go=%d bytes", len(cRaw), len(goRaw))
				minLen := len(cRaw)
				if len(goRaw) < minLen {
					minLen = len(goRaw)
				}
				for i := 0; i < minLen; i++ {
					if cRaw[i] != goRaw[i] {
						t.Errorf("first diff at byte %d: C=0x%02x Go=0x%02x", i, cRaw[i], goRaw[i])
						break
					}
				}
			} else {
				t.Logf("bit-exact: %d bytes", len(cRaw))
			}
		})
	}
}

// ── Bit-exact packet decode: C decoder vs Go decoder ────────────────

func TestBitExact_PacketDecode(t *testing.T) {
	const serialNo = 54321

	// Encode with C, decode with both C and Go, compare packets.
	cEnc := NewCEncoder(serialNo)
	defer cEnc.Destroy()

	srcPackets := [][]byte{
		[]byte("packet one"),
		[]byte("packet two with more data"),
		[]byte("final"),
	}

	for i, data := range srcPackets {
		cEnc.PacketIn(data, i == 0, i == len(srcPackets)-1, int64(i*960), int64(i))
	}

	var raw []byte
	for {
		h, b, ok := cEnc.Flush()
		if !ok {
			break
		}
		raw = append(raw, h...)
		raw = append(raw, b...)
	}

	// C decode path.
	cSync := NewCSync()
	defer cSync.Destroy()
	cSync.Write(raw)

	cDec := NewCDecoder(serialNo)
	defer cDec.Destroy()

	for {
		h, b, ret := cSync.PageOut()
		if ret <= 0 {
			break
		}
		cDec.PageIn(h, b)
	}

	var cPackets [][]byte
	for {
		data, _, _, _, _, ret := cDec.PacketOut()
		if ret <= 0 {
			break
		}
		cPackets = append(cPackets, data)
	}

	// Go decode path.
	goSync := ogg.NewSync()
	goSync.Write(raw)

	goDec, _ := ogg.NewDecoder(serialNo)
	for {
		page, ret, _ := goSync.PageOut()
		if ret <= 0 {
			break
		}
		goDec.PageIn(&page)
	}

	var goPackets []ogg.Packet
	for {
		pkt, ret, _ := goDec.PacketOut()
		if ret <= 0 {
			break
		}
		goPackets = append(goPackets, pkt)
	}

	if len(cPackets) != len(goPackets) {
		t.Fatalf("packet count: C=%d Go=%d", len(cPackets), len(goPackets))
	}

	for i := range cPackets {
		if !bytes.Equal(cPackets[i], goPackets[i].Data) {
			t.Errorf("packet %d data mismatch: C=%d bytes, Go=%d bytes",
				i, len(cPackets[i]), len(goPackets[i].Data))
		} else {
			t.Logf("packet %d: bit-exact (%d bytes)", i, len(cPackets[i]))
		}
	}
}

// ── Cross: Go encode -> C decode ────────────────────────────────────

func TestGoEncode_CDecode(t *testing.T) {
	const serialNo = 9999

	goEnc, _ := ogg.NewEncoder(serialNo)

	srcData := [][]byte{
		[]byte("Go-encoded packet A"),
		[]byte("Go-encoded packet B"),
	}

	for i, data := range srcData {
		goEnc.PacketIn(&ogg.Packet{
			Data:       data,
			BOS:        i == 0,
			EOS:        i == len(srcData)-1,
			GranulePos: int64(i * 480),
			PacketNo:   int64(i),
		})
	}

	var raw []byte
	for {
		page, ok := goEnc.Flush()
		if !ok {
			break
		}
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
	}

	cSync := NewCSync()
	defer cSync.Destroy()
	cSync.Write(raw)

	cDec := NewCDecoder(serialNo)
	defer cDec.Destroy()

	for {
		h, b, ret := cSync.PageOut()
		if ret <= 0 {
			break
		}
		cDec.PageIn(h, b)
	}

	for i, want := range srcData {
		data, _, _, _, _, ret := cDec.PacketOut()
		if ret <= 0 {
			t.Fatalf("packet %d: C decode returned %d", i, ret)
		}
		if !bytes.Equal(data, want) {
			t.Errorf("packet %d: got %q, want %q", i, data, want)
		} else {
			t.Logf("packet %d: C decoded Go-encoded packet (%d bytes)", i, len(data))
		}
	}
}

// ── Summary ─────────────────────────────────────────────────────────

func TestSummary(t *testing.T) {
	t.Log("=== Ogg Go vs C Comparison ===")
	t.Log("")
	t.Log("BitExact_PageOutput:   C and Go encoders produce identical page bytes")
	t.Log("BitExact_PacketDecode: C and Go decoders extract identical packets")
	t.Log("GoEncode_CDecode:      Go-encoded pages are valid for C decoder")
	t.Log("")
	t.Log("Run benchmarks with: go test ./libraries/ogg/benchcmp/ -bench=.")
}
