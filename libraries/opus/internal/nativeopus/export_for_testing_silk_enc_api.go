package nativeopus

// Exports for the Phase 8 capstone SILK encoder parity test.
// TestParity_SilkEncode_Mono drives silk_Encode end-to-end and
// compares pulses + bitstream bytes against the C oracle.

// ExportTestSilkEncoder is an opaque wrapper around silk_encoder that
// lets the tests hold state across frame calls without exposing the
// full struct layout. Internally it's just a pointer.
type ExportTestSilkEncoder struct {
	st      silk_encoder
	control silk_EncControlStruct
}

// ExportTestSilkEncoder_New allocates a zeroed silk_encoder and
// initializes it via silk_InitEncoder.
func ExportTestSilkEncoder_New(channels int, arch int) *ExportTestSilkEncoder {
	e := &ExportTestSilkEncoder{}
	silk_InitEncoder(&e.st, opus_int(channels), arch, &e.control)
	return e
}

// ExportTestSilkEncoder_Configure fills the silk_EncControlStruct fields
// needed by silk_Encode and validates them via silk_control_encoder.
// Returns 0 on success or the SILK error code.
func (e *ExportTestSilkEncoder_Cfg) Apply(enc *ExportTestSilkEncoder) {
	enc.control.nChannelsAPI = opus_int32(e.NChannelsAPI)
	enc.control.nChannelsInternal = opus_int32(e.NChannelsInternal)
	enc.control.API_sampleRate = opus_int32(e.APISampleRate)
	enc.control.maxInternalSampleRate = opus_int32(e.MaxInternalSampleRate)
	enc.control.minInternalSampleRate = opus_int32(e.MinInternalSampleRate)
	enc.control.desiredInternalSampleRate = opus_int32(e.DesiredInternalSampleRate)
	enc.control.payloadSize_ms = opus_int(e.PayloadSizeMs)
	enc.control.bitRate = opus_int32(e.BitRate)
	enc.control.packetLossPercentage = opus_int(e.PacketLossPct)
	enc.control.complexity = opus_int(e.Complexity)
	enc.control.useInBandFEC = opus_int(e.UseInBandFEC)
	enc.control.LBRR_coded = opus_int(e.LBRRCoded)
	enc.control.useDTX = opus_int(e.UseDTX)
	enc.control.useCBR = opus_int(e.UseCBR)
	enc.control.maxBits = opus_int(e.MaxBits)
	enc.control.toMono = opus_int(e.ToMono)
	enc.control.opusCanSwitch = opus_int(e.OpusCanSwitch)
	enc.control.reducedDependency = opus_int(e.ReducedDependency)
	enc.control.internalSampleRate = opus_int32(e.InternalSampleRate)
}

// ExportTestSilkEncoder_Cfg mirrors the silk_EncControlStruct inputs
// the test drives. Using a flat struct keeps the test + cgo harness
// in lockstep.
type ExportTestSilkEncoder_Cfg struct {
	NChannelsAPI              int
	NChannelsInternal         int
	APISampleRate             int
	MaxInternalSampleRate     int
	MinInternalSampleRate     int
	DesiredInternalSampleRate int
	PayloadSizeMs             int
	BitRate                   int
	PacketLossPct             int
	Complexity                int
	UseInBandFEC              int
	LBRRCoded                 int
	UseDTX                    int
	UseCBR                    int
	MaxBits                   int
	ToMono                    int
	OpusCanSwitch             int
	ReducedDependency         int
	InternalSampleRate        int
}

// ExportTestSilkEncoder_EncodeFrame runs one silk_Encode on the
// configured state. Returns (nBytesOut, ret). The range coder output is
// written into pkt.
func ExportTestSilkEncoder_EncodeFrame(
	enc *ExportTestSilkEncoder,
	cfg ExportTestSilkEncoder_Cfg,
	pcm []float32,
	pkt []byte,
	prefillFlag int,
	activity int,
) (int, []byte, []int8, int, uint32) {
	cfg.Apply(enc)

	var rng ec_enc
	ec_enc_init(&rng, pkt, opus_uint32(len(pkt)))

	nBytesOut := opus_int32(len(pkt))
	ret := silk_Encode(&enc.st, &enc.control,
		pcm, opus_int(len(pcm)), &rng,
		&nBytesOut, opus_int(prefillFlag), opus_int(activity))

	ec_enc_done(&rng)

	// Capture pulses for parity check.
	nb := int(enc.st.state_Fxx[0].sCmn.frame_length)
	pulses := make([]int8, nb)
	for i := 0; i < nb; i++ {
		pulses[i] = int8(enc.st.state_Fxx[0].sCmn.pulses[i])
	}

	// Capture bytes written (the range coder has finalized rng.offs /
	// rng.buf). nBytesOut is pnBytesOut payload length; for silk_Encode
	// with prefillFlag==0 it's (ec_tell(rng) + 7)/8.
	used := int(nBytesOut)
	if used < 0 {
		used = 0
	}
	out := make([]byte, used)
	copy(out, pkt[:used])

	return int(ret), out, pulses, int(enc.st.state_Fxx[0].sCmn.indices.signalType), uint32(rng.rng)
}
