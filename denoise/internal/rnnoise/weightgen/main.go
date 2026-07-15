//go:build ignore

// Command weightgen extracts the RNNoise float-path weights from the
// vendored rnnoise_data.c (via the rnnoise_arrays WeightArray table) and
// writes them, concatenated in the fixed order below, as a little-endian
// binary blob that denoise/internal/rnnoise embeds with go:embed.
//
// Run from the repo root (needs the vendored C + a C toolchain):
//
//	mise run //libraries/rnnoise:weights
//
// The order and element counts here MUST match weights.go's weightOrder.
package main

/*
#cgo CFLAGS: -I${SRCDIR}/../../parity_tests/rnncgo
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/src
#cgo CFLAGS: -I${SRCDIR}/../../../../libraries/rnnoise/librnnoise/include
#cgo CFLAGS: -DDISABLE_NEON -DRNNCGO_WITH_WEIGHTS
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-sign-compare
#cgo LDFLAGS: -lm

#include <string.h>
#include "librnnoise_units.h"

// rnndump_find returns the raw bytes (and byte length) of a named weight
// array from the vendored rnnoise_arrays table.
static const void *rnndump_find(const char *name, int *size_out) {
	for (int i = 0; rnnoise_arrays[i].name != NULL; i++) {
		if (strcmp(rnnoise_arrays[i].name, name) == 0) {
			*size_out = rnnoise_arrays[i].size;
			return rnnoise_arrays[i].data;
		}
	}
	*size_out = 0;
	return NULL;
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// order must exactly match weights.go weightOrder (name only).
var order = []string{
	"conv1_weights_float", "conv1_bias",
	"conv2_weights_float", "conv2_bias",
	"gru1_input_weights_float", "gru1_input_weights_idx", "gru1_input_bias",
	"gru1_recurrent_weights_float", "gru1_recurrent_weights_idx", "gru1_recurrent_weights_diag", "gru1_recurrent_bias",
	"gru2_input_weights_float", "gru2_input_weights_idx", "gru2_input_bias",
	"gru2_recurrent_weights_float", "gru2_recurrent_weights_idx", "gru2_recurrent_weights_diag", "gru2_recurrent_bias",
	"gru3_input_weights_float", "gru3_input_weights_idx", "gru3_input_bias",
	"gru3_recurrent_weights_float", "gru3_recurrent_weights_idx", "gru3_recurrent_weights_diag", "gru3_recurrent_bias",
	"dense_out_weights_float", "dense_out_bias",
	"vad_dense_weights_float", "vad_dense_bias",
}

func main() {
	out := "denoise/internal/rnnoise/rnnoise_weights.bin"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	var blob []byte
	for _, name := range order {
		cn := C.CString(name)
		var size C.int
		p := C.rnndump_find(cn, &size)
		C.free(unsafe.Pointer(cn))
		if p == nil || size == 0 {
			fmt.Fprintf(os.Stderr, "weight array %q not found\n", name)
			os.Exit(1)
		}
		blob = append(blob, C.GoBytes(p, size)...)
	}
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, %d arrays)\n", out, len(blob), len(order))
}
