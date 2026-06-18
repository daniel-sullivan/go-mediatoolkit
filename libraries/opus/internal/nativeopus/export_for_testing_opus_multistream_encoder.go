package nativeopus

// Test shims for opus_multistream_encoder.
//
// These wrappers expose the ported multistream encoder (and a scalar
// state snapshot struct) so benchcmp can drive byte-exact parity tests
// against the C oracle's opus_multistream_encoder_* APIs.

// OpusMSEncoderStateSnapshot mirrors the scalar fields of OpusMSEncoder
// that a C init would populate, plus the derived arena size.
type OpusMSEncoderStateSnapshot struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	Mapping          [256]byte
	Arch             int
	LfeStream        int
	Application      int
	Fs               int32
	VariableDuration int
	MappingType      int
	BitrateBps       int32
	ArenaSize        int32
}

func snapshotMSEncoder(st *OpusMSEncoder) OpusMSEncoderStateSnapshot {
	s := OpusMSEncoderStateSnapshot{
		NbChannels:       st.layout.nb_channels,
		NbStreams:        st.layout.nb_streams,
		NbCoupledStreams: st.layout.nb_coupled_streams,
		Arch:             st.arch,
		LfeStream:        st.lfe_stream,
		Application:      st.application,
		Fs:               int32(st.Fs),
		VariableDuration: st.variable_duration,
		MappingType:      int(st.mapping_type),
		BitrateBps:       int32(st.bitrate_bps),
	}
	s.Mapping = st.layout.mapping
	return s
}

// ExportMSEncoderInitAndSnapshot mirrors opus_multistream_encoder_init
// and returns the post-init snapshot.
func ExportMSEncoderInitAndSnapshot(Fs int32, channels, streams, coupledStreams int,
	mapping []byte, application int) (OpusMSEncoderStateSnapshot, int) {
	var snap OpusMSEncoderStateSnapshot
	size := opus_multistream_encoder_get_size(streams, coupledStreams)
	if size <= 0 {
		return snap, OPUS_INTERNAL_ERROR
	}
	st := &OpusMSEncoder{}
	ret := opus_multistream_encoder_init(st, opus_int32(Fs), channels, streams,
		coupledStreams, mapping, application)
	if ret != OPUS_OK {
		return snap, ret
	}
	snap = snapshotMSEncoder(st)
	snap.ArenaSize = int32(size)
	return snap, OPUS_OK
}

// ExportMSSurroundEncoderInitAndSnapshot mirrors
// opus_multistream_surround_encoder_init.
func ExportMSSurroundEncoderInitAndSnapshot(Fs int32, channels, mappingFamily int,
	application int) (OpusMSEncoderStateSnapshot, []byte, int, int, int) {
	var snap OpusMSEncoderStateSnapshot
	var streams, coupled int
	mapping := make([]byte, 256)
	st := &OpusMSEncoder{}
	ret := opus_multistream_surround_encoder_init(st, opus_int32(Fs), channels,
		mappingFamily, &streams, &coupled, mapping, application)
	if ret != OPUS_OK {
		return snap, mapping[:channels], streams, coupled, ret
	}
	snap = snapshotMSEncoder(st)
	size := opus_multistream_surround_encoder_get_size(channels, mappingFamily)
	snap.ArenaSize = int32(size)
	return snap, mapping[:channels], streams, coupled, OPUS_OK
}

// ExportMSEncoderCreate mirrors opus_multistream_encoder_create and
// additionally installs the built-in CELT mode on every sub-encoder so
// that byte-exact encoding parity is achievable without a benchcmp
// side detour.
func ExportMSEncoderCreate(Fs int32, channels, streams, coupledStreams int,
	mapping []byte, application int, installMode func(*OpusEncoder) int) (*OpusMSEncoder, int) {
	var err int
	st := opus_multistream_encoder_create(opus_int32(Fs), channels, streams,
		coupledStreams, mapping, application, &err)
	if err != OPUS_OK {
		return nil, err
	}
	if installMode != nil {
		for _, enc := range st.encoders {
			if ret := installMode(enc); ret != OPUS_OK {
				return nil, ret
			}
		}
	}
	return st, OPUS_OK
}

// ExportMSSurroundEncoderCreate mirrors opus_multistream_surround_encoder_create.
func ExportMSSurroundEncoderCreate(Fs int32, channels, mappingFamily int, application int,
	installMode func(*OpusEncoder) int) (*OpusMSEncoder, []byte, int, int, int) {
	var err int
	var streams, coupled int
	mapping := make([]byte, 256)
	st := opus_multistream_surround_encoder_create(opus_int32(Fs), channels,
		mappingFamily, &streams, &coupled, mapping, application, &err)
	if err != OPUS_OK {
		return nil, mapping[:channels], streams, coupled, err
	}
	if installMode != nil {
		for _, enc := range st.encoders {
			if ret := installMode(enc); ret != OPUS_OK {
				return nil, mapping[:channels], streams, coupled, ret
			}
		}
	}
	return st, mapping[:channels], streams, coupled, OPUS_OK
}

// ExportMSEncoderCtl forwards a multistream CTL request.
func ExportMSEncoderCtl(st *OpusMSEncoder, request int, args ...interface{}) int {
	return opus_multistream_encoder_ctl(st, request, args...)
}

// ExportMSEncodeFloat wraps opus_multistream_encode_float.
func ExportMSEncodeFloat(st *OpusMSEncoder, pcm []float32, frameSize int,
	data []byte, maxDataBytes int32) int32 {
	return int32(opus_multistream_encode_float(st, pcm, frameSize, data, opus_int32(maxDataBytes)))
}

// ExportMSEncodeInt16 wraps opus_multistream_encode.
func ExportMSEncodeInt16(st *OpusMSEncoder, pcm []int16, frameSize int,
	data []byte, maxDataBytes int32) int32 {
	p := make([]opus_int16, len(pcm))
	for i, v := range pcm {
		p[i] = opus_int16(v)
	}
	return int32(opus_multistream_encode(st, p, frameSize, data, opus_int32(maxDataBytes)))
}

// ExportMSEncoderSnapshot returns the current encoder state snapshot.
func ExportMSEncoderSnapshot(st *OpusMSEncoder) OpusMSEncoderStateSnapshot {
	return snapshotMSEncoder(st)
}

// ExportMSEncoderConstants exposes the CTL request codes the tests use.
const (
	ExportMULTISTREAM_GET_ENCODER_STATE = OPUS_MULTISTREAM_GET_ENCODER_STATE_REQUEST
)
