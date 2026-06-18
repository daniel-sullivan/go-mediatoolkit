//go:build darwin

// Annex-B NAL parsing and AVCC re-framing for the VideoToolbox decoder.
// The encoder emits start-code-prefixed (Annex-B) elementary streams;
// VideoToolbox's CMSampleBuffer wants length-prefixed (AVCC) NAL units
// and the parameter sets carried out of band in the format description.
// These helpers bridge the two: split an access unit into its NALs,
// classify the parameter sets out of it, and re-prefix the remaining VCL
// NALs to 4-byte lengths.

package hwaccel

import "go-mediatoolkit/video"

// splitAnnexB splits an Annex-B byte stream into its raw NAL units
// (start codes stripped). It recognises both the 3-byte (00 00 01) and
// 4-byte (00 00 00 01) start codes anywhere in the stream. Empty units
// are dropped.
func splitAnnexB(b []byte) [][]byte {
	var nals [][]byte
	i := 0
	n := len(b)
	// Advance to the first start code.
	start := -1
	for i+3 <= n {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			start = i + 3
			i = start
			break
		}
		if i+4 <= n && b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 {
			start = i + 4
			i = start
			break
		}
		i++
	}
	if start < 0 {
		return nil
	}
	for i+3 <= n {
		// A start code begins the next NAL.
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if i > start {
				nals = appendNAL(nals, b[start:i])
			}
			start = i + 3
			i = start
			continue
		}
		if i+4 <= n && b[i] == 0 && b[i+1] == 0 && b[i+2] == 0 && b[i+3] == 1 {
			if i > start {
				nals = appendNAL(nals, b[start:i])
			}
			start = i + 4
			i = start
			continue
		}
		i++
	}
	if start < n {
		nals = appendNAL(nals, b[start:n])
	}
	return nals
}

// appendNAL trims any trailing zero bytes (a 4-byte start code's leading
// zero can be left dangling by the 3-byte scan) and appends a non-empty
// NAL.
func appendNAL(nals [][]byte, nal []byte) [][]byte {
	for len(nal) > 0 && nal[len(nal)-1] == 0 {
		nal = nal[:len(nal)-1]
	}
	if len(nal) == 0 {
		return nals
	}
	return append(nals, nal)
}

// nalType returns the codec-specific NAL unit type of a raw NAL:
// for H.264 it is the low 5 bits of byte 0; for H.265 it is bits 1..6 of
// byte 0.
func (d *vtDecoder) nalType(nal []byte) int {
	if len(nal) == 0 {
		return -1
	}
	if d.codec == video.H265 {
		return int((nal[0] >> 1) & 0x3f)
	}
	return int(nal[0] & 0x1f)
}

// H.264 / H.265 NAL type numbers used to route parameter sets and slices.
const (
	// H.264 (nal[0] & 0x1f).
	h264NALSPS = 7
	h264NALPPS = 8
	h264NALAUD = 9

	// H.265 ((nal[0] >> 1) & 0x3f).
	h265NALVPS = 32
	h265NALSPS = 33
	h265NALPPS = 34
	h265NALAUD = 35
)

// classifyParameterSets pulls the parameter-set NALs out of an access
// unit's NAL list, returning VPS (H.265 only), SPS, and PPS groups in
// stream order. Non-parameter-set NALs are ignored here.
func (d *vtDecoder) classifyParameterSets(nals [][]byte) (vps, sps, pps [][]byte) {
	for _, nal := range nals {
		t := d.nalType(nal)
		if d.codec == video.H265 {
			switch t {
			case h265NALVPS:
				vps = append(vps, nal)
			case h265NALSPS:
				sps = append(sps, nal)
			case h265NALPPS:
				pps = append(pps, nal)
			}
			continue
		}
		switch t {
		case h264NALSPS:
			sps = append(sps, nal)
		case h264NALPPS:
			pps = append(pps, nal)
		}
	}
	return vps, sps, pps
}

// isParameterSetOrDelimiter reports whether a NAL is a parameter set or
// an access-unit delimiter — i.e. something carried out of band (in the
// format description) rather than submitted as picture data.
func (d *vtDecoder) isParameterSetOrDelimiter(nal []byte) bool {
	t := d.nalType(nal)
	if d.codec == video.H265 {
		switch t {
		case h265NALVPS, h265NALSPS, h265NALPPS, h265NALAUD:
			return true
		}
		return false
	}
	switch t {
	case h264NALSPS, h264NALPPS, h264NALAUD:
		return true
	}
	return false
}

// vclToAVCC re-frames the picture NALs of an access unit (everything that
// is not a parameter set or AU delimiter) into AVCC: each NAL prefixed
// with its 4-byte big-endian length. Returns an empty slice when the AU
// carries no picture NALs (e.g. a parameter-set-only packet).
func (d *vtDecoder) vclToAVCC(nals [][]byte) []byte {
	var total int
	for _, nal := range nals {
		if d.isParameterSetOrDelimiter(nal) {
			continue
		}
		total += 4 + len(nal)
	}
	if total == 0 {
		return nil
	}
	out := make([]byte, 0, total)
	for _, nal := range nals {
		if d.isParameterSetOrDelimiter(nal) {
			continue
		}
		n := len(nal)
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		out = append(out, nal...)
	}
	return out
}

// paramSetArrays builds the parallel pointer/size arrays the
// CMVideoFormatDescriptionCreateFrom*ParameterSets calls expect, plus a
// pinning slice that the caller must keep alive across the call (the
// arrays alias the backing NAL bytes). The pointer array points at the
// first byte of each parameter set.
func paramSetArrays(sets [][]byte) (ptrs **byte, sizes *uint64, pin [][]byte) {
	if len(sets) == 0 {
		return nil, nil, nil
	}
	ptrSlice := make([]*byte, len(sets))
	sizeSlice := make([]uint64, len(sets))
	for i, s := range sets {
		ptrSlice[i] = &s[0]
		sizeSlice[i] = uint64(len(s))
	}
	return &ptrSlice[0], &sizeSlice[0], sets
}

// paramsEqual reports whether two parameter-set groups are byte-identical
// in the same order — used to decide whether a keyframe's parameter sets
// match the live session's.
func paramsEqual(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

// cloneParams deep-copies a parameter-set group so the cached copy is
// independent of the caller's backing buffer.
func cloneParams(sets [][]byte) [][]byte {
	if len(sets) == 0 {
		return nil
	}
	out := make([][]byte, len(sets))
	for i, s := range sets {
		out[i] = append([]byte(nil), s...)
	}
	return out
}
