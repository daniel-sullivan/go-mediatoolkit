//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// randomMatrixData fills a rows*cols int16 slice with deterministic
// pseudo-random values in the int16 range. Using a fixed seed keeps
// trial counts reproducible between runs.
func randomMatrixData(r *rand.Rand, rows, cols int) []int16 {
	n := rows * cols
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(r.Int31n(65536) - 32768)
	}
	return out
}

// realisticPCMFloat32 returns frame_size*input_rows samples in [-1, 1].
// This is the valid input domain for RES2INT16/FLOAT2INT16; synthetic
// over-range values would exercise C UB we avoid under the project's
// fuzz discipline.
func realisticPCMFloat32(r *rand.Rand, n int) []float32 {
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = r.Float32()*2.0 - 1.0
	}
	return out
}

// realisticPCMInt16 returns n int16 samples across the full int16
// range. The `_in_short` path converts int16 samples, so the whole
// range is the valid domain.
func realisticPCMInt16(r *rand.Rand, n int) []int16 {
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(r.Int31n(65536) - 32768)
	}
	return out
}

func TestParity_MappingMatrix_Init(t *testing.T) {
	seeds := []int64{1, 42, 99, 2024}
	combos := []struct{ rows, cols, gain int }{
		{1, 1, 0},
		{2, 2, 0},
		{6, 6, 256},
		{11, 11, 3050},
		{18, 18, -512},
		{27, 27, 0},
		{38, 38, 8192},
		{4, 8, 0},
		{8, 4, -1000},
		{3, 5, 500},
	}
	trials := 0
	for _, seed := range seeds {
		r := rand.New(rand.NewSource(seed))
		for _, c := range combos {
			data := randomMatrixData(r, c.rows, c.cols)
			// get_size parity.
			wantSize := cMappingMatrixGetSize(c.rows, c.cols)
			gotSize := nativeopus.ExportTestMappingMatrixGetSize(c.rows, c.cols)
			if wantSize != gotSize {
				t.Errorf("get_size(%d,%d): want %d got %d", c.rows, c.cols, wantSize, gotSize)
			}
			// init parity: compare the cell payload after init.
			cBuf, _ := cMappingMatrixInit(c.rows, c.cols, c.gain, data)
			cRows, cCols, cGain, cCells := cMappingMatrixExtract(cBuf)
			g := nativeopus.ExportTestMappingMatrixInit(c.rows, c.cols, c.gain, data)
			if cRows != g.Rows || cCols != g.Cols || cGain != g.Gain {
				t.Errorf("init meta mismatch (r=%d c=%d gain=%d): C=(%d,%d,%d) Go=(%d,%d,%d)",
					c.rows, c.cols, c.gain, cRows, cCols, cGain, g.Rows, g.Cols, g.Gain)
			}
			if len(cCells) != len(g.Data) {
				t.Fatalf("cell length mismatch (r=%d c=%d): C=%d Go=%d", c.rows, c.cols, len(cCells), len(g.Data))
			}
			for i := range cCells {
				if cCells[i] != g.Data[i] {
					t.Errorf("cell[%d] mismatch (r=%d c=%d seed=%d): C=%d Go=%d", i, c.rows, c.cols, seed, cCells[i], g.Data[i])
					break
				}
			}
			trials++
		}
	}
	t.Logf("TestParity_MappingMatrix_Init: %d trials", trials)
}

// layout describes one multistream channel layout used by the
// multiply-path tests. Layouts mirror the shapes produced by
// opus_multistream_encoder_create for 1/2/5.1 channel configs, scaled
// into the matrix dimensions (rows=streams, cols=channels).
type layout struct {
	name     string
	channels int // input channel count (cols on the _in_ paths)
	streams  int // output stream count (rows on the _in_ paths)
}

var multiplyLayouts = []layout{
	{"mono", 1, 1},
	{"stereo", 2, 2},
	{"5.1", 6, 6},
	{"wide", 4, 8},
	{"tall", 8, 4},
}

