//go:build !arm64 || flac_strict

package nativeflac

// windowDataMulNEON is unreachable when no NEON window-multiply kernel is
// compiled in (non-arm64, or the flac_strict build which must stay scalar +
// bit-exact). windowMulNEONAvailable is false so LPCWindowData never calls it;
// the symbol exists purely so the package compiles on every build.
func windowDataMulNEON(in *int32, window *float32, out *float32, n int) int {
	panic("windowDataMulNEON called on a build without the NEON kernel")
}

// windowMulNEONAvailable reports that no NEON window-multiply kernel is present.
const windowMulNEONAvailable = false
