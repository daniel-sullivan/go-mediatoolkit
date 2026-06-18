package nativeopus

// Port of libopus/celt/os_support.h.
//
// The C header provides a "tiny OS abstraction layer" — malloc/free
// wrappers (opus_alloc / opus_realloc / opus_free / opus_alloc_scratch)
// and the OPUS_COPY / OPUS_MOVE / OPUS_CLEAR helpers. Go has no need
// for the alloc wrappers (use `make` / `new` at call sites; GC handles
// free), so they are deliberately not ported. The three element-wise
// helpers are ported as Go generics so every C call site ports 1:1
// while benefiting from compile-time type checking equivalent to the
// `0*((dst)-(src))` trick the C macros use.

// OPUS_COPY copies n elements from src to dst (non-overlapping, like memcpy).
// C: `memcpy((dst), (src), (n)*sizeof(*(dst)) + 0*((dst)-(src)))`.
func OPUS_COPY[T any](dst, src []T, n int) {
	copy(dst[:n], src[:n])
}

// OPUS_MOVE copies n elements from src to dst, allowing overlapping
// regions (like memmove). Go's built-in copy already handles overlap
// correctly, so the implementation is identical to OPUS_COPY — the
// separate name is kept to preserve the 1:1 mapping with the C source.
// C: `memmove((dst), (src), (n)*sizeof(*(dst)) + 0*((dst)-(src)))`.
func OPUS_MOVE[T any](dst, src []T, n int) {
	copy(dst[:n], src[:n])
}

// OPUS_CLEAR sets n elements of dst to the zero value for T.
// C: `memset((dst), 0, (n)*sizeof(*(dst)))`.
func OPUS_CLEAR[T any](dst []T, n int) {
	clear(dst[:n])
}
