package nativemp3

// Bit reservoir reassembly — the main data buffer handling that lets a
// Layer III frame's main data span into bytes carried over from previous
// frames.

// minimp3Min returns the smaller of a and b (MINIMP3_MIN, minimp3.h:84).
//
//	#define MINIMP3_MIN(a, b) ((a) > (b) ? (b) : (a))
func minimp3Min(a, b int) int {
	if a > b {
		return b
	}
	return a
}

// minimp3Max returns the larger of a and b (MINIMP3_MAX, minimp3.h:85).
//
//	#define MINIMP3_MAX(a, b) ((a) < (b) ? (b) : (a))
func minimp3Max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// L3RestoreReservoir reassembles the main data for the current frame by
// prepending up to mainDataBegin bytes of carried-over reservoir to this
// frame's payload, then points the scratch bit reader at the result. It
// returns whether enough reservoir was available, i.e. h.Reserv >=
// mainDataBegin (L3_restore_reservoir, minimp3.h:1228).
//
//	static int L3_restore_reservoir(mp3dec_t *h, bs_t *bs, mp3dec_scratch_t *s, int main_data_begin)
//	{
//	    int frame_bytes = (bs->limit - bs->pos)/8;
//	    int bytes_have = MINIMP3_MIN(h->reserv, main_data_begin);
//	    memcpy(s->maindata, h->reserv_buf + MINIMP3_MAX(0, h->reserv - main_data_begin), MINIMP3_MIN(h->reserv, main_data_begin));
//	    memcpy(s->maindata + bytes_have, bs->buf + bs->pos/8, frame_bytes);
//	    bs_init(&s->bs, s->maindata, bytes_have + frame_bytes);
//	    return h->reserv >= main_data_begin;
//	}
func L3RestoreReservoir(h *Decoder, bs *BitStream, s *Scratch, mainDataBegin int) bool {
	frameBytes := (bs.Limit - bs.Pos) / 8
	bytesHave := minimp3Min(h.Reserv, mainDataBegin)
	copy(s.Maindata[:minimp3Min(h.Reserv, mainDataBegin)], h.ReservBuf[minimp3Max(0, h.Reserv-mainDataBegin):])
	copy(s.Maindata[bytesHave:bytesHave+frameBytes], bs.Buf[bs.Pos/8:])
	BsInit(&s.Bs, s.Maindata[:], bytesHave+frameBytes)
	return h.Reserv >= mainDataBegin
}

// L3SaveReservoir copies the unconsumed tail of the current frame's main
// data back into the decoder's reservoir for the next frame, clamped to
// MaxBitReservoirBytes (L3_save_reservoir, minimp3.h:1212).
//
//	static void L3_save_reservoir(mp3dec_t *h, mp3dec_scratch_t *s)
//	{
//	    int pos = (s->bs.pos + 7)/8u;
//	    int remains = s->bs.limit/8u - pos;
//	    if (remains > MAX_BITRESERVOIR_BYTES)
//	    {
//	        pos += remains - MAX_BITRESERVOIR_BYTES;
//	        remains = MAX_BITRESERVOIR_BYTES;
//	    }
//	    if (remains > 0)
//	    {
//	        memmove(h->reserv_buf, s->maindata + pos, remains);
//	    }
//	    h->reserv = remains;
//	}
func L3SaveReservoir(h *Decoder, s *Scratch) {
	pos := (s.Bs.Pos + 7) / 8
	remains := s.Bs.Limit/8 - pos
	if remains > MaxBitReservoirBytes {
		pos += remains - MaxBitReservoirBytes
		remains = MaxBitReservoirBytes
	}
	if remains > 0 {
		copy(h.ReservBuf[:remains], s.Maindata[pos:pos+remains])
	}
	h.Reserv = remains
}
