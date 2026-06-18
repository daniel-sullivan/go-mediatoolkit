package nativemp3

// Layer III scalefactor dequantization — the "dequantize" slice that turns a
// granule's coded scalefactors into the per-band float gain table the Huffman
// stage multiplies onto each frequency line. This is a 1:1 translation of
// minimp3's L3_decode_scalefactors area (minimp3.h, the public-domain
// single-header decoder by lieff): L3_read_scalefactors (the raw scalefactor
// bit unpack), L3_ldexp_q2 (the quarter-step power-of-two gain table), and
// L3_decode_scalefactors itself (scalefactor-size decode, subblock-gain /
// preamp adjustment, and the iscf -> scf gain expansion).
//
// The ix -> xr conversion proper (g_pow43 / L3_pow_43 and the L3_huffman
// inner loop that applies these scf[] gains) lives in huffman.go. This file
// produces the scf[] input it consumes.
//
// # Strict mode
//
// L3_read_scalefactors and the integer body of L3_decode_scalefactors are
// pure-integer and bit-identical regardless of build tag. The only
// floating-point work is L3_ldexp_q2's float32 multiplies; those route
// through the package f32mul helper (huffman_fp_strict.go / huffman_fp_default.go)
// so the mp3_strict build keeps the same separately-rounded float32 operation
// order as the cgo oracle, while the default build may fuse/SIMD.

// Dequantizer step / scalefactor range constants (minimp3.h:80).
//
//	#define BITS_DEQUANTIZER_OUT  -1
//	#define MAX_SCF               (255 + BITS_DEQUANTIZER_OUT*4 - 210)
//	#define MAX_SCFI              ((MAX_SCF + 3) & ~3)
//
// BitsDequantizerOut biases the global-gain exponent; MaxSCF / MaxSCFI bound
// the quarter-step exponent fed to L3_ldexp_q2 so the gain stays representable.
const (
	BitsDequantizerOut = -1
	MaxSCF             = 255 + BitsDequantizerOut*4 - 210
	MaxSCFI            = (MaxSCF + 3) & ^3
)

// hdrTestIStereo returns the (non-boolean) intensity-stereo mode bit
// (HDR_TEST_I_STEREO, minimp3.h:69).
//
//	#define HDR_TEST_I_STEREO(h) ((h[3]) & 0x10)
func hdrTestIStereo(h []byte) int { return int(h[3] & 0x10) }

// hdrIsMsStereo reports whether the header selects MS (mid/side) stereo
// (HDR_IS_MS_STEREO, minimp3.h:63).
//
//	#define HDR_IS_MS_STEREO(h) (((h[3]) & 0xE0) == 0x60)
func hdrIsMsStereo(h []byte) bool { return (h[3] & 0xE0) == 0x60 }

// L3ReadScalefactors unpacks the four scalefactor partitions for one granule
// from the bit reader into scf, mirroring minimp3's L3_read_scalefactors
// (minimp3.h:602).
//
//	static void L3_read_scalefactors(uint8_t *scf, uint8_t *ist_pos, const uint8_t *scf_size, const uint8_t *scf_count, bs_t *bitbuf, int scfsi)
//	{
//	    int i, k;
//	    for (i = 0; i < 4 && scf_count[i]; i++, scfsi *= 2)
//	    {
//	        int cnt = scf_count[i];
//	        if (scfsi & 8)
//	        {
//	            memcpy(scf, ist_pos, cnt);
//	        } else
//	        {
//	            int bits = scf_size[i];
//	            if (!bits)
//	            {
//	                memset(scf, 0, cnt);
//	                memset(ist_pos, 0, cnt);
//	            } else
//	            {
//	                int max_scf = (scfsi < 0) ? (1 << bits) - 1 : -1;
//	                for (k = 0; k < cnt; k++)
//	                {
//	                    int s = get_bits(bitbuf, bits);
//	                    ist_pos[k] = (s == max_scf ? -1 : s);
//	                    scf[k] = s;
//	                }
//	            }
//	        }
//	        ist_pos += cnt;
//	        scf += cnt;
//	    }
//	    scf[0] = scf[1] = scf[2] = 0;
//	}
//
// scf and istPos are walked by an advancing offset (scfOff) in place of the C
// pointer post-increments; the trailing scf[0]=scf[1]=scf[2]=0 writes the
// three guard entries at the final offset. The "-1" sentinel stored into the
// uint8 istPos wraps to 255 via the byte conversion, matching the C exactly.
func L3ReadScalefactors(scf, istPos, scfSize, scfCount []uint8, bitbuf *BitStream, scfsi int) {
	scfOff := 0
	for i := 0; i < 4 && scfCount[i] != 0; i, scfsi = i+1, scfsi*2 {
		cnt := int(scfCount[i])
		if scfsi&8 != 0 {
			copy(scf[scfOff:scfOff+cnt], istPos[scfOff:scfOff+cnt])
		} else {
			bits := int(scfSize[i])
			if bits == 0 {
				for k := 0; k < cnt; k++ {
					scf[scfOff+k] = 0
					istPos[scfOff+k] = 0
				}
			} else {
				maxScf := -1
				if scfsi < 0 {
					maxScf = (1 << uint(bits)) - 1
				}
				for k := 0; k < cnt; k++ {
					s := int(GetBits(bitbuf, bits))
					if s == maxScf {
						// C stores -1 into a uint8_t, which wraps to 0xFF.
						istPos[scfOff+k] = 0xFF
					} else {
						istPos[scfOff+k] = uint8(s)
					}
					scf[scfOff+k] = uint8(s)
				}
			}
		}
		scfOff += cnt
	}
	scf[scfOff+0] = 0
	scf[scfOff+1] = 0
	scf[scfOff+2] = 0
}

