// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// bits<->pe conversion and the bits2PeFactor lookup of the AAC encoder
// threshold-adjustment DRIVER tier, ported 1:1 from the vendored FDK-AAC
// reference libAACenc/src/adj_thr.cpp. FDKaacEnc_bits2pe2 converts a granted
// dynamic-bit budget to a perceptual-entropy target; FDKaacEnc_InitBits2PeFactor
// retrieves the per-(sampleRate, bitrate, nChannels, afterburner) bits2PE factor
// (mantissa/exponent) from the bits2PeConfigTab ROM, linearly interpolating across
// the bitrate axis.
//
// CBR/AAC-LC path only. The advancedBitsToPe branch (AAC-(E)LD) is carried for 1:1
// fidelity but is only reached when isLowDelay != 0, which AAC-LC never sets.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8 on the aarch64 target; fDivNorm/fMin are the
// already-verified leaf kernels.

// Q-format definitions (adj_thr.cpp:310-312).
const (
	qBitFac  = 24 // Q_BITFAC: Q scaling used in FDKaacEnc_bitresCalcBitFac()
	qAvgBits = 17 // Q_AVGBITS: scale bit values
)

// afterburnerStati / maxAllowedElChannels (adj_thr.cpp:142-143).
const (
	afterburnerStati     = 2 // AFTERBURNER_STATI
	maxAllowedElChannels = 2 // MAX_ALLOWED_EL_CHANNELS
)

// bitPeSfac is the 1:1 port of BIT_PE_SFAC (adj_thr.cpp:145-148): one bitrate row
// with the bits2PeFactor matrix [afterburner off/on][nCh=1/2].
type bitPeSfac struct {
	bitrate       int
	bits2PeFactor [afterburnerStati][maxAllowedElChannels]int32 // bits2PeFactor[2][2] (FIXP_DBL)
}

// bits2peCfgTab is the 1:1 port of BITS2PE_CFG_TAB (adj_thr.cpp:150-155): one
// sample-rate's bits2PE factor table.
type bits2peCfgTab struct {
	sampleRate int
	pPeTab     []bitPeSfac
	nEntries   int
}

// fl2b2pe is the 1:1 port of #define FL2B2PE(value) FL2FXCONST_DBL((value)/(1<<2))
// (adj_thr.cpp:157): the table literals are pre-scaled by 1/4 (exponent 2).
func fl2b2pe(value float64) int32 { return fl2fxconstDBL(value / 4.0) }

// sBits2PeTab16000 is the 1:1 port of S_Bits2PeTab16000 (adj_thr.cpp:159-177).
var sBits2PeTab16000 = []bitPeSfac{
	{10000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(0.00)}, {fl2b2pe(1.40), fl2b2pe(0.00)}}},
	{24000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.40)}, {fl2b2pe(1.60), fl2b2pe(1.20)}}},
	{32000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{48000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.80)}, {fl2b2pe(1.60), fl2b2pe(1.60)}}},
	{64000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.60)}, {fl2b2pe(1.20), fl2b2pe(1.60)}}},
	{96000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.80)}, {fl2b2pe(1.40), fl2b2pe(1.60)}}},
	{128000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.80)}, {fl2b2pe(1.40), fl2b2pe(1.80)}}},
	{148000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.80)}, {fl2b2pe(1.40), fl2b2pe(1.40)}}},
}

// sBits2PeTab22050 is the 1:1 port of S_Bits2PeTab22050 (adj_thr.cpp:179-197).
var sBits2PeTab22050 = []bitPeSfac{
	{16000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.40)}, {fl2b2pe(1.20), fl2b2pe(0.80)}}},
	{24000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.40)}, {fl2b2pe(1.40), fl2b2pe(1.00)}}},
	{32000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.40)}, {fl2b2pe(1.40), fl2b2pe(1.20)}}},
	{48000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.60)}, {fl2b2pe(1.20), fl2b2pe(1.40)}}},
	{64000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{96000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.60)}}},
	{128000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.80)}, {fl2b2pe(1.60), fl2b2pe(1.60)}}},
	{148000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.80)}, {fl2b2pe(1.40), fl2b2pe(1.60)}}},
}

// sBits2PeTab24000 is the 1:1 port of S_Bits2PeTab24000 (adj_thr.cpp:199-217).
var sBits2PeTab24000 = []bitPeSfac{
	{16000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.40)}, {fl2b2pe(1.20), fl2b2pe(0.80)}}},
	{24000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.20)}, {fl2b2pe(1.40), fl2b2pe(1.00)}}},
	{32000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.20)}, {fl2b2pe(1.40), fl2b2pe(0.80)}}},
	{48000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.60)}, {fl2b2pe(1.40), fl2b2pe(1.40)}}},
	{64000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{96000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.60)}}},
	{128000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.80)}}},
	{148000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.60)}, {fl2b2pe(1.40), fl2b2pe(1.80)}}},
}

