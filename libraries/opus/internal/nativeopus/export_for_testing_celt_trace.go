//go:build debug_celt_trace

package nativeopus

// export_for_testing_celt_trace.go — optional CELT mid-frame tracing
// hooks. Enabled with `-tags debug_celt_trace`; otherwise the no-op
// stubs in export_for_testing_celt_trace_stub.go are used.
//
// The Go port does not yet wire these hook points into celt_encode_with_ec
// proper — doing so is a future follow-up that requires threading a
// per-call Tracer pointer through the inner call chain. This file
// declares the hook surface so the benchcmp harness can compile
// against a consistent signature now; the follow-up work is just the
// mechanical insertion of `if tracer != nil { tracer.Emit(...) }`
// calls at the named points inside celt_encoder.go.
//
// The named hook points mirror the task spec:
//   - after_prefilter
//   - after_transient_detection
//   - after_compute_band_energies
//   - after_normalise_bands
//   - after_tf_analysis
//   - after_alloc_trim_analysis
//   - after_compute_allocation
//   - after_quant_coarse_energy
//   - after_quant_all_bands
//   - after_quant_fine_energy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CELTTracer is a per-frame trace sink. One instance per call to
// celt_encode_with_ec.
type CELTTracer struct {
	mu     sync.Mutex
	frame  int
	dir    string
	writer *os.File
	enc    *json.Encoder
}

// NewCELTTracer opens a JSONL file at <dir>/celt_trace_frameN.jsonl.
func NewCELTTracer(dir string, frame int) (*CELTTracer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, fmt.Sprintf("celt_trace_frame%d.jsonl", frame))
	f, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	return &CELTTracer{dir: dir, frame: frame, writer: f, enc: json.NewEncoder(f)}, nil
}

// Close flushes and releases resources.
func (t *CELTTracer) Close() error {
	if t == nil || t.writer == nil {
		return nil
	}
	return t.writer.Close()
}

// EmitArray writes one (point, name, float32[]) record.
func (t *CELTTracer) EmitArray(point, name string, xs []float32) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.enc.Encode(map[string]any{
		"frame": t.frame,
		"point": point,
		"name":  name,
		"vals":  xs,
	})
}

// EmitInts writes one (point, name, int32[]) record.
func (t *CELTTracer) EmitInts(point, name string, xs []int32) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.enc.Encode(map[string]any{
		"frame": t.frame,
		"point": point,
		"name":  name,
		"vals":  xs,
	})
}

// activeTracer is the tracer installed for the next
// celt_encode_with_ec call. Set via SetCELTTracer; cleared after the
// call consumes it.
var activeTracer *CELTTracer

// SetCELTTracer installs a tracer for the next celt_encode_with_ec
// call. Call with nil to disable.
func SetCELTTracer(t *CELTTracer) { activeTracer = t }

// celtTraceEnabled reports whether tracing is compiled in. The
// production build (without -tags debug_celt_trace) uses the stub
// package and returns false.
func celtTraceEnabled() bool { return true }

// ExportCELTTraceEnabled is the public check for the benchcmp
// harness.
func ExportCELTTraceEnabled() bool { return celtTraceEnabled() }

// HookCELTAfterPrefilter / ... — named dispatchers. These are
// unconditionally called from celt_encoder.go's cardinal points when
// the debug_celt_trace tag is set. Implementations pull from
// activeTracer; if no tracer is installed, the call is a no-op.
func HookCELTAfter(point string, name string, xs []float32) {
	if activeTracer != nil {
		activeTracer.EmitArray(point, name, xs)
	}
}

// HookCELTAfterInts — int32 variant for tf_res / fine_quant / etc.
func HookCELTAfterInts(point string, name string, xs []int32) {
	if activeTracer != nil {
		activeTracer.EmitInts(point, name, xs)
	}
}
