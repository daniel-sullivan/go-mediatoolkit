package nativemp3

// BitStream is minimp3's MSB-first bit reader over a byte payload
// (bs_t struct, minimp3.h:206).
//
//	typedef struct
//	{
//	    const uint8_t *buf;
//	    int pos, limit;
//	} bs_t;
//
// Buf is the backing byte slice, Pos is the read cursor in bits, and Limit
// is the total number of readable bits (bytes*8). Reads past Limit yield 0
// (see GetBits) but still advance Pos, so the caller can detect overrun via
// Pos > Limit, exactly as minimp3 does.
type BitStream struct {
	Buf   []byte
	Pos   int
	Limit int
}

// BsInit initializes the bit reader over the first bytes of data
// (bs_init, minimp3.h:241).
//
//	static void bs_init(bs_t *bs, const uint8_t *data, int bytes)
//	{
//	    bs->buf   = data;
//	    bs->pos   = 0;
//	    bs->limit = bytes*8;
//	}
func BsInit(bs *BitStream, data []byte, bytes int) {
	bs.Buf = data
	bs.Pos = 0
	bs.Limit = bytes * 8
}

// GetBits reads the next n bits MSB-first and returns them right-justified
// (get_bits, minimp3.h:248).
//
//	static uint32_t get_bits(bs_t *bs, int n)
//	{
//	    uint32_t next, cache = 0, s = bs->pos & 7;
//	    int shl = n + s;
//	    const uint8_t *p = bs->buf + (bs->pos >> 3);
//	    if ((bs->pos += n) > bs->limit)
//	        return 0;
//	    next = *p++ & (255 >> s);
//	    while ((shl -= 8) > 0)
//	    {
//	        cache |= next << shl;
//	        next = *p++;
//	    }
//	    return cache | (next >> -shl);
//	}
//
// The pointer p walks Buf starting at byte bs.Pos>>3; the loop variable shl
// is a signed int so the terminal "next >> -shl" is a right shift by
// (8 - (shl+8)) bits, matching the C exactly.
func GetBits(bs *BitStream, n int) uint32 {
	var next, cache uint32
	s := uint32(bs.Pos & 7)
	shl := n + int(s)
	p := bs.Pos >> 3
	bs.Pos += n
	if bs.Pos > bs.Limit {
		return 0
	}
	next = uint32(bs.Buf[p]) & (255 >> s)
	p++
	for {
		shl -= 8
		if shl <= 0 {
			break
		}
		cache |= next << uint(shl)
		next = uint32(bs.Buf[p])
		p++
	}
	return cache | (next >> uint(-shl))
}
