package rnnoise

// Neural-network primitives, ported 1:1 from librnnoise/src/nnet.c,
// nnet_arch.h, and vec.h (GENERIC branch). Float-path only: the int8
// quantised path (cgemv/DOT_PROD/USE_SU_BIAS) is disabled in the oracle,
// so only sgemv / sparse_sgemv8x4 are ported. Every multiply-accumulate
// goes through the strict primitives to match -ffp-contract=off; the GRU
// gate order (z,r,h; h += recur*r before tanh; h = z*state + (1-z)*h) is
// preserved exactly, and the sparse 8x4 block loop is verbatim.

const (
	activationTanh    = 0
	activationSigmoid = 1
	activationLinear  = 2
)

// sparseSgemv8x4 is vec.h sparse_sgemv8x4: structured-sparse mat-vec with
// 8x4 blocks (SPARSE_BLOCK_SIZE 32). idx holds, per row-block of 8, a
// count followed by that many column positions.
func sparseSgemv8x4(out, w []float32, idx []int32, rows int, x []float32) {
	for i := 0; i < rows; i++ {
		out[i] = 0
	}
	wi := 0
	ii := 0
	for i := 0; i < rows; i += 8 {
		cols := int(idx[ii])
		ii++
		for j := 0; j < cols; j++ {
			pos := int(idx[ii])
			ii++
			xj0 := x[pos+0]
			xj1 := x[pos+1]
			xj2 := x[pos+2]
			xj3 := x[pos+3]
			for k := 0; k < 8; k++ {
				out[i+k] = mla(out[i+k], w[wi+k], xj0)
			}
			for k := 0; k < 8; k++ {
				out[i+k] = mla(out[i+k], w[wi+8+k], xj1)
			}
			for k := 0; k < 8; k++ {
				out[i+k] = mla(out[i+k], w[wi+16+k], xj2)
			}
			for k := 0; k < 8; k++ {
				out[i+k] = mla(out[i+k], w[wi+24+k], xj3)
			}
			wi += 32
		}
	}
}

// sgemv is vec.h sgemv: dense mat-vec, dispatching to the 16- or 8-row
// unrolled kernel or a scalar fallback exactly as the C does. weights are
// column-major with the given col_stride.
func sgemv(out, weights []float32, rows, cols, colStride int, x []float32) {
	switch {
	case rows&0xf == 0:
		for i := 0; i < rows; i++ {
			out[i] = 0
		}
		for i := 0; i < rows; i += 16 {
			for j := 0; j < cols; j++ {
				xj := x[j]
				base := j*colStride + i
				for k := 0; k < 16; k++ {
					out[i+k] = mla(out[i+k], weights[base+k], xj)
				}
			}
		}
	case rows&0x7 == 0:
		for i := 0; i < rows; i++ {
			out[i] = 0
		}
		for i := 0; i < rows; i += 8 {
			for j := 0; j < cols; j++ {
				xj := x[j]
				base := j*colStride + i
				for k := 0; k < 8; k++ {
					out[i+k] = mla(out[i+k], weights[base+k], xj)
				}
			}
		}
	default:
		for i := 0; i < rows; i++ {
			out[i] = 0
			for j := 0; j < cols; j++ {
				out[i] = mla(out[i], weights[j*colStride+i], x[j])
			}
		}
	}
}

// computeLinear is nnet_arch.h compute_linear_ (float path): mat-vec +
// bias + optional GRU diagonal.
func computeLinear(layer *linearLayer, out, in []float32) {
	m := layer.nbInputs
	n := layer.nbOutputs
	if layer.floatWeights != nil {
		if layer.weightsIdx != nil {
			sparseSgemv8x4(out, layer.floatWeights, layer.weightsIdx, n, in)
		} else {
			sgemv(out, layer.floatWeights, n, m, n, in)
		}
	} else {
		for i := 0; i < n; i++ {
			out[i] = 0
		}
	}
	if layer.bias != nil {
		for i := 0; i < n; i++ {
			out[i] = add32(out[i], layer.bias[i])
		}
	}
	if layer.diag != nil {
		// diag is only used for GRU recurrent weights (3*M == N).
		for i := 0; i < m; i++ {
			out[i] = mla(out[i], layer.diag[i], in[i])
			out[i+m] = mla(out[i+m], layer.diag[i+m], in[i])
			out[i+2*m] = mla(out[i+2*m], layer.diag[i+2*m], in[i])
		}
	}
}

// computeActivation is nnet_arch.h compute_activation_ for the tanh /
// sigmoid / linear cases the RNNoise net uses (SOFTMAX_HACK makes softmax
// dead code; swish/relu are unused).
func computeActivation(out, in []float32, n, activation int) {
	switch activation {
	case activationSigmoid:
		for i := 0; i < n; i++ {
			out[i] = sigmoidApprox(in[i])
		}
	case activationTanh:
		for i := 0; i < n; i++ {
			out[i] = tanhApprox(in[i])
		}
	default: // linear
		if &out[0] != &in[0] {
			copy(out[:n], in[:n])
		}
	}
}

// computeGenericDense is nnet.c compute_generic_dense.
func computeGenericDense(layer *linearLayer, out, in []float32, activation int) {
	computeLinear(layer, out, in)
	computeActivation(out, out, layer.nbOutputs, activation)
}

// computeGenericGru is nnet.c compute_generic_gru. state and in must not
// alias.
func computeGenericGru(inputW, recurW *linearLayer, state, in []float32) {
	n := recurW.nbInputs
	zrh := make([]float32, 3*n)
	recur := make([]float32, 3*n)
	computeLinear(inputW, zrh, in)
	computeLinear(recurW, recur, state)
	for i := 0; i < 2*n; i++ {
		zrh[i] = add32(zrh[i], recur[i])
	}
	computeActivation(zrh, zrh, 2*n, activationSigmoid) // z, r
	// h[i] += recur[2N+i]*r[i]
	for i := 0; i < n; i++ {
		zrh[2*n+i] = mla(zrh[2*n+i], recur[2*n+i], zrh[n+i])
	}
	computeActivation(zrh[2*n:], zrh[2*n:], n, activationTanh)
	// h[i] = z[i]*state[i] + (1-z[i])*h[i]
	for i := 0; i < n; i++ {
		zrh[2*n+i] = muladd(zrh[i], state[i], sub32(1, zrh[i]), zrh[2*n+i])
	}
	for i := 0; i < n; i++ {
		state[i] = zrh[2*n+i]
	}
}

// computeGenericConv1d is nnet.c compute_generic_conv1d: prepend the
// retained input memory, run the linear layer, activate, and update mem.
func computeGenericConv1d(layer *linearLayer, out, mem, input []float32, inputSize, activation int) {
	nbIn := layer.nbInputs
	tmp := make([]float32, nbIn)
	if nbIn != inputSize {
		copy(tmp[:nbIn-inputSize], mem[:nbIn-inputSize])
	}
	copy(tmp[nbIn-inputSize:], input[:inputSize])
	computeLinear(layer, out, tmp)
	computeActivation(out, out, layer.nbOutputs, activation)
	if nbIn != inputSize {
		copy(mem[:nbIn-inputSize], tmp[inputSize:inputSize+(nbIn-inputSize)])
	}
}
