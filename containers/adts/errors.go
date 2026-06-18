package adts

import "errors"

var (
	// ErrShortHeader indicates a buffer too small to hold an ADTS frame
	// header (7 bytes, or 9 with CRC).
	ErrShortHeader = errors.New("adts: short header")

	// ErrBadSyncword indicates the expected 12-bit ADTS syncword (0xFFF) was
	// not present at the parse position.
	ErrBadSyncword = errors.New("adts: bad syncword")

	// ErrBadFrameLength indicates the aac_frame_length field is inconsistent
	// — smaller than its own header, or larger than the 13-bit maximum.
	ErrBadFrameLength = errors.New("adts: bad frame length")

	// ErrNoSync indicates the Reader scanned its resync window without
	// finding a valid ADTS frame header.
	ErrNoSync = errors.New("adts: no syncword found")

	// ErrUnsupportedSampleRate indicates a sample rate with no MPEG-4
	// samplingFrequencyIndex (the explicit-frequency escape is not emitted
	// in an ADTS header).
	ErrUnsupportedSampleRate = errors.New("adts: unsupported sample rate")

	// ErrUnsupportedChannels indicates a channel count with no ADTS
	// channel-configuration mapping.
	ErrUnsupportedChannels = errors.New("adts: unsupported channel count")

	// ErrPacketTooLarge indicates an access unit whose framed length would
	// exceed the 13-bit aac_frame_length maximum.
	ErrPacketTooLarge = errors.New("adts: access unit too large for ADTS frame")

	// ErrBadArg indicates an invalid argument, such as a nil reader or
	// destination writer.
	ErrBadArg = errors.New("adts: invalid argument")
)
