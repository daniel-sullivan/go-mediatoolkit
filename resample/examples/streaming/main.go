// Example: streaming sample rate conversion using Process().
//
// Demonstrates processing audio in fixed-size chunks, as you would when
// reading from a file or network stream. Converts 44100 Hz to 48000 Hz.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

func main() {
	srcRate := consts.SampleRate44100
	dstRate := consts.SampleRate48000
	ratio := resample.Ratio{InputRate: srcRate, OutputRate: dstRate}

	// Generate 1 second of 440 Hz mono audio at 44100 Hz.
	input := generators.Sine(consts.FreqNoteA4, time.Second, srcRate)

	// Create a streaming converter.
	conv, err := resample.New(resample.SincMediumQuality, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer conv.Close()

	// Process in 1024-sample chunks.
	outBuf := make([]float64, int(1024*ratio.Float64())+256)
	var totalOut int

	err = mutations.ChunkFunc(input.Data, 1024, func(chunk []float64, last bool) error {
		d := &resample.Data{
			DataIn:     chunk,
			DataOut:    outBuf,
			EndOfInput: last,
			Ratio:      ratio,
		}
		if err := conv.Process(d); err != nil {
			return err
		}
		// In a real application, you would write outBuf[:d.OutputFramesGen]
		// to a file, network socket, or audio device here.
		totalOut += d.OutputFramesGen
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "process error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Input:  %d frames at %d Hz\n", len(input.Data), srcRate)
	fmt.Printf("Output: %d frames at %d Hz\n", totalOut, dstRate)
	fmt.Printf("Expected: ~%d frames\n", int(float64(len(input.Data))*ratio.Float64()))
}
