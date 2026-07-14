package rnnoise

// The RNNoise recurrent network forward pass, ported 1:1 from
// librnnoise/src/rnn.c (compute_rnn): two temporal convolutions feeding
// three GRUs, then two dense heads producing the 32 band gains and the
// VAD probability. State (conv/GRU memories) is carried across frames.

// rnnState mirrors rnn.h RNNState.
type rnnState struct {
	conv1State [130]float32 // CONV1_STATE_SIZE = 65*2
	conv2State [256]float32 // CONV2_STATE_SIZE = 128*2
	gru1State  [384]float32
	gru2State  [384]float32
	gru3State  [384]float32
}

// computeRnn is rnn.c compute_rnn. input is the NBFeatures feature
// vector; gains is filled with NBBands (32) band gains and vad with the
// single voice-activity probability.
func computeRnn(m *model, rnn *rnnState, gains, vad, input []float32) {
	tmp := make([]float32, 128)  // conv1 out / conv2 in (CONV1_OUT_SIZE)
	tmp2 := make([]float32, 384) // conv2 out / gru1 in (CONV2_OUT_SIZE)
	computeGenericConv1d(&m.conv1, tmp, rnn.conv1State[:], input, 65, activationTanh)
	computeGenericConv1d(&m.conv2, tmp2, rnn.conv2State[:], tmp, 128, activationTanh)
	computeGenericGru(&m.gru1Input, &m.gru1Recur, rnn.gru1State[:], tmp2)
	computeGenericGru(&m.gru2Input, &m.gru2Recur, rnn.gru2State[:], rnn.gru1State[:])
	computeGenericGru(&m.gru3Input, &m.gru3Recur, rnn.gru3State[:], rnn.gru2State[:])
	computeGenericDense(&m.denseOut, gains, rnn.gru3State[:], activationSigmoid)
	computeGenericDense(&m.vadDense, vad, rnn.gru3State[:], activationSigmoid)
}
