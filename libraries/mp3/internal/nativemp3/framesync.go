package nativemp3

// mp3dMatchFrame verifies that, starting at hdr, a run of consecutive
// frames all share hdr's format — minimp3's confidence check that a
// candidate sync is real (mp3d_match_frame, minimp3.h:1657).
//
//	static int mp3d_match_frame(const uint8_t *hdr, int mp3_bytes, int frame_bytes)
//	{
//	    int i, nmatch;
//	    for (i = 0, nmatch = 0; nmatch < MAX_FRAME_SYNC_MATCHES; nmatch++)
//	    {
//	        i += hdr_frame_bytes(hdr + i, frame_bytes) + hdr_padding(hdr + i);
//	        if (i + HDR_SIZE > mp3_bytes)
//	            return nmatch > 0;
//	        if (!hdr_compare(hdr, hdr + i))
//	            return 0;
//	    }
//	    return 1;
//	}
//
// hdr is the candidate header slice and mp3Bytes is the number of bytes
// available from hdr onward (the C caller passes mp3_bytes - i). The
// returned bool stands in for the C int (0 / nonzero).
func mp3dMatchFrame(hdr []byte, mp3Bytes, frameBytes int) bool {
	i := 0
	for nmatch := 0; nmatch < MaxFrameSyncMatches; nmatch++ {
		i += hdrFrameBytes(hdr[i:], frameBytes) + hdrPadding(hdr[i:])
		if i+HDRSize > mp3Bytes {
			return nmatch > 0
		}
		if !hdrCompare(hdr, hdr[i:]) {
			return false
		}
	}
	return true
}

// mp3dFindFrame scans mp3 for the first valid frame, resolving free-format
// frame sizes by probing forward, and reports the frame offset together
// with its byte length (including padding) via ptrFrameBytes
// (mp3d_find_frame, minimp3.h:1671).
//
//	static int mp3d_find_frame(const uint8_t *mp3, int mp3_bytes, int *free_format_bytes, int *ptr_frame_bytes)
//	{
//	    int i, k;
//	    for (i = 0; i < mp3_bytes - HDR_SIZE; i++, mp3++)
//	    {
//	        if (hdr_valid(mp3))
//	        {
//	            int frame_bytes = hdr_frame_bytes(mp3, *free_format_bytes);
//	            int frame_and_padding = frame_bytes + hdr_padding(mp3);
//
//	            for (k = HDR_SIZE; !frame_bytes && k < MAX_FREE_FORMAT_FRAME_SIZE && i + 2*k < mp3_bytes - HDR_SIZE; k++)
//	            {
//	                if (hdr_compare(mp3, mp3 + k))
//	                {
//	                    int fb = k - hdr_padding(mp3);
//	                    int nextfb = fb + hdr_padding(mp3 + k);
//	                    if (i + k + nextfb + HDR_SIZE > mp3_bytes || !hdr_compare(mp3, mp3 + k + nextfb))
//	                        continue;
//	                    frame_and_padding = k;
//	                    frame_bytes = fb;
//	                    *free_format_bytes = fb;
//	                }
//	            }
//	            if ((frame_bytes && i + frame_and_padding <= mp3_bytes &&
//	                mp3d_match_frame(mp3, mp3_bytes - i, frame_bytes)) ||
//	                (!i && frame_and_padding == mp3_bytes))
//	            {
//	                *ptr_frame_bytes = frame_and_padding;
//	                return i;
//	            }
//	            *free_format_bytes = 0;
//	        }
//	    }
//	    *ptr_frame_bytes = 0;
//	    return mp3_bytes;
//	}
//
// freeFormatBytes and ptrFrameBytes are the C int-by-reference out
// parameters. The C "mp3++" pointer walk is expressed here by reslicing
// mp3[i:] (so "mp3 + k" becomes "mp3[i+k:]").
func mp3dFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes, ptrFrameBytes *int) int {
	for i := 0; i < mp3Bytes-HDRSize; i++ {
		h := mp3[i:]
		if hdrValid(h) {
			frameBytes := hdrFrameBytes(h, *freeFormatBytes)
			frameAndPadding := frameBytes + hdrPadding(h)

			for k := HDRSize; frameBytes == 0 && k < MaxFreeFormatFrameSize && i+2*k < mp3Bytes-HDRSize; k++ {
				if hdrCompare(h, mp3[i+k:]) {
					fb := k - hdrPadding(h)
					nextfb := fb + hdrPadding(mp3[i+k:])
					if i+k+nextfb+HDRSize > mp3Bytes || !hdrCompare(h, mp3[i+k+nextfb:]) {
						continue
					}
					frameAndPadding = k
					frameBytes = fb
					*freeFormatBytes = fb
				}
			}
			if (frameBytes != 0 && i+frameAndPadding <= mp3Bytes &&
				mp3dMatchFrame(h, mp3Bytes-i, frameBytes)) ||
				(i == 0 && frameAndPadding == mp3Bytes) {
				*ptrFrameBytes = frameAndPadding
				return i
			}
			*freeFormatBytes = 0
		}
	}
	*ptrFrameBytes = 0
	return mp3Bytes
}
