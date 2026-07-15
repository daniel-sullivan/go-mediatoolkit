//go:build cgo

// Package gtcrn_ort is the opt-in onnxruntime parity slice for the
// pure-Go GTCRN port (denoise/internal/gtcrn): it runs the vendored
// gtcrn.onnx streaming graph through onnxruntime and asserts the Go
// port matches per frame within tolerance, carrying the three recurrent
// caches exactly as the model does. It follows the Silero precedent
// (vad/internal/parity_tests/silero_ort): the "vendored reference" is a
// model file executed by an external runtime, so it cannot run in
// default CI and is gated on the ONNXRUNTIME_SHARED_LIB environment
// variable (t.Skip otherwise). Default CI gets the same numbers through
// the committed golden JSON (generated here by gen, checked by the
// default-CI golden test) and kept honest by gen -verify.
//
// # Tolerance: mixed criterion max(|Δ| ≤ 1e-4, rel ≤ 1e-3)
//
// Both sides accumulate fp32 in different orders (onnxruntime's MLAS
// kernels vectorise and use FMA; the Go port uses fixed sequential
// fp32), so bit-equality against ORT is unachievable without
// re-implementing MLAS's exact blocking. The bound is justified in
// VERSION and pinned to onnxruntime 1.27.0 via
// github.com/yalue/onnxruntime_go v1.31.0, driven single-threaded with
// graph optimizations disabled (ORT_DISABLE_ALL) so the oracle is as
// deterministic as the runtime allows.
package gtcrn_ort

