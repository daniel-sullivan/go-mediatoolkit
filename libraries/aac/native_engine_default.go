//go:build !aacfdk

package aac

// In the default build (the aacfdk tag is absent) the internal/nativeaac port —
// a 1:1 translation of the FDK-derived reference — is fenced out, so even the
// always-available NewNativeDecoder cannot construct a decode engine. It
// surfaces ErrEngineRequiresFDK, matching the tag-routed NewDecoder. Rebuild
// with `-tags aacfdk` to link the pure-Go port.
func newNativeDecodeEngine(frameSamples, sampleRate, channels int) (nativeDecodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}

// newNativeSbrDecodeEngine likewise surfaces ErrEngineRequiresFDK in the default
// build: the FDK-derived HE-AAC v1 (SBR) port is fenced behind the aacfdk tag.
func newNativeSbrDecodeEngine(coreFrameLen, coreRate, channels, outRate int) (nativeDecodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}

// newNativePsDecodeEngine likewise surfaces ErrEngineRequiresFDK in the default
// build: the FDK-derived HE-AAC v2 (SBR + parametric stereo) port is fenced
// behind the aacfdk tag.
func newNativePsDecodeEngine(coreFrameLen, coreRate, outRate int) (nativeDecodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}

// newNativeEncodeEngine likewise surfaces ErrEngineRequiresFDK in the default
// build: the FDK-derived 1:1 encode port is fenced behind the aacfdk tag, so
// the always-available NewNativeEncoder cannot construct an encode engine.
func newNativeEncodeEngine(sampleRate, channels, bitRate, vbrMode int) (nativeEncodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}

// newNativeSbrEncodeEngine likewise surfaces ErrEngineRequiresFDK in the default
// build: the FDK-derived HE-AAC v1 (AOT-5) SBR encode port is fenced behind the
// aacfdk tag.
func newNativeSbrEncodeEngine(sampleRate, channels, bitRate int) (nativeSbrEncodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}

// newNativePsEncodeEngine likewise surfaces ErrEngineRequiresFDK in the default
// build: the FDK-derived HE-AAC v2 (AOT-29) SBR+PS encode port is fenced behind
// the aacfdk tag.
func newNativePsEncodeEngine(sampleRate, bitRate int) (nativePsEncodeEngine, error) {
	return nil, ErrEngineRequiresFDK
}
