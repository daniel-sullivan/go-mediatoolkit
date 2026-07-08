//go:build cgo

// Package silero_ort is the opt-in onnxruntime parity slice for the
// pure-Go Silero port (vad/internal/silero): it runs the vendored
// original silero_vad_16k_op15.onnx through onnxruntime and asserts
// the Go port matches per-window within tolerance, over the shared
// goldensignals set. It follows the repo's self-contained-slice
// pattern (loudness/internal/parity_tests), except its "vendored
// reference" is a model file executed by an external runtime rather
// than C sources compiled into the test binary — so unlike the fvad
// slices it cannot run in default CI, and is gated on the
// ONNXRUNTIME_SHARED_LIB environment variable (t.Skip otherwise).
// Default CI gets the same numbers through vad/testdata/
// silero_golden.json (generated here by gen/main.go, checked by
// vad/silero_golden_test.go, and kept honest by the silero-parity
// workflow's fresh-oracle verification, gen -verify).
//
// # Tolerance: per-window |p_go − p_ort| ≤ 1e-4
//
// The bound cannot be 0 and must not silently grow, so it is pinned
// here with its justification:
//
//   - Both implementations accumulate in fp32, but in different
//     orders: onnxruntime's MLAS kernels vectorise and use FMA where
//     the hardware has it; the Go port uses fixed sequential fp32.
//     fp32 summation is not associative, so bit-equality is not
//     achievable without re-implementing MLAS's exact blocking.
//   - Observed magnitude of that drift (this slice logs the max on
//     every run): ≤ 2e-5 worst-case over the goldensignals set on
//     onnxruntime 1.27.0 / osx-arm64 (most signals ≤ 6e-6),
//     consistent with k·ulp for dot products of length 128–256, plus
//     ~1e-7-level differences between Go's float64-rounded
//     transcendentals and MLAS's fp32 polynomial approximations.
//   - The final sigmoid CONTRACTS differences (|σ′| ≤ 1/4), so the
//     probability delta is smaller still than the pre-activation
//     drift.
//   - 1e-4 therefore clears the observed worst case ~5× while sitting
//     ~1000× below the smallest decision-relevant probability scale
//     in the package (the 0.15 default threshold/neg-threshold gap).
//
// Widening this bound requires measured evidence (a logged max-delta
// exceeding it on some platform/onnxruntime pairing), never
// convenience. The pinned oracle is onnxruntime 1.27.0 via
// github.com/yalue/onnxruntime_go v1.31.0 (see
// vad/internal/silero/VERSION).
package silero_ort
