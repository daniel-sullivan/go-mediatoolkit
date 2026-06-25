// Example: one-shot sample rate conversion using Simple().
//
// Converts a 440 Hz sine wave from 44100 Hz to 48000 Hz.
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
	dstRate := consts.SampleRate48000

	// Generate a 10ms 440 Hz sine wave at 44100 Hz.
	input := generators.Sine(consts.FreqNoteA4, 10*time.Millisecond, srcRate)

	// Convert to 48000 Hz in one call.
	ratio := resample.Ratio{InputRate: srcRate, OutputRate: dstRate}
	output, err := resample.Simple(input.Data, resample.SincFastest, 1, ratio)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Input:  %d samples at %d Hz\n", len(input.Data), srcRate)
	fmt.Printf("Output: %d samples at %d Hz\n", len(output), dstRate)
	fmt.Printf("Ratio:  %.6f\n", ratio.Float64())
}
