// record captures 5 seconds of audio from the default input device and
// writes it to a WAV file on disk. Run with
// `go run ./devices/examples/record /tmp/capture.wav`.
//
// The capture callback feeds into a byte-oriented writer via a
// channel-based queue: the audio thread enqueues a copy of the buffer
// and the main goroutine owns the disk write. This keeps the callback
// free of file I/O while still using simple, ordinary Go primitives.
package main

import (
	"log"
	"os"
	"time"

	"go-mediatoolkit/codec/pcm"
	"go-mediatoolkit/containers/wav"
	"go-mediatoolkit/devices"
	"go-mediatoolkit/mutations"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: record <output.wav>")
	}
	path := os.Args[1]

	sys, err := devices.GetSystem()
	if err != nil {
		log.Fatalf("GetSystem: %v", err)
	}
	defer sys.Close()

	in, ok := sys.DefaultInput()
	if !ok {
		log.Fatal("no default input device")
	}

	// Chunks flow from the audio callback to the file-writer
	// goroutine. The channel is buffered so brief scheduling jitter
	// doesn't drop samples; if it fills up the callback drops rather
	// than blocks, which is the right choice on a realtime thread.
	chunks := make(chan []float64, 32)

	cb := func(buf []float64) {
		cp := make([]float64, len(buf))
		copy(cp, buf)
		select {
		case chunks <- cp:
		default:
			log.Println("record: writer falling behind, dropped a chunk")
		}
	}

	stream, err := sys.OpenInput(in, devices.StreamFormat{SampleRate: 48000, Channels: 2}, cb)
	if err != nil {
		log.Fatalf("OpenInput: %v", err)
	}
	defer stream.Close()

	actual := stream.Format()
	log.Printf("recording from %q: rate=%d channels=%d frames=%d",
		in.Name, actual.SampleRate, actual.Channels, actual.Frames)

	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create: %v", err)
	}
	defer f.Close()

	header := wav.Header{
		Format:       "wav",
		SampleRate:   actual.SampleRate,
		Channels:     actual.Channels,
		SampleFormat: mutations.FormatInt16,
	}
	ww, err := wav.NewWriter(f, header)
	if err != nil {
		log.Fatalf("wav.NewWriter: %v", err)
	}
	enc, err := pcm.NewEncoder(ww.Data(), actual.SampleRate, actual.Channels, mutations.FormatInt16)
	if err != nil {
		log.Fatalf("pcm.NewEncoder: %v", err)
	}

	// The writer goroutine drains chunks until the channel closes.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range chunks {
			audio := mutations.Audio{Data: chunk, SampleRate: actual.SampleRate, Channels: actual.Channels}
			if _, err := enc.Write(audio); err != nil {
				log.Printf("record: encode: %v", err)
				return
			}
		}
	}()

	if err := stream.Start(); err != nil {
		log.Fatalf("Start: %v", err)
	}
	time.Sleep(5 * time.Second)
	if err := stream.Stop(); err != nil {
		log.Fatalf("Stop: %v", err)
	}
	close(chunks)
	<-done

	if err := enc.Close(); err != nil {
		log.Fatalf("enc.Close: %v", err)
	}
	if err := ww.Close(); err != nil {
		log.Fatalf("wav.Close: %v", err)
	}
	log.Printf("wrote %s", path)
}
