// Multi-stream example: interleave two logical bitstreams into one Ogg file,
// then demux them back into separate streams.
package main

import (
	"fmt"
	"log"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/ogg"
)

func main() {
	const (
		audioSerial int32 = 100
		videoSerial int32 = 200
	)

	audioEnc, _ := ogg.NewEncoder(audioSerial)
	videoEnc, _ := ogg.NewEncoder(videoSerial)

	// Encode audio packets.
	audioPackets := []ogg.Packet{
		{Data: []byte("audio-header"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("audio-frame-1"), GranulePos: 960, PacketNo: 1},
		{Data: []byte("audio-frame-2"), EOS: true, GranulePos: 1920, PacketNo: 2},
	}
	for i := range audioPackets {
		audioEnc.PacketIn(&audioPackets[i])
	}

	// Encode video packets.
	videoPackets := []ogg.Packet{
		{Data: []byte("video-header"), BOS: true, GranulePos: 0, PacketNo: 0},
		{Data: []byte("video-frame-1"), GranulePos: 1, PacketNo: 1},
		{Data: []byte("video-frame-2"), EOS: true, GranulePos: 2, PacketNo: 2},
	}
	for i := range videoPackets {
		videoEnc.PacketIn(&videoPackets[i])
	}

	// Interleave: BOS pages first (Ogg spec requirement), then data pages.
	var raw []byte
	flushAll := func(enc ogg.Encoder) {
		for {
			page, ok := enc.Flush()
			if !ok {
				break
			}
			raw = append(raw, page.Header...)
			raw = append(raw, page.Body...)
		}
	}
	flushAll(audioEnc)
	flushAll(videoEnc)

	fmt.Printf("interleaved stream: %d bytes\n\n", len(raw))

	// Demux: use Sync to extract pages, then route by serial number.
	sync := ogg.NewSync()
	sync.Write(raw)

	audioDec, _ := ogg.NewDecoder(audioSerial)
	videoDec, _ := ogg.NewDecoder(videoSerial)

	for {
		page, ret, _ := sync.PageOut()
		if ret == 0 {
			break
		}
		if ret < 0 {
			continue
		}

		// Route page to the correct decoder by serial number.
		switch page.SerialNo() {
		case audioSerial:
			if err := audioDec.PageIn(&page); err != nil {
				log.Fatal(err)
			}
		case videoSerial:
			if err := videoDec.PageIn(&page); err != nil {
				log.Fatal(err)
			}
		default:
			fmt.Printf("unknown stream serial=%d, skipping\n", page.SerialNo())
		}
	}

	// Read packets from each stream.
	fmt.Println("audio stream:")
	drainPackets(audioDec)

	fmt.Println("\nvideo stream:")
	drainPackets(videoDec)
}

func drainPackets(dec ogg.Decoder) {
	for i := 0; ; i++ {
		pkt, ret, _ := dec.PacketOut()
		if ret <= 0 {
			break
		}
		fmt.Printf("  packet %d: %q (granule=%d)\n", i, pkt.Data, pkt.GranulePos)
	}
}
