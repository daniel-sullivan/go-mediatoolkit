package gtcrn

// SampleRate is the only rate the GTCRN graph runs at.
const SampleRate = 16000

// Feature geometry (VERSION).
const (
	nfreqs  = 257              // onesided STFT bins
	erbLow  = 65               // ERB low bins kept verbatim
	erbBand = 64               // ERB bands
	erbF    = erbLow + erbBand // 129 = feature freq width after ERB merge
	encC    = 16               // encoder/decoder channel width
	dpF     = 33               // freq width entering the DPGRNN stack
)

// Cache tensor element counts (VERSION): the three recurrent caches
// carried frame to frame, zero at stream start. Stored in the exact ONNX
// tensor layout so cache-checkpoint parity is a direct comparison.
//
//	convCache  [2,1,16,16,33]  [enc|dec][C][Tc=16][F=33]
//	traCache   [2,3,1,1,16]    [enc|dec][block 0..2][hidden 16]
//	interCache [2,1,33,16]     [dpgrnn 0..1][BF=33][hidden 16]
const (
	convCacheN = 2 * encC * 16 * dpF
	traCacheN  = 2 * 3 * encC
	interN     = 2 * dpF * encC
)

// Model is one streaming instance of the GTCRN graph: the shared
// read-only weights plus this stream's three recurrent caches. Each
// Model serves one stream; not safe for concurrent use.
type Model struct {
	w *weights

	convCache  [convCacheN]float32
	traCache   [traCacheN]float32
	interCache [interN]float32
}

// NewModel builds a Model over the embedded weights (parsed and shared
// once). It fails only if the embedded payload is corrupt or incomplete.
func NewModel() (*Model, error) {
	t, err := loadTensors()
	if err != nil {
		return nil, err
	}
	w, err := buildWeights(t)
	if err != nil {
		return nil, err
	}
	return &Model{w: w}, nil
}

// Reset clears the carried caches back to the just-constructed zeros.
func (m *Model) Reset() {
	clear(m.convCache[:])
	clear(m.traCache[:])
	clear(m.interCache[:])
}

// convCacheSlice extracts a GTConvBlock's temporal cache (half 0=encoder,
// 1=decoder; dilation dil) from the ONNX-layout store into a c-major
// [C*Tc*F] buffer for the depth-conv kernels.
func (m *Model) convCacheSlice(half, dil int) []float32 {
	start, length := convCacheOffset(dil)
	buf := make([]float32, encC*length*dpF)
	for c := 0; c < encC; c++ {
		base := (half*encC + c) * 16 * dpF
		for t := 0; t < length; t++ {
			copy(buf[(c*length+t)*dpF:(c*length+t+1)*dpF], m.convCache[base+(start+t)*dpF:base+(start+t+1)*dpF])
		}
	}
	return buf
}

// putConvCache writes an updated [C*Tc*F] cache back into the ONNX store.
func (m *Model) putConvCache(half, dil int, buf []float32) {
	start, length := convCacheOffset(dil)
	for c := 0; c < encC; c++ {
		base := (half*encC + c) * 16 * dpF
		for t := 0; t < length; t++ {
			copy(m.convCache[base+(start+t)*dpF:base+(start+t+1)*dpF], buf[(c*length+t)*dpF:(c*length+t+1)*dpF])
		}
	}
}

// traCacheSlice returns the TRA GRU hidden state (16) for a block by
// half and position index (0..2 in block order).
func (m *Model) traCacheSlice(half, pos int) []float32 {
	i := (half*3 + pos) * encC
	return m.traCache[i : i+encC]
}

// interCacheSlice returns dpgrnn i's inter-RNN hidden store ([33*16]).
func (m *Model) interCacheSlice(i int) []float32 {
	return m.interCache[i*dpF*encC : (i+1)*dpF*encC]
}

// Caches returns copies of the three carried caches in the ONNX tensor
// layout (conv_cache [2,1,16,16,33], tra_cache [2,3,1,1,16], inter_cache
// [2,1,33,16] flattened) — for cache-checkpoint parity against the oracle.
func (m *Model) Caches() (conv, tra, inter []float32) {
	return append([]float32(nil), m.convCache[:]...),
		append([]float32(nil), m.traCache[:]...),
		append([]float32(nil), m.interCache[:]...)
}
