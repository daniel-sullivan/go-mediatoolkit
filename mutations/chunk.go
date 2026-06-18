package mutations

// Chunk splits a buffer into fixed-size chunks. The final chunk may be
// shorter than size if len(buf) is not evenly divisible.
//
// For multi-channel interleaved audio, size should be a multiple of the
// channel count to preserve frame alignment.
func Chunk(buf []float64, size int) [][]float64 {
	if size <= 0 || len(buf) == 0 {
		return nil
	}
	n := (len(buf) + size - 1) / size
	chunks := make([][]float64, 0, n)
	_ = ChunkFunc(buf, size, func(chunk []float64, _ bool) error {
		chunks = append(chunks, chunk)
		return nil
	})
	return chunks
}

// ChunkFunc iterates over buf in fixed-size chunks, calling fn for each one.
// The last parameter indicates whether this is the final chunk. If fn returns
// an error, iteration stops and the error is returned.
func ChunkFunc(buf []float64, size int, fn func(chunk []float64, last bool) error) error {
	if size <= 0 || len(buf) == 0 {
		return nil
	}
	for offset := 0; offset < len(buf); offset += size {
		end := offset + size
		if end > len(buf) {
			end = len(buf)
		}
		if err := fn(buf[offset:end], end >= len(buf)); err != nil {
			return err
		}
	}
	return nil
}
