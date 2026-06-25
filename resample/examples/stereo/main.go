// Example: multi-channel (stereo) sample rate conversion.
//
// Demonstrates interleaved stereo processing with different signals
// on each channel.
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
	srcRate := consts.SampleRate48000
	dstRate := consts.SampleRate22050
	ratio := resample.Ratio{InputRate: srcRate, OutputRate: dstRate}
	duration := 100 * time.Millisecond

	// Generate separate mono signals for each channel.
	left := generators.Sine(consts.FreqNoteA4, duration, srcRate)
	right := generators.Sine(consts.FreqNoteA5, duration, srcRate)

	// Interleave into stereo: [L0, R0, L1, R1, ...]
	input := mutations.Interleave([][]float64{left.Data, right.Data})
	frames := len(left.Data)

	// Use best quality sinc for the downsampling.
	output, err := resample.Simple(input, resample.SincBestQuality, 2, ratio)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	outFrames := len(output) / 2
	fmt.Printf("Downsampled stereo: %d frames at %d Hz -> %d frames at %d Hz\n",
		frames, srcRate, outFrames, dstRate)

	// Print first few stereo samples to show channel separation is preserved.
	fmt.Println("\nFirst 5 output frames (L, R):")
	for i := 0; i < 5 && i < outFrames; i++ {
		fmt.Printf("  [%d] L=%+.4f  R=%+.4f\n", i, output[i*2], output[i*2+1])
	}
}
