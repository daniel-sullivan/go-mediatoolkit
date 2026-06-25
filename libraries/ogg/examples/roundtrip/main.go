// Round-trip example: encode packets into Ogg pages, then decode them back.
package main

import (
	"fmt"
	"log"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/ogg"
)

func main() {
	const serialNo int32 = 42

	// Create an encoder for a logical bitstream.
	enc, err := ogg.NewEncoder(serialNo)
	if err != nil {
		log.Fatal(err)
	}

	// Submit some packets (simulating codec frames).
	packets := []ogg.Packet{
		{Data: []byte("first frame"), BOS: true, GranulePos: 960, PacketNo: 0},
		{Data: []byte("second frame"), GranulePos: 1920, PacketNo: 1},
		{Data: []byte("third frame"), EOS: true, GranulePos: 2880, PacketNo: 2},
	}

	for i := range packets {
		if err := enc.PacketIn(&packets[i]); err != nil {
			log.Fatalf("PacketIn(%d): %v", i, err)
		}
	}

	// Flush all buffered data into pages.
	var raw []byte
	pageCount := 0
	for {
		page, ok := enc.Flush()
		if !ok {
			break
		}
		fmt.Printf("page %d: %d header bytes, %d body bytes (BOS=%v EOS=%v granule=%d)\n",
			pageCount, len(page.Header), len(page.Body),
			page.BOS(), page.EOS(), page.GranulePos())
		raw = append(raw, page.Header...)
		raw = append(raw, page.Body...)
		pageCount++
	}
	fmt.Printf("\nencoded %d pages, %d total bytes\n\n", pageCount, len(raw))

	// Decode: feed raw bytes into Sync to extract pages.
	sync := ogg.NewSync()
	sync.Write(raw)

	dec, err := ogg.NewDecoder(serialNo)
	if err != nil {
		log.Fatal(err)
	}

	for {
		page, ret, err := sync.PageOut()
		if err != nil {
			log.Fatal(err)
		}
		if ret == 0 {
			break // no more pages
		}
		if ret < 0 {
			fmt.Println("sync loss detected, continuing...")
			continue
		}
		if err := dec.PageIn(&page); err != nil {
			log.Fatal(err)
		}
	}

	// Extract packets from the decoder.
	for i := 0; ; i++ {
		pkt, ret, err := dec.PacketOut()
		if err != nil {
			log.Fatal(err)
		}
		if ret == 0 {
			break
		}
		if ret < 0 {
			fmt.Printf("packet %d: hole in data\n", i)
			continue
		}
		fmt.Printf("packet %d: %q (BOS=%v EOS=%v granule=%d)\n",
			i, pkt.Data, pkt.BOS, pkt.EOS, pkt.GranulePos)
	}
}
