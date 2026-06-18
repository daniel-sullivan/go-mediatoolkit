//go:build !debug_celt_trace

package nativeopus

// Stub surface for the CELT mid-frame tracing API. See
// export_for_testing_celt_trace.go (built with the debug_celt_trace
// tag) for the real implementation.

// CELTTracer is an opaque type; the stub version does nothing.
type CELTTracer struct{}

// NewCELTTracer is a no-op under the stub build. Returns a valid
// pointer whose methods are all no-ops.
func NewCELTTracer(dir string, frame int) (*CELTTracer, error) {
	_ = dir
	_ = frame
	return &CELTTracer{}, nil
}

// Close is a no-op in the stub build.
func (t *CELTTracer) Close() error { return nil }

// EmitArray / EmitInts are no-ops.
func (t *CELTTracer) EmitArray(point, name string, xs []float32) {}
func (t *CELTTracer) EmitInts(point, name string, xs []int32)    {}

// SetCELTTracer is a no-op in the stub build.
func SetCELTTracer(t *CELTTracer) {}

// ExportCELTTraceEnabled reports false in the stub build.
func ExportCELTTraceEnabled() bool { return false }

// HookCELTAfter / HookCELTAfterInts are no-ops in the stub build. The
// production celt_encoder.go does not call these unconditionally; when
// we wire them in for the tracing build, they will be guarded by
// `if ExportCELTTraceEnabled() { ... }` so the non-trace build keeps
// emitting identical bit-exact output.
func HookCELTAfter(point string, name string, xs []float32)   {}
func HookCELTAfterInts(point string, name string, xs []int32) {}
