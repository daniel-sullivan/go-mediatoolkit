//go:build linux

// Temporary AV1 encode/decode seams, replaced as the AV1 paths land. Kept in a
// dedicated file so the VP9 work compiles and runs independently while the AV1
// OBU parser/writer is built out.

package hwaccel

func (e *vaEncoder) buildAV1Params(add addFunc) error {
	return ErrUnsupportedCodec
}