func TestParity_MappingMatrix_MultiplyChannelInFloat(t *testing.T) {
	const frameSize = 120
	seeds := []int64{1, 7, 128, 2026}
	trials := 0
	for _, seed := range seeds {
		r := rand.New(rand.NewSource(seed))
		for _, L := range multiplyLayouts {
			rows := L.streams
			cols := L.channels
			data := randomMatrixData(r, rows, cols)
			input := realisticPCMFloat32(r, cols*frameSize)
			// Test every output row to cover the whole matrix.
			for out_row := 0; out_row < rows; out_row++ {
				outC := make([]float32, rows*frameSize)
				outG := make([]float32, rows*frameSize)
				// Pre-seed with a non-zero pattern to detect spurious
				// writes to other rows.
				for i := range outC {
					v := r.Float32()*0.5 - 0.25
					outC[i] = v
					outG[i] = v
				}
				cMappingMatrixMultiplyChannelInFloat(rows, cols, data, input, cols, outC, out_row, rows, frameSize)
				nativeopus.ExportTestMappingMatrixMultiplyChannelInFloat(rows, cols, data, input, cols, outG, out_row, rows, frameSize)
				for i := range outC {
					if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
						t.Errorf("%s seed=%d out_row=%d i=%d: C=%g(0x%08x) Go=%g(0x%08x)",
							L.name, seed, out_row, i,
							outC[i], math.Float32bits(outC[i]),
							outG[i], math.Float32bits(outG[i]))
						t.FailNow()
					}
				}
				trials++
			}
		}
	}
	t.Logf("TestParity_MappingMatrix_MultiplyChannelInFloat: %d trials", trials)
}

func TestParity_MappingMatrix_MultiplyChannelOutFloat(t *testing.T) {
	const frameSize = 120
	seeds := []int64{2, 13, 777, 2027}
	trials := 0
	for _, seed := range seeds {
		r := rand.New(rand.NewSource(seed))
		for _, L := range multiplyLayouts {
			rows := L.channels
			cols := L.streams
			data := randomMatrixData(r, rows, cols)
			// Input is one opus_res stream laid out [input_rows * frame_size].
			input_rows := cols
			input := realisticPCMFloat32(r, input_rows*frameSize)
			for in_row := 0; in_row < cols; in_row++ {
				outC := realisticPCMFloat32(r, rows*frameSize)
				outG := make([]float32, len(outC))
				copy(outG, outC)
				cMappingMatrixMultiplyChannelOutFloat(rows, cols, data, input, in_row, input_rows, outC, rows, frameSize)
				nativeopus.ExportTestMappingMatrixMultiplyChannelOutFloat(rows, cols, data, input, in_row, input_rows, outG, rows, frameSize)
				for i := range outC {
					if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
						t.Errorf("%s seed=%d in_row=%d i=%d: C=%g(0x%08x) Go=%g(0x%08x)",
							L.name, seed, in_row, i,
							outC[i], math.Float32bits(outC[i]),
							outG[i], math.Float32bits(outG[i]))
						t.FailNow()
					}
				}
				trials++
			}
		}
	}
	t.Logf("TestParity_MappingMatrix_MultiplyChannelOutFloat: %d trials", trials)
}

func TestParity_MappingMatrix_MultiplyChannelInInt16(t *testing.T) {
	const frameSize = 120
	seeds := []int64{3, 21, 333, 2028}
	trials := 0
	for _, seed := range seeds {
		r := rand.New(rand.NewSource(seed))
		for _, L := range multiplyLayouts {
			rows := L.streams
			cols := L.channels
			data := randomMatrixData(r, rows, cols)
			input := realisticPCMInt16(r, cols*frameSize)
			for out_row := 0; out_row < rows; out_row++ {
				outC := make([]float32, rows*frameSize)
				outG := make([]float32, rows*frameSize)
				for i := range outC {
					v := r.Float32()*0.5 - 0.25
					outC[i] = v
					outG[i] = v
				}
				cMappingMatrixMultiplyChannelInShort(rows, cols, data, input, cols, outC, out_row, rows, frameSize)
				nativeopus.ExportTestMappingMatrixMultiplyChannelInShort(rows, cols, data, input, cols, outG, out_row, rows, frameSize)
				for i := range outC {
					if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
						t.Errorf("%s seed=%d out_row=%d i=%d: C=%g(0x%08x) Go=%g(0x%08x)",
							L.name, seed, out_row, i,
							outC[i], math.Float32bits(outC[i]),
							outG[i], math.Float32bits(outG[i]))
						t.FailNow()
					}
				}
				trials++
			}
		}
	}
	t.Logf("TestParity_MappingMatrix_MultiplyChannelInInt16: %d trials", trials)
}

func TestParity_MappingMatrix_MultiplyChannelOutInt16(t *testing.T) {
	const frameSize = 120
	seeds := []int64{4, 55, 919, 2029}
	trials := 0
	for _, seed := range seeds {
		r := rand.New(rand.NewSource(seed))
		for _, L := range multiplyLayouts {
			rows := L.channels
			cols := L.streams
			data := randomMatrixData(r, rows, cols)
			input_rows := cols
			input := realisticPCMFloat32(r, input_rows*frameSize)
			for in_row := 0; in_row < cols; in_row++ {
				outC := realisticPCMInt16(r, rows*frameSize)
				outG := make([]int16, len(outC))
				copy(outG, outC)
				cMappingMatrixMultiplyChannelOutShort(rows, cols, data, input, in_row, input_rows, outC, rows, frameSize)
				nativeopus.ExportTestMappingMatrixMultiplyChannelOutShort(rows, cols, data, input, in_row, input_rows, outG, rows, frameSize)
				for i := range outC {
					if outC[i] != outG[i] {
						t.Errorf("%s seed=%d in_row=%d i=%d: C=%d Go=%d",
							L.name, seed, in_row, i, outC[i], outG[i])
						t.FailNow()
					}
				}
				trials++
			}
		}
	}
	t.Logf("TestParity_MappingMatrix_MultiplyChannelOutInt16: %d trials", trials)
}

