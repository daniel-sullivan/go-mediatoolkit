// Large file example: stream many packets through Ogg using incremental
// page output, demonstrating how PageOut works for continuous encoding.
package main

import (
	"fmt"
	"log"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/ogg"
)

func main() {
	const (
		serialNo   int32 = 1
		numPackets       = 500
		frameSize        = 960 // samples per frame (20ms at 48kHz)
	)

	enc, _ := ogg.NewEncoder(serialNo)

	// Simulate encoding a long audio stream frame by frame.
	var raw []byte
	pageCount := 0
	totalBodyBytes := 0

	for i := 0; i < numPackets; i++ {
		// Simulate a codec frame (e.g. ~80 bytes of compressed audio).
		frame := make([]byte, 80)
		for j := range frame {
			frame[j] = byte((i + j) & 0xff)
		}

		pkt := ogg.Packet{
			Data:       frame,
			BOS:        i == 0,
			EOS:        i == numPackets-1,
			GranulePos: int64((i + 1) * frameSize),
			PacketNo:   int64(i),
		}
		if err := enc.PacketIn(&pkt); err != nil {
			log.Fatal(err)
		}

		// PageOut returns pages when enough data has accumulated.
		// Unlike Flush, it respects the internal page-fill threshold.
		for {
			page, ok := enc.PageOut()
			if !ok {
				break
			}
			raw = append(raw, page.Header...)
			raw = append(raw, page.Body...)
			totalBodyBytes += len(page.Body)
			pageCount++
		}
	}

	// Flush any remaining buffered data.
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
		totalBodyBytes += len(page.Body)
		pageCount++
	}

	fmt.Printf("encoded %d packets into %d pages\n", numPackets, pageCount)
	fmt.Printf("total: %d bytes (%d header overhead, %d body)\n",
		len(raw), len(raw)-totalBodyBytes, totalBodyBytes)
	fmt.Printf("overhead: %.1f%%\n", 100*float64(len(raw)-totalBodyBytes)/float64(len(raw)))

	// Verify by decoding all packets back.
	sync := ogg.NewSync()
	sync.Write(raw)

	dec, _ := ogg.NewDecoder(serialNo)
	for {
		page, ret, _ := sync.PageOut()
		if ret == 0 {
			break
		}
		if ret > 0 {
			dec.PageIn(&page)
		}
	}

	decoded := 0
	for {
		_, ret, _ := dec.PacketOut()
		if ret <= 0 {
			break
		}
		decoded++
	}

	fmt.Printf("decoded: %d/%d packets\n", decoded, numPackets)
}