// sBits2PeTab32000 is the 1:1 port of S_Bits2PeTab32000 (adj_thr.cpp:219-243).
var sBits2PeTab32000 = []bitPeSfac{
	{16000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.40)}, {fl2b2pe(0.80), fl2b2pe(0.80)}}},
	{24000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.20)}, {fl2b2pe(1.00), fl2b2pe(0.60)}}},
	{32000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.20)}, {fl2b2pe(1.00), fl2b2pe(0.80)}}},
	{48000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.40)}, {fl2b2pe(1.20), fl2b2pe(1.20)}}},
	{64000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.40)}, {fl2b2pe(1.60), fl2b2pe(1.20)}}},
	{96000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.40)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{128000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.60)}}},
	{148000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.60)}}},
	{160000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.80), fl2b2pe(1.60)}}},
	{200000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.60)}, {fl2b2pe(1.40), fl2b2pe(1.60)}}},
	{320000, [2][2]int32{{fl2b2pe(3.20), fl2b2pe(1.80)}, {fl2b2pe(3.20), fl2b2pe(1.80)}}},
}

// sBits2PeTab44100 is the 1:1 port of S_Bits2PeTab44100 (adj_thr.cpp:245-269).
var sBits2PeTab44100 = []bitPeSfac{
	{16000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.60)}, {fl2b2pe(0.80), fl2b2pe(1.00)}}},
	{24000, [2][2]int32{{fl2b2pe(1.00), fl2b2pe(1.20)}, {fl2b2pe(1.00), fl2b2pe(0.80)}}},
	{32000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.20)}, {fl2b2pe(0.80), fl2b2pe(0.60)}}},
	{48000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.20)}, {fl2b2pe(1.20), fl2b2pe(0.80)}}},
	{64000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.20)}, {fl2b2pe(1.20), fl2b2pe(1.00)}}},
	{96000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.20)}, {fl2b2pe(1.60), fl2b2pe(1.20)}}},
	{128000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{148000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.60)}}},
	{160000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.60)}}},
	{200000, [2][2]int32{{fl2b2pe(1.80), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.60)}}},
	{320000, [2][2]int32{{fl2b2pe(3.20), fl2b2pe(1.60)}, {fl2b2pe(3.20), fl2b2pe(1.60)}}},
}

