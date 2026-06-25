// Example: variable ratio conversion.
//
// Demonstrates changing the conversion ratio between Process() calls,
// useful for pitch bending, doppler effects, or adaptive rate matching.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

func main() {
	srcRate := consts.SampleRate44100

	// Generate a 1-second 440 Hz tone at 44100 Hz.
	input := generators.Sine(consts.FreqNoteA4, time.Second, srcRate)

	conv, err := resample.New(resample.SincFastest, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer conv.Close()

	chunkSize := 4410 // 100ms chunks
	outBuf := make([]float64, chunkSize*4)

	// Sweep the ratio from 1.0 (44100 Hz) up to 2.0 (88200 Hz) over 10 chunks.
	var totalOut int
	chunks := len(input.Data) / chunkSize

	for i := 0; i < chunks; i++ {
		offset := i * chunkSize
		endOfInput := i == chunks-1

		// Linear sweep: ratio goes from 1.0 to 2.0.
		ratioF := 1.0 + float64(i)/float64(chunks-1)
		ratio := resample.Ratio{InputRate: 1000, OutputRate: int(ratioF * 1000)}

		d := &resample.Data{
			DataIn:     input.Data[offset : offset+chunkSize],
			DataOut:    outBuf,
			EndOfInput: endOfInput,
			Ratio:      ratio,
		}

		if err := conv.Process(d); err != nil {
			fmt.Fprintf(os.Stderr, "process error at chunk %d: %v\n", i, err)
			os.Exit(1)
		}

		totalOut += d.OutputFramesGen
		fmt.Printf("Chunk %2d: ratio=%.2f  in=%d  out=%d\n",
			i, ratioF, d.InputFramesUsed, d.OutputFramesGen)
	}

	fmt.Printf("\nTotal output: %d frames\n", totalOut)
}
