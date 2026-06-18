package opus

// PacketInfo contains information extracted from an Opus packet header.
type PacketInfo struct {
	Mode          Mode      // Operating mode (SILK, CELT, Hybrid).
	Bandwidth     Bandwidth // Audio bandwidth.
	FrameDuration float64   // Frame duration in milliseconds (2.5, 5, 10, 20, 40, or 60).
	FrameCount    int       // Number of frames in the packet.
	Stereo        bool      // True if the packet contains stereo audio.
}

// Frame is a single encoded frame extracted from an Opus packet.
type Frame struct {
	Data []byte // The raw frame payload (sub-slice of the original packet).
}

// ParsePacket extracts header information from an Opus packet without decoding.
func ParsePacket(data []byte) (PacketInfo, error) {
	info, _, err := parsePacket(data)
	return info, err
}

// SamplesPerFrame returns the number of samples per channel for the given
// frame duration (in ms) at the given sample rate.
func SamplesPerFrame(durationMs float64, sampleRate int) int {
	// Convert ms to samples: duration_ms * sampleRate / 1000
	// Use integer arithmetic to avoid floating point issues.
	switch durationMs {
	case 2.5:
		return sampleRate / 400
	case 5:
		return sampleRate / 200
	case 10:
		return sampleRate / 100
	case 20:
		return sampleRate / 50
	case 40:
		return sampleRate / 25
	case 60:
		return sampleRate * 60 / 1000
	default:
		return 0
	}
}

