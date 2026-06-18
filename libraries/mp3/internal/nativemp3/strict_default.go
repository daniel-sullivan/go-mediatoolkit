//go:build !mp3_strict

package nativemp3

// StrictMode is false in the production build. The default build lets Go's
// backend fuse a*b+c into an FMA and otherwise optimize the floating-point
// dequantization freely; its output is within PSNR noise of minimp3 but is
// not a bit-exact target. The mp3_strict build (strict.go) is the bit-exact
// parity build.
const StrictMode = false
