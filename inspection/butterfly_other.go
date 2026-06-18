//go:build !arm64 && !amd64

package inspection

func butterflyPass(x []complex128, tw []complex128, half, step int) {
	n := len(x)
	size := half << 1
	for i := 0; i < n; i += size {
		for j := 0; j < half; j++ {
			w := tw[j*step]
			u := x[i+j]
			v := x[i+j+half] * w
			x[i+j] = u + v
			x[i+j+half] = u - v
		}
	}
}
