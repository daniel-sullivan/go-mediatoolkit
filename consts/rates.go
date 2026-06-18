package consts

// Named constants for the sample rates the toolkit cares about. Prefer
// these over magic numbers in callers and tests — they document intent
// and make matrix tests trivial to extend.
const (
	SampleRate8000   = 8000   // telephony / narrowband voice
	SampleRate16000  = 16000  // speech / wideband voice
	SampleRate22050  = 22050  // half-CD
	SampleRate24000  = 24000  // half-DAW default; Opus medium band
	SampleRate32000  = 32000  // digital broadcast, DAT-lo
	SampleRate44100  = 44100  // CD audio
	SampleRate48000  = 48000  // DAT / DAW default / professional video
	SampleRate88200  = 88200  // 2× CD
	SampleRate96000  = 96000  // 2× DAW default
	SampleRate176400 = 176400 // 4× CD
	SampleRate192000 = 192000 // hi-res mastering
)

// CommonSampleRates enumerates the rates the toolkit validates its
// algorithms against. Test matrices and format-pickers should iterate
// this list so new supported rates only need to be added in one place.
var CommonSampleRates = []int{
	SampleRate22050,
	SampleRate32000,
	SampleRate44100,
	SampleRate48000,
	SampleRate88200,
	SampleRate96000,
	SampleRate192000,
}
