package nativeopus

// Test shims for libopus/src/extensions.c.
//
// The benchcmp parity tests for opus_packet_extensions_* drive
// both the C oracle and the Go port with the same extension
// payloads and assert byte-for-byte / field-for-field parity. The
// shims below expose the unexported generate/count/parse entry
// points and the opaque opus_extension_data struct.

// ExportExtensionData mirrors the C opus_extension_data struct
// exactly (including the separate `data` slice + `len` pair) so
// tests can construct inputs and inspect outputs without
// reaching into the unexported type.
type ExportExtensionData struct {
	ID    int
	Frame int
	Data  []byte
	Len   int32
}

func toInternalExt(e ExportExtensionData) opus_extension_data {
	return opus_extension_data{
		id:    e.ID,
		frame: e.Frame,
		data:  e.Data,
		len:   opus_int32(e.Len),
	}
}

func fromInternalExt(e opus_extension_data) ExportExtensionData {
	return ExportExtensionData{
		ID:    e.id,
		Frame: e.frame,
		Data:  e.data,
		Len:   int32(e.len),
	}
}

// ExportTestOpusPacketExtensionsGenerate wraps opus_packet_extensions_generate.
func ExportTestOpusPacketExtensionsGenerate(data []byte, maxlen int32,
	extensions []ExportExtensionData, nb_frames int, pad int) int32 {
	in := make([]opus_extension_data, len(extensions))
	for i, e := range extensions {
		in[i] = toInternalExt(e)
	}
	return int32(opus_packet_extensions_generate(data, opus_int32(maxlen),
		in, opus_int32(len(in)), nb_frames, pad))
}

// ExportTestOpusPacketExtensionsCount wraps opus_packet_extensions_count.
func ExportTestOpusPacketExtensionsCount(data []byte, len_ int32, nb_frames int) int32 {
	return int32(opus_packet_extensions_count(data, opus_int32(len_), nb_frames))
}

// ExportTestOpusPacketExtensionsCountExt wraps opus_packet_extensions_count_ext.
func ExportTestOpusPacketExtensionsCountExt(data []byte, len_ int32,
	nb_frame_exts []int32, nb_frames int) int32 {
	tmp := make([]opus_int32, len(nb_frame_exts))
	ret := opus_packet_extensions_count_ext(data, opus_int32(len_), tmp, nb_frames)
	for i := range nb_frame_exts {
		nb_frame_exts[i] = int32(tmp[i])
	}
	return int32(ret)
}

// ExportTestOpusPacketExtensionsParse wraps opus_packet_extensions_parse.
// `nb_extensions` is an in/out buffer-size/result-count parameter.
func ExportTestOpusPacketExtensionsParse(data []byte, len_ int32,
	extensions []ExportExtensionData, nb_extensions *int32, nb_frames int) int32 {
	out := make([]opus_extension_data, len(extensions))
	cap_ := opus_int32(*nb_extensions)
	ret := opus_packet_extensions_parse(data, opus_int32(len_), out, &cap_, nb_frames)
	*nb_extensions = int32(cap_)
	if ret >= 0 {
		for i := opus_int32(0); i < cap_; i++ {
			extensions[i] = fromInternalExt(out[i])
		}
	}
	return int32(ret)
}

// ExportTestOpusPacketExtensionsParseExt wraps opus_packet_extensions_parse_ext.
func ExportTestOpusPacketExtensionsParseExt(data []byte, len_ int32,
	extensions []ExportExtensionData, nb_extensions *int32,
	nb_frame_exts []int32, nb_frames int) int32 {
	out := make([]opus_extension_data, len(extensions))
	cap_ := opus_int32(*nb_extensions)
	nfe := make([]opus_int32, len(nb_frame_exts))
	for i, v := range nb_frame_exts {
		nfe[i] = opus_int32(v)
	}
	ret := opus_packet_extensions_parse_ext(data, opus_int32(len_), out, &cap_,
		nfe, nb_frames)
	*nb_extensions = int32(cap_)
	if ret >= 0 {
		for i := opus_int32(0); i < cap_; i++ {
			extensions[i] = fromInternalExt(out[i])
		}
	}
	return int32(ret)
}