import (
	"errors"
	"fmt"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// SharedLibEnv names the environment variable that must point at the
// onnxruntime shared library for the oracle to run. Unset → the parity
// tests skip and the generator refuses to run.
const SharedLibEnv = "ONNXRUNTIME_SHARED_LIB"

// ModelSHA256 is the pinned digest of the vendored oracle model —
// gtcrn.onnx at upstream commit 3862c448 (see
// denoise/internal/gtcrn/VERSION).
const ModelSHA256 = "f648b02f2d7ff96ebcb0eec2219688a08ed12fe7e3d50f248605a90eba8cad17"

// Model geometry (VERSION): one STFT frame in, three caches carried.
const (
	Bins       = 257 // onesided spectrum bins
	frameLen   = Bins * 2
	convCacheN = 2 * 1 * 16 * 16 * 33
	traCacheN  = 2 * 3 * 1 * 1 * 16
	interN     = 2 * 1 * 33 * 16
)

var (
	envOnce sync.Once
	envErr  error
)

// ErrNoSharedLib signals that SharedLibEnv is unset.
var ErrNoSharedLib = errors.New("gtcrn_ort: ONNXRUNTIME_SHARED_LIB not set")

// InitEnvironment initialises the onnxruntime environment from
// SharedLibEnv, once per process.
func InitEnvironment() error {
	envOnce.Do(func() {
		lib := os.Getenv(SharedLibEnv)
		if lib == "" {
			envErr = fmt.Errorf("%w: set %s to the onnxruntime shared library path", ErrNoSharedLib, SharedLibEnv)
			return
		}
		ort.SetSharedLibraryPath(lib)
		if err := ort.InitializeEnvironment(); err != nil {
			envErr = fmt.Errorf("gtcrn_ort: initialising onnxruntime from %s=%s: %w", SharedLibEnv, lib, err)
		}
	})
	return envErr
}

// Oracle drives the vendored gtcrn.onnx exactly as the streaming
// pipeline does: one mix frame [1,257,1,2] in, enh frame out, three
// recurrent caches carried between Run calls (zero at start / Reset).
type Oracle struct {
	session    *ort.AdvancedSession
	opts       *ort.SessionOptions
	mix        *ort.Tensor[float32]
	convCache  *ort.Tensor[float32]
	traCache   *ort.Tensor[float32]
	interCache *ort.Tensor[float32]
	enh        *ort.Tensor[float32]
	convOut    *ort.Tensor[float32]
	traOut     *ort.Tensor[float32]
	interOut   *ort.Tensor[float32]
}

// NewOracle opens the model at modelPath with a hardened, single-thread,
// optimization-disabled session. InitEnvironment must have succeeded.
func NewOracle(modelPath string) (*Oracle, error) {
	o := new(Oracle)
	var err error
	o.opts, err = ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	if err := o.opts.SetIntraOpNumThreads(1); err != nil {
		o.Destroy()
		return nil, err
	}
	if err := o.opts.SetInterOpNumThreads(1); err != nil {
		o.Destroy()
		return nil, err
	}
	if err := o.opts.SetGraphOptimizationLevel(ort.GraphOptimizationLevelDisableAll); err != nil {
		o.Destroy()
		return nil, err
	}

	mk := func(dst **ort.Tensor[float32], shape ...int64) bool {
		t, e := ort.NewEmptyTensor[float32](ort.NewShape(shape...))
		if e != nil {
			err = e
			return false
		}
		*dst = t
		return true
	}
	ok := mk(&o.mix, 1, 257, 1, 2) &&
		mk(&o.convCache, 2, 1, 16, 16, 33) &&
		mk(&o.traCache, 2, 3, 1, 1, 16) &&
		mk(&o.interCache, 2, 1, 33, 16) &&
		mk(&o.enh, 1, 257, 1, 2) &&
		mk(&o.convOut, 2, 1, 16, 16, 33) &&
		mk(&o.traOut, 2, 3, 1, 1, 16) &&
		mk(&o.interOut, 2, 1, 33, 16)
	if !ok {
		o.Destroy()
		return nil, err
	}

	o.session, err = ort.NewAdvancedSession(modelPath,
		[]string{"mix", "conv_cache", "tra_cache", "inter_cache"},
		[]string{"enh", "conv_cache_out", "tra_cache_out", "inter_cache_out"},
		[]ort.Value{o.mix, o.convCache, o.traCache, o.interCache},
		[]ort.Value{o.enh, o.convOut, o.traOut, o.interOut},
		o.opts)
	if err != nil {
		o.Destroy()
		return nil, err
	}
	return o, nil
}

// Reset zeros the carried caches.
func (o *Oracle) Reset() {
	clear(o.convCache.GetData())
	clear(o.traCache.GetData())
	clear(o.interCache.GetData())
}

// RunFrame enhances one spectrum frame (length 514, interleaved re,im
// per bin: [re0,im0,re1,im1,…]) and advances the caches. The returned
// slice aliases the oracle's output tensor — copy it if retained.
func (o *Oracle) RunFrame(mix []float32) ([]float32, error) {
	if len(mix) != frameLen {
		return nil, fmt.Errorf("gtcrn_ort: frame length %d, want %d", len(mix), frameLen)
	}
	copy(o.mix.GetData(), mix)
	if err := o.session.Run(); err != nil {
		return nil, err
	}
	copy(o.convCache.GetData(), o.convOut.GetData())
	copy(o.traCache.GetData(), o.traOut.GetData())
	copy(o.interCache.GetData(), o.interOut.GetData())
	return o.enh.GetData(), nil
}

// Caches returns copies of the three carried caches, for checkpoint
// parity against the Go port.
func (o *Oracle) Caches() (conv, tra, inter []float32) {
	return append([]float32(nil), o.convCache.GetData()...),
		append([]float32(nil), o.traCache.GetData()...),
		append([]float32(nil), o.interCache.GetData()...)
}

// Destroy releases the session, options, and tensors.
func (o *Oracle) Destroy() {
	if o.session != nil {
		_ = o.session.Destroy()
	}
	for _, t := range []interface{ Destroy() error }{
		o.mix, o.convCache, o.traCache, o.interCache,
		o.enh, o.convOut, o.traOut, o.interOut,
	} {
		if t != nil {
			_ = t.Destroy()
		}
	}
	if o.opts != nil {
		_ = o.opts.Destroy()
	}
}