// Combined dense matmul test: exercise all four paths at realistic
// layouts + PCM shapes, confirming 0 ULP / 0 bit diff.
func TestParity_MappingMatrix_Multiply(t *testing.T) {
	// Convenience top-level wrapper to keep the original test-entry
	// name from the task brief. It simply runs the four per-shape
	// tests via t.Run so they surface as a single suite.
	t.Run("in_float", TestParity_MappingMatrix_MultiplyChannelInFloat)
	t.Run("out_float", TestParity_MappingMatrix_MultiplyChannelOutFloat)
	t.Run("in_int16", TestParity_MappingMatrix_MultiplyChannelInInt16)
	t.Run("out_int16", TestParity_MappingMatrix_MultiplyChannelOutInt16)
}

func TestParity_OpusMultistream_LayoutHelpers(t *testing.T) {
	// 5.1 encoder layout with 4 streams (2 coupled, 2 uncoupled) is
	// the standard shape produced by opus_multistream_surround_encoder.
	layouts := []cChannelLayout{
		{NbChannels: 1, NbStreams: 1, NbCoupledStreams: 0, Mapping: mapping([]byte{0})},
		{NbChannels: 2, NbStreams: 1, NbCoupledStreams: 1, Mapping: mapping([]byte{0, 1})},
		{NbChannels: 6, NbStreams: 4, NbCoupledStreams: 2, Mapping: mapping([]byte{0, 4, 1, 2, 3, 5})},
		{NbChannels: 8, NbStreams: 5, NbCoupledStreams: 3, Mapping: mapping([]byte{0, 6, 1, 2, 3, 4, 5, 7})},
		// layout with 255 sentinel markers (unused channels).
		{NbChannels: 4, NbStreams: 2, NbCoupledStreams: 1, Mapping: mapping([]byte{0, 1, 255, 2})},
		// Invalid layout: mapping index >= nb_streams+nb_coupled.
		{NbChannels: 2, NbStreams: 1, NbCoupledStreams: 0, Mapping: mapping([]byte{0, 7})},
	}
	trials := 0
	for i, l := range layouts {
		// convert to Export type
		el := nativeopus.ExportChannelLayout{
			NbChannels:       l.NbChannels,
			NbStreams:        l.NbStreams,
			NbCoupledStreams: l.NbCoupledStreams,
			Mapping:          l.Mapping,
		}
		if cv, gv := cValidateLayout(l), nativeopus.ExportTestValidateLayout(el); cv != gv {
			t.Errorf("layout[%d] validate_layout: C=%d Go=%d", i, cv, gv)
		}
		trials++
		// Sweep stream_id range [0..10] and prev [-1..nb_channels].
		for sid := 0; sid < 8; sid++ {
			for prev := -1; prev <= l.NbChannels; prev++ {
				if cv, gv := cGetLeftChannel(l, sid, prev), nativeopus.ExportTestGetLeftChannel(el, sid, prev); cv != gv {
					t.Errorf("layout[%d] get_left(sid=%d prev=%d): C=%d Go=%d", i, sid, prev, cv, gv)
				}
				if cv, gv := cGetRightChannel(l, sid, prev), nativeopus.ExportTestGetRightChannel(el, sid, prev); cv != gv {
					t.Errorf("layout[%d] get_right(sid=%d prev=%d): C=%d Go=%d", i, sid, prev, cv, gv)
				}
				if cv, gv := cGetMonoChannel(l, sid, prev), nativeopus.ExportTestGetMonoChannel(el, sid, prev); cv != gv {
					t.Errorf("layout[%d] get_mono(sid=%d prev=%d): C=%d Go=%d", i, sid, prev, cv, gv)
				}
				trials += 3
			}
		}
	}
	t.Logf("TestParity_OpusMultistream_LayoutHelpers: %d trials", trials)
}

// mapping copies the head of the provided byte slice into a [256]byte
// buffer, zero-filling the rest.
func mapping(head []byte) [256]byte {
	var out [256]byte
	copy(out[:], head)
	return out
}