// sBits2PeTab48000 is the 1:1 port of S_Bits2PeTab48000 (adj_thr.cpp:271-295).
var sBits2PeTab48000 = []bitPeSfac{
	{16000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(0.00)}, {fl2b2pe(0.80), fl2b2pe(0.00)}}},
	{24000, [2][2]int32{{fl2b2pe(1.40), fl2b2pe(1.20)}, {fl2b2pe(1.00), fl2b2pe(0.80)}}},
	{32000, [2][2]int32{{fl2b2pe(1.00), fl2b2pe(1.20)}, {fl2b2pe(0.60), fl2b2pe(0.80)}}},
	{48000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.00)}, {fl2b2pe(0.80), fl2b2pe(0.80)}}},
	{64000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.20)}, {fl2b2pe(1.20), fl2b2pe(1.00)}}},
	{96000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.40)}, {fl2b2pe(1.60), fl2b2pe(1.20)}}},
	{128000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{148000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{160000, [2][2]int32{{fl2b2pe(1.60), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{200000, [2][2]int32{{fl2b2pe(1.20), fl2b2pe(1.60)}, {fl2b2pe(1.60), fl2b2pe(1.40)}}},
	{320000, [2][2]int32{{fl2b2pe(3.20), fl2b2pe(1.60)}, {fl2b2pe(3.20), fl2b2pe(1.60)}}},
}

// bits2PeConfigTab is the 1:1 port of bits2PeConfigTab (adj_thr.cpp:297-304).
var bits2PeConfigTab = []bits2peCfgTab{
	{16000, sBits2PeTab16000, len(sBits2PeTab16000)},
	{22050, sBits2PeTab22050, len(sBits2PeTab22050)},
	{24000, sBits2PeTab24000, len(sBits2PeTab24000)},
	{32000, sBits2PeTab32000, len(sBits2PeTab32000)},
	{44100, sBits2PeTab44100, len(sBits2PeTab44100)},
	{48000, sBits2PeTab48000, len(sBits2PeTab48000)},
}

// bits2pe2 is the 1:1 port of FDKaacEnc_bits2pe2 (adj_thr.cpp:433-437):
// (INT)(fMult(factor_m, bits<<Q_AVGBITS) >> (Q_AVGBITS - factor_e)).
func bits2pe2(bits int, factorM int32, factorE int) int {
	return int(fMult(factorM, int32(bits)<<qAvgBits) >> uint(qAvgBits-factorE))
}

// initBits2PeFactor is the 1:1 port of FDKaacEnc_InitBits2PeFactor
// (adj_thr.cpp:318-427): retrieve bits2PeFactor (mantissa/exponent) from the
// table, interpolating across the bitrate axis, then apply the dead-zone
// quantiser correction. Returns (bits2PeFactor_m, bits2PeFactor_e).
func initBits2PeFactor(bitRate, nChannels, sampleRate, advancedBitsToPe,
	dZoneQuantEnable, invQuant int) (bits2PeFactorM int32, bits2PeFactorE int) {
	// 1) Set default bits2pe factor.
	// C: FL2FXCONST_DBL(1.18f / (1 << (1))). The 1.18f literal is a 32-bit
	// float, so the constant is float32(1.18)/2 rounded to single precision
	// BEFORE the Q31 scale — fl2fxconstDBLf applies that float32 rounding.
	// Using the float64 fl2fxconstDBL here would round 1.18 in double precision
	// and land one ULP off the genuine fixed-point constant (1267015352 vs
	// the correct 1267015296).
	bit2PEm := fl2fxconstDBLf(1.18 / (1 << 1))
	bit2PEe := 1

	// 2) For AAC-(E)LD, make use of advanced bits to pe factor table
	if advancedBitsToPe != 0 && nChannels <= 2 {
		var peTab []bitPeSfac
		size := 0

		// 2.1) Get correct table entry
		for i := 0; i < len(bits2PeConfigTab); i++ {
			if sampleRate >= bits2PeConfigTab[i].sampleRate {
				peTab = bits2PeConfigTab[i].pPeTab
				size = bits2PeConfigTab[i].nEntries
			}
		}

		if peTab != nil && size != 0 {
			startB := -1
			stopB := -1
			var startPF int32 = fl2fxconstDBL(0.0)
			var stopPF int32 = fl2fxconstDBL(0.0)
			qualityIdx := 0
			if invQuant != 0 {
				qualityIdx = 1
			}

			if bitRate >= peTab[size-1].bitrate {
				startB = peTab[size-1].bitrate
				stopB = bitRate + 1
				startPF = peTab[size-1].bits2PeFactor[qualityIdx][nChannels-1]
				stopPF = peTab[size-1].bits2PeFactor[qualityIdx][nChannels-1]
			} else {
				for i := 0; i < size-1; i++ {
					if peTab[i].bitrate <= bitRate && peTab[i+1].bitrate > bitRate {
						startB = peTab[i].bitrate
						stopB = peTab[i+1].bitrate
						startPF = peTab[i].bits2PeFactor[qualityIdx][nChannels-1]
						stopPF = peTab[i+1].bits2PeFactor[qualityIdx][nChannels-1]
						break
					}
				}
			}

			// 2.2) Configuration available?
			if startB != -1 {
				// 2.2.1) linear interpolate to actual PEfactor
				var bit2PE int32 = 0
				maxBit2PE := fl2fxconstDBL(3.0 / 4.0)

				slope, _ := fDivNorm(int32(bitRate-startB), int32(stopB-startB))
				bit2PE = fMult(slope, stopPF-startPF) + startPF
				bit2PE = fMin(maxBit2PE, bit2PE)

				// 2.2.2) sanity check if bits2pe value is high enough
				if bit2PE >= (fl2fxconstDBL(0.35) >> 2) {
					bit2PEm = bit2PE
					bit2PEe = 2 // table is fixed scaled
				}
			}
		}
	}

	if dZoneQuantEnable != 0 {
		if bit2PEm >= (fl2fxconstDBL(0.6) >> uint(bit2PEe)) {
			// Additional headroom for addition
			bit2PEm >>= 1
			bit2PEe += 1
		}

		// the quantTendencyCompensator compensates a lower bit consumption
		if (bitRate/nChannels > 32000) && (bitRate/nChannels <= 40000) {
			bit2PEm += fl2fxconstDBL(0.4) >> uint(bit2PEe)
		} else if bitRate/nChannels > 20000 {
			bit2PEm += fl2fxconstDBL(0.3) >> uint(bit2PEe)
		} else if bitRate/nChannels >= 16000 {
			bit2PEm += fl2fxconstDBL(0.3) >> uint(bit2PEe)
		} else {
			bit2PEm += fl2fxconstDBL(0.0) >> uint(bit2PEe)
		}
	}

	// 3) Return bits2pe factor
	return bit2PEm, bit2PEe
}
