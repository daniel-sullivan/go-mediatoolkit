//go:build linux

// Registers the Linux hardware backends into the default registry at
// package load. NVENC (NVIDIA GPUs) is registered first, then VAAPI
// (Intel/AMD GPUs via libva); a backend whose construction fails (libcuda /
// libva not installed, no device) is simply skipped, and Open* falls back to
// software / errors per policy. Registration order is selection-preference
// order (see policy.go).

package hwaccel

func init() {
	// NVENC/NVDEC: NVIDIA GPU encode+decode via the CUDA driver + NVENCODE +
	// cuvid libraries. Skipped on hosts with no NVIDIA driver (libcuda fails
	// to dlopen) or no CUDA device. The hardware path is written against the
	// Video Codec SDK 13.0 ABI but is unverified on hardware — see the status
	// banner in nvenc_linux.go.
	if b, err := newNVBackend(); err == nil {
		defaultRegistry.Register(b)
	}

	// VAAPI: Intel/AMD GPU encode+decode via libva + the DRM render node.
	if b, err := newVABackend(); err == nil {
		defaultRegistry.Register(b)
	}

	// V4L2 stateless + stateful M2M codec backend (Linux SoCs: Pi 5
	// rpi-hevc-dec via the Request API, Pi 4 / Rockchip via the stateful
	// state machine). Registered after VAAPI; skipped on hosts with no M2M
	// multiplanar video node. See v4l2_linux.go.
	if b, err := newV4L2Backend(); err == nil {
		defaultRegistry.Register(b)
	}
}