// parsePacket is the internal packet parser. It returns both the PacketInfo
// and the extracted frames. Port of opus_packet_parse_impl from src/opus.c.
func parsePacket(data []byte) (PacketInfo, []Frame, error) {
	if len(data) == 0 {
		return PacketInfo{}, nil, ErrInvalidPacket
	}

	toc := data[0]
	info := PacketInfo{
		Mode:      packetMode(toc),
		Bandwidth: packetBandwidth(toc),
		Stereo:    toc&0x04 != 0,
	}
	info.FrameDuration = packetFrameDuration(toc)

	payload := data[1:]
	remaining := len(payload)

	var sizes []int
	var frameCount int

	code := toc & 0x3
	switch code {
	case 0:
		// One frame.
		frameCount = 1
		sizes = []int{remaining}

	case 1:
		// Two CBR frames.
		frameCount = 2
		if remaining%2 != 0 {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		half := remaining / 2
		if half > MaxFrameBytes {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		sizes = []int{half, half}

	case 2:
		// Two VBR frames.
		frameCount = 2
		s1, consumed := parseSize(payload, remaining)
		if s1 < 0 || s1 > remaining-consumed {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		payload = payload[consumed:]
		remaining -= consumed
		s2 := remaining - s1
		if s2 < 0 || s2 > MaxFrameBytes {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		sizes = []int{s1, s2}

	case 3:
		// Multiple CBR/VBR frames (0 to 120 ms).
		if remaining < 1 {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		ch := payload[0]
		payload = payload[1:]
		remaining--

		frameCount = int(ch & 0x3F)

		// Validate frame count and total duration.
		frameSamples48k := samplesPerFrameAt48k(toc)
		if frameCount <= 0 || frameSamples48k*frameCount > MaxPacketDuration {
			return PacketInfo{}, nil, ErrInvalidPacket
		}

		// Parse padding (bit 6).
		if ch&0x40 != 0 {
			for {
				if remaining <= 0 {
					return PacketInfo{}, nil, ErrInvalidPacket
				}
				p := int(payload[0])
				payload = payload[1:]
				remaining--
				pad := p
				if p == 255 {
					pad = 254
				}
				remaining -= pad
				if p != 255 {
					break
				}
			}
		}
		if remaining < 0 {
			return PacketInfo{}, nil, ErrInvalidPacket
		}

		cbr := ch&0x80 == 0

		sizes = make([]int, frameCount)
		if !cbr {
			// VBR: parse sizes for all but last frame.
			lastSize := remaining
			for i := 0; i < frameCount-1; i++ {
				s, consumed := parseSize(payload, remaining)
				if s < 0 || s > remaining-consumed {
					return PacketInfo{}, nil, ErrInvalidPacket
				}
				sizes[i] = s
				payload = payload[consumed:]
				remaining -= consumed
				lastSize -= consumed + s
			}
			if lastSize < 0 || lastSize > MaxFrameBytes {
				return PacketInfo{}, nil, ErrInvalidPacket
			}
			sizes[frameCount-1] = lastSize
		} else {
			// CBR: all frames equal size.
			frameSize := remaining / frameCount
			if frameSize*frameCount != remaining {
				return PacketInfo{}, nil, ErrInvalidPacket
			}
			if frameSize > MaxFrameBytes {
				return PacketInfo{}, nil, ErrInvalidPacket
			}
			for i := range sizes {
				sizes[i] = frameSize
			}
		}
	}

	info.FrameCount = frameCount

	// Extract frame data.
	frames := make([]Frame, frameCount)
	for i := 0; i < frameCount; i++ {
		if sizes[i] > len(payload) {
			return PacketInfo{}, nil, ErrInvalidPacket
		}
		frames[i] = Frame{Data: payload[:sizes[i]]}
		payload = payload[sizes[i]:]
	}

	return info, frames, nil
}

// parseSize parses a 1 or 2-byte frame size from the payload.
// Returns the size and number of bytes consumed, or (-1, -1) on error.
func parseSize(data []byte, remaining int) (size int, consumed int) {
	if remaining < 1 {
		return -1, -1
	}
	if data[0] < 252 {
		return int(data[0]), 1
	}
	if remaining < 2 {
		return -1, -1
	}
	return 4*int(data[1]) + int(data[0]), 2
}

// packetMode extracts the operating mode from a TOC byte.
func packetMode(toc byte) Mode {
	if toc&0x80 != 0 {
		return ModeCELTOnly
	}
	if toc&0x60 == 0x60 {
		return ModeHybrid
	}
	return ModeSILKOnly
}

// packetBandwidth extracts the bandwidth from a TOC byte.
func packetBandwidth(toc byte) Bandwidth {
	if toc&0x80 != 0 {
		// CELT mode.
		bw := (toc >> 5) & 0x3
		// Map: 0→NB, 1→WB, 2→SWB, 3→FB
		switch bw {
		case 0:
			return BandwidthNarrowband
		case 1:
			return BandwidthWideband
		case 2:
			return BandwidthSuperwideband
		case 3:
			return BandwidthFullband
		}
	} else if toc&0x60 == 0x60 {
		// Hybrid mode.
		if toc&0x10 != 0 {
			return BandwidthFullband
		}
		return BandwidthSuperwideband
	} else {
		// SILK mode.
		bw := (toc >> 5) & 0x3
		switch bw {
		case 0:
			return BandwidthNarrowband
		case 1:
			return BandwidthMediumband
		case 2:
			return BandwidthWideband
		}
	}
	return BandwidthNarrowband
}

// packetFrameDuration returns the frame duration in milliseconds from a TOC byte.
func packetFrameDuration(toc byte) float64 {
	if toc&0x80 != 0 {
		// CELT mode: bits 3-4 encode the frame size.
		switch (toc >> 3) & 0x3 {
		case 0:
			return 2.5
		case 1:
			return 5
		case 2:
			return 10
		case 3:
			return 20
		}
	} else if toc&0x60 == 0x60 {
		// Hybrid mode.
		if toc&0x08 != 0 {
			return 20
		}
		return 10
	} else {
		// SILK mode: bits 3-4 encode the frame size.
		switch (toc >> 3) & 0x3 {
		case 0:
			return 10
		case 1:
			return 20
		case 2:
			return 40
		case 3:
			return 60
		}
	}
	return 20 // default
}

// samplesPerFrameAt48k returns the frame size in samples at 48 kHz.
// Used for validation (max packet duration check).
func samplesPerFrameAt48k(toc byte) int {
	return SamplesPerFrame(packetFrameDuration(toc), Rate48000)
}
