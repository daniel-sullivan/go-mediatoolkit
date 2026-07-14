package rnnoise

import (
	_ "embed"
	"encoding/binary"
	"math"
	"sync"
)

// rnnoise_weights.bin is the float-path weight blob extracted from the
// vendored rnnoise_data.c by denoise/internal/rnnoise/weightgen (run via
// `mise run //libraries/rnnoise:weights`). It is the concatenation, in
// weightOrder, of the raw little-endian float32 / int32 arrays the
// network needs; the int8 quantised arrays are the SIMD-only path and are
// not included. See LICENSING.md and librnnoise/VERSION for provenance.
//
//go:embed rnnoise_weights.bin
var weightsBlob []byte

// linearLayer mirrors the float-path fields of nnet.h LinearLayer.
// weightsIdx != nil selects the sparse (sparse_sgemv8x4) path; diag != nil
// adds the GRU recurrent diagonal term.
type linearLayer struct {
	bias         []float32
	floatWeights []float32
	weightsIdx   []int32
	diag         []float32
	nbInputs     int
	nbOutputs    int
}

// model mirrors rnnoise_data.h RNNoise: the ten linear layers of the
// denoiser network.
type model struct {
	conv1, conv2         linearLayer
	gru1Input, gru1Recur linearLayer
	gru2Input, gru2Recur linearLayer
	gru3Input, gru3Recur linearLayer
	denseOut, vadDense   linearLayer
}

var (
	modelOnce   sync.Once
	loadedModel *model
)

// theModel returns the shared, read-only network weights, parsed once
// from the embedded blob.
func theModel() *model {
	modelOnce.Do(func() { loadedModel = parseModel(weightsBlob) })
	return loadedModel
}

func parseModel(blob []byte) *model {
	off := 0
	nextF := func(count int) []float32 {
		f := make([]float32, count)
		for i := 0; i < count; i++ {
			f[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[off:]))
			off += 4
		}
		return f
	}
	nextI := func(count int) []int32 {
		v := make([]int32, count)
		for i := 0; i < count; i++ {
			v[i] = int32(binary.LittleEndian.Uint32(blob[off:]))
			off += 4
		}
		return v
	}

	m := new(model)
	// Order must match weightgen/main.go's `order`.
	m.conv1 = linearLayer{floatWeights: nextF(24960), bias: nextF(128), nbInputs: 195, nbOutputs: 128}
	m.conv2 = linearLayer{floatWeights: nextF(147456), bias: nextF(384), nbInputs: 384, nbOutputs: 384}

	gruInput := func() linearLayer {
		w := nextF(147456)
		idx := nextI(4752)
		b := nextF(1152)
		return linearLayer{floatWeights: w, weightsIdx: idx, bias: b, nbInputs: 384, nbOutputs: 1152}
	}
	gruRecur := func() linearLayer {
		w := nextF(147456)
		idx := nextI(4752)
		d := nextF(1152)
		b := nextF(1152)
		return linearLayer{floatWeights: w, weightsIdx: idx, diag: d, bias: b, nbInputs: 384, nbOutputs: 1152}
	}
	m.gru1Input = gruInput()
	m.gru1Recur = gruRecur()
	m.gru2Input = gruInput()
	m.gru2Recur = gruRecur()
	m.gru3Input = gruInput()
	m.gru3Recur = gruRecur()

	m.denseOut = linearLayer{floatWeights: nextF(12288), bias: nextF(32), nbInputs: 384, nbOutputs: 32}
	m.vadDense = linearLayer{floatWeights: nextF(384), bias: nextF(1), nbInputs: 384, nbOutputs: 1}
	return m
}