// gExpfrac is the static quarter-step mantissa table from L3_ldexp_q2
// (minimp3.h:637): g_expfrac[e&3] = 2^(-(e&3)/4) scaled into the 1<<30 range.
var gExpfrac = [4]float32{9.31322575e-10, 7.83145814e-10, 6.58544508e-10, 5.53767716e-10}

// L3Ldexp returns y scaled by 2^(-expQ2/4), accumulating the quarter-step
// exponent in chunks so the shift stays in range, mirroring minimp3's
// L3_ldexp_q2 (minimp3.h:635).
//
//	static float L3_ldexp_q2(float y, int exp_q2)
//	{
//	    static const float g_expfrac[4] = { ... };
//	    int e;
//	    do
//	    {
//	        e = MINIMP3_MIN(30*4, exp_q2);
//	        y *= g_expfrac[e & 3]*(1 << 30 >> (e >> 2));
//	    } while ((exp_q2 -= e) > 0);
//	    return y;
//	}
//
// The two float32 multiplies (the g_expfrac lookup and the running y scale)
// route through f32mul so the strict build does not fuse them; the
// (1<<30)>>(e>>2) factor is an exact integer power of two converted to
// float32. The do/while is rendered as an unconditional first pass followed
// by the (exp_q2 -= e) > 0 test, matching the C loop exactly.
//
// Negative-shift caveat: the first call from L3_decode_scalefactors passes
// exp_q2 = MAX_SCFI - gain_exp, which is reachably negative (global_gain up to
// 255 gives gain_exp up to 44 > MAX_SCFI = 40), so e>>2 reaches -1 / -2 and
// the C does `1 << 30 >> (e >> 2)` — a shift by a negative count, which C
// leaves undefined. The cgo parity oracle compiles minimp3 with clang on the
// same arm64 host; clang lowers this to a variable 32-bit right shift (LSRV)
// whose count is taken mod 32, so e>>2 == -1 -> shift 31 -> 0 and
// e>>2 == -2 -> shift 30 -> 1. Go's `>>` instead yields 0 for any count >= 32,
// which would diverge for the e>>2 == -2 case. To stay bit-exact with the
// oracle this reproduces the hardware mod-32 masking explicitly (& 31).
func L3Ldexp(y float32, expQ2 int) float32 {
	for {
		e := minimp3Min(30*4, expQ2)
		factor := f32mul(gExpfrac[e&3], float32(int32(1)<<30>>(uint(e>>2)&31)))
		y = f32mul(y, factor)
		expQ2 -= e
		if expQ2 <= 0 {
			break
		}
	}
	return y
}

// gScfPartitions is g_scf_partitions[3][28] from L3_decode_scalefactors
// (minimp3.h:649): the per-block-shape scalefactor partition (band-count)
// tables selected by !!n_short_sfb + !n_long_sfb.
var gScfPartitions = [3][28]uint8{
	{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0, 0, 7, 7, 7, 0, 6, 6, 6, 3, 8, 8, 5, 0},
	{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0, 0, 6, 15, 12, 0, 6, 12, 9, 6, 6, 18, 9, 0},
	{9, 9, 6, 12, 9, 9, 9, 9, 9, 9, 12, 6, 18, 18, 0, 0, 12, 12, 12, 0, 12, 9, 9, 6, 15, 12, 9, 0},
}

