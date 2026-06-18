//go:build darwin

// Registers the VideoToolbox backend into the default registry at
// package load on darwin. If the frameworks fail to dlopen (which should
// never happen on a real macOS install) the backend is simply not
// registered, and Open* falls back to software / errors per policy.

package hwaccel

func init() {
	b, err := newVTBackend()
	if err != nil {
		return
	}
	defaultRegistry.Register(b)
}
