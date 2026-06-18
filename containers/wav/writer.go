package wav

import (
	"bytes"
	"encoding/binary"
	"io"

	"go-mediatoolkit/mutations"
)

// Writer emits a RIFF/WAVE file to an io.WriteSeeker. It writes the RIFF +
// fmt + metadata chunks up front, then exposes an io.Writer over the data
// chunk payload. Close backpatches the RIFF and data chunk sizes.
type Writer struct {
	w            io.WriteSeeker
	closed       bool
	riffSizeOff  int64 // offset of the RIFF chunk size field
	dataSizeOff  int64 // offset of the data chunk size field
	dataStartOff int64 // offset of the first data payload byte
	written      uint32
	header       Header
	data         *dataWriter
}

// NewWriter constructs a Writer that emits a WAV file matching h.
// SampleRate, Channels and SampleFormat must be non-zero/valid; remaining
// Header fields are optional.
//
// w must be an [io.WriteSeeker] because RIFF chunk sizes are emitted as a
// fixed header and backpatched on Close.
func NewWriter(w io.WriteSeeker, h Header) (*Writer, error) {
	if h.SampleRate <= 0 || h.Channels <= 0 {
		return nil, ErrUnsupportedFormat
	}

	tag, bits, ok := formatTagFor(h.SampleFormat)
	if !ok {
		return nil, ErrUnsupportedFormat
	}
	blockAlign := uint16(h.Channels) * (bits / 8)
	byteRate := uint32(h.SampleRate) * uint32(blockAlign)

	// --- RIFF header ---
	if _, err := w.Write(idRIFF[:]); err != nil {
		return nil, err
	}
	riffSizeOff, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(make([]byte, 4)); err != nil { // placeholder
		return nil, err
	}
	if _, err := w.Write(idWAVE[:]); err != nil {
		return nil, err
	}

	// --- fmt chunk ---
	fc := fmtChunk{
		FormatTag:     tag,
		Channels:      uint16(h.Channels),
		SampleRate:    uint32(h.SampleRate),
		ByteRate:      byteRate,
		BlockAlign:    blockAlign,
		BitsPerSample: bits,
	}
	body := buildFmt(fc)
	if err := writeChunkHeader(w, idFMT, uint32(len(body))); err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		return nil, err
	}

	// --- optional metadata chunks before data ---
	if bext := h.Extra.Bext; bext != nil {
		if err := writeSubChunk(w, idBEXT, buildBext(bext)); err != nil {
			return nil, err
		}
	}
	if len(h.Extra.Cues) > 0 {
		if err := writeSubChunk(w, idCUE, buildCue(h.Extra.Cues)); err != nil {
			return nil, err
		}
	}
	if info := buildLISTInfo(h.Tags); info != nil {
		if err := writeSubChunk(w, idLIST, info); err != nil {
			return nil, err
		}
	}
	for ccStr, body := range h.Extra.Unknown {
		if len(ccStr) != 4 {
			continue
		}
		var cc [4]byte
		copy(cc[:], ccStr)
		if err := writeSubChunk(w, cc, body); err != nil {
			return nil, err
		}
	}

	// --- data chunk header ---
	if _, err := w.Write(idDATA[:]); err != nil {
		return nil, err
	}
	dataSizeOff, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(make([]byte, 4)); err != nil { // placeholder size
		return nil, err
	}
	dataStartOff, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	ww := &Writer{
		w:            w,
		riffSizeOff:  riffSizeOff,
		dataSizeOff:  dataSizeOff,
		dataStartOff: dataStartOff,
		header:       h,
	}
	ww.data = &dataWriter{ww: ww}
	return ww, nil
}

// Header returns the header this writer was configured with.
func (w *Writer) Header() Header { return w.header }

// Data returns an io.Writer over the WAV data chunk payload. Writes go
// directly to the underlying stream; Close backpatches the chunk sizes.
func (w *Writer) Data() io.Writer { return w.data }

// Close writes a trailing pad byte if needed, then backpatches the RIFF
// and data chunk sizes. It does NOT close the underlying writer.
func (w *Writer) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true

	// Pad data chunk to even length.
	if w.written%2 == 1 {
		if _, err := w.w.Write([]byte{0}); err != nil {
			return err
		}
	}

	endOff, err := w.w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	// data chunk size
	if _, err := w.w.Seek(w.dataSizeOff, io.SeekStart); err != nil {
		return err
	}
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], w.written)
	if _, err := w.w.Write(sz[:]); err != nil {
		return err
	}

	// RIFF size = endOff - 8 (everything after the initial "RIFF" + size).
	riffSize := uint32(endOff - 8)
	if _, err := w.w.Seek(w.riffSizeOff, io.SeekStart); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(sz[:], riffSize)
	if _, err := w.w.Write(sz[:]); err != nil {
		return err
	}

	// Restore position at file end.
	_, err = w.w.Seek(endOff, io.SeekStart)
	return err
}

// dataWriter forwards to the underlying writer and counts bytes so Close
// can backpatch the data chunk size.
type dataWriter struct {
	ww *Writer
}

func (d *dataWriter) Write(p []byte) (int, error) {
	if d.ww.closed {
		return 0, ErrAlreadyClosed
	}
	n, err := d.ww.w.Write(p)
	d.ww.written += uint32(n)
	return n, err
}

// formatTagFor maps a sample format to (wFormatTag, bits, ok).
func formatTagFor(f mutations.SampleFormat) (tag uint16, bits uint16, ok bool) {
	switch f {
	case mutations.FormatUint8:
		return formatPCM, 8, true
	case mutations.FormatInt16:
		return formatPCM, 16, true
	case mutations.FormatInt24:
		return formatPCM, 24, true
	case mutations.FormatInt32:
		return formatPCM, 32, true
	case mutations.FormatFloat32:
		return formatIEEEFloat, 32, true
	case mutations.FormatFloat64:
		return formatIEEEFloat, 64, true
	}
	return 0, 0, false
}

// writeSubChunk writes an ID + size + body + optional pad byte.
func writeSubChunk(w io.Writer, id [4]byte, body []byte) error {
	if err := writeChunkHeader(w, id, uint32(len(body))); err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if len(body)&1 == 1 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

// buildCue serialises a "cue " chunk body from the given cue points.
func buildCue(cues []CuePoint) []byte {
	var buf bytes.Buffer
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(cues)))
	buf.Write(count[:])
	for _, cp := range cues {
		var rec [24]byte
		binary.LittleEndian.PutUint32(rec[0:4], cp.ID)
		binary.LittleEndian.PutUint32(rec[4:8], cp.Position)
		copy(rec[8:12], cp.DataChunkID[:])
		binary.LittleEndian.PutUint32(rec[12:16], cp.ChunkStart)
		binary.LittleEndian.PutUint32(rec[16:20], cp.BlockStart)
		binary.LittleEndian.PutUint32(rec[20:24], cp.SampleOffset)
		buf.Write(rec[:])
	}
	return buf.Bytes()
}