// gScfcDecode is g_scfc_decode[16] from the MPEG-1 branch of
// L3_decode_scalefactors (minimp3.h:661): the scalefac_compress -> packed
// scalefactor-size lookup.
var gScfcDecode = [16]uint8{0, 1, 2, 3, 12, 5, 6, 7, 9, 10, 11, 13, 14, 15, 18, 19}

// gMod is g_mod[6*4] from the MPEG-2/2.5 branch of L3_decode_scalefactors
// (minimp3.h:667): the mixed-radix divisors used to unpack scalefactor sizes.
var gMod = [6 * 4]uint8{5, 5, 4, 4, 5, 5, 4, 1, 4, 3, 1, 1, 5, 6, 6, 1, 4, 4, 4, 1, 4, 3, 1, 1}

// gPreamp is g_preamp[10] from L3_decode_scalefactors (minimp3.h:694): the
// high-frequency preamplification added when gr->preflag is set.
var gPreamp = [10]uint8{1, 1, 1, 1, 2, 2, 3, 3, 3, 2}

// L3DecodeScalefactors decodes one granule's scalefactors and expands them
// into the per-band float gain table scf, mirroring minimp3's
// L3_decode_scalefactors (minimp3.h:647).
//
//	static void L3_decode_scalefactors(const uint8_t *hdr, uint8_t *ist_pos, bs_t *bs, const L3_gr_info_t *gr, float *scf, int ch)
//
// hdr is the 4-byte frame header (for the MPEG-1 / I-stereo / MS-stereo
// tests); istPos is this channel's intensity-stereo position scratch
// (s->ist_pos[ch], 39 bytes); gr is the granule side-info; scf receives the
// n_long_sfb + n_short_sfb float gains; ch is the channel index. The C
// pointer "scf_partition += k" is rendered by re-slicing the selected
// partition row, and the inner uint8 iscf arithmetic (subblock-gain shifts,
// preamp, additions) wraps mod 256 exactly as the C uint8_t array does.
func L3DecodeScalefactors(hdr []byte, istPos []uint8, bs *BitStream, gr *L3GrInfo, scf []float32, ch int) {
	// scf_partition = g_scf_partitions[!!gr->n_short_sfb + !gr->n_long_sfb]
	partIdx := 0
	if gr.NShortSfb != 0 {
		partIdx++
	}
	if gr.NLongSfb == 0 {
		partIdx++
	}
	scfPartition := gScfPartitions[partIdx][:]

	var scfSize [4]uint8
	var iscf [40]uint8
	scfShift := int(gr.ScalefacScale) + 1
	scfsi := int(gr.Scfsi)

	if hdrTestMPEG1(hdr) != 0 {
		part := int(gScfcDecode[gr.ScalefacCompress])
		scfSize[1] = uint8(part >> 2)
		scfSize[0] = scfSize[1]
		scfSize[3] = uint8(part & 3)
		scfSize[2] = scfSize[3]
	} else {
		ist := 0
		if hdrTestIStereo(hdr) != 0 && ch != 0 {
			ist = 1
		}
		sfc := int(gr.ScalefacCompress) >> uint(ist)
		k := ist * 3 * 4
		var modprod int
		for ; sfc >= 0; sfc, k = sfc-modprod, k+4 {
			modprod = 1
			for i := 3; i >= 0; i-- {
				scfSize[i] = uint8(sfc / modprod % int(gMod[k+i]))
				modprod *= int(gMod[k+i])
			}
		}
		scfPartition = scfPartition[k:]
		scfsi = -16
	}
	L3ReadScalefactors(iscf[:], istPos, scfSize[:], scfPartition, bs, scfsi)

	if gr.NShortSfb != 0 {
		sh := 3 - scfShift
		for i := 0; i < int(gr.NShortSfb); i += 3 {
			iscf[int(gr.NLongSfb)+i+0] += gr.SubblockGain[0] << uint(sh)
			iscf[int(gr.NLongSfb)+i+1] += gr.SubblockGain[1] << uint(sh)
			iscf[int(gr.NLongSfb)+i+2] += gr.SubblockGain[2] << uint(sh)
		}
	} else if gr.Preflag != 0 {
		for i := 0; i < 10; i++ {
			iscf[11+i] += gPreamp[i]
		}
	}

	gainExp := int(gr.GlobalGain) + BitsDequantizerOut*4 - 210
	if hdrIsMsStereo(hdr) {
		gainExp -= 2
	}
	gain := L3Ldexp(float32(int32(1)<<(MaxSCFI/4)), MaxSCFI-gainExp)
	for i := 0; i < int(gr.NLongSfb)+int(gr.NShortSfb); i++ {
		scf[i] = L3Ldexp(gain, int(iscf[i])<<uint(scfShift))
	}
}
