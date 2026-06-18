// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// HFgen_preFlat.cpp pre-flattening ROM: the backsubst_data table bsd[28] and the
// getLog2[32] helper (HFgen_preFlat.cpp:117-542), 1:1. bsd[numBands-5] holds the
// pre-computed normalized-Cholesky factors for the degree-3 polynomial fit of the
// low-band envelope. The FIXP_CHB (== FIXP_SGL) coefficients are CHC(0x........) ==
// FX_DBL2FXCONST_SGL(a) narrowings of Q1.31 constants; materialised once at init
// through nativeaac.StcNarrow (the same FX_DBL2FXCONST_SGL macro), so the Go ROM is
// byte-identical to the genuine C bsd[]. The SCHAR _sf arrays are plain int8.

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// getLog2 is getLog2[32] (HFgen_preFlat.cpp:135): trunc(log2(n))+1 for n in 0..31
// (with getLog2[0]==0).
var getLog2 = [32]uint8{
	0, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
}

// bsdIdxOffset is BSD_IDX_OFFSET (HFgen_preFlat.cpp:143): bsd[] starts at numBands=5.
const bsdIdxOffset = 5

// nNumBands is N_NUMBANDS (HFgen_preFlat.cpp:145): bsd[] element count (28).
const nNumBands = 32 - bsdIdxOffset + 1

// backsubstData is backsubst_data (HFgen_preFlat.cpp:117-132): the per-numBands
// normalized-Cholesky factor block. FIXP_CHB == FIXP_SGL (int16); _sf are SCHAR.
type backsubstData struct {
	Lnorm1d      [3]int16 // Lnorm1d: normalized L matrix
	Lnorm1dSf    [3]int8  // Lnorm1d_sf
	Lnormii      [3]int16 // Lnormii: diagonal L[i][i]
	LnormiiSf    [3]int8  // Lnormii_sf
	Bmul0        [4]int16 // Bmul0: pre-backsubst normalization of b
	Bmul0Sf      [4]int8  // Bmul0_sf
	LnormInv1d   [6]int16 // LnormInv1d: normalized inverted L (L')
	LnormInv1dSf [6]int8  // LnormInv1d_sf
	Bmul1        [4]int16 // Bmul1: post-backsubst normalization of b
	Bmul1Sf      [4]int8  // Bmul1_sf
}

// bsdRaw holds the Q1.31 hex constants (CHC arg) + SCHAR exponents straight from the
// C bsd[] initializer; bsd[] below narrows the CHC columns via StcNarrow at init.
var bsdRaw = [nNumBands]struct {
	Lnorm1d      [3]uint32
	Lnorm1dSf    [3]int8
	Lnormii      [3]uint32
	LnormiiSf    [3]int8
	Bmul0        [4]uint32
	Bmul0Sf      [4]int8
	LnormInv1d   [6]uint32
	LnormInv1dSf [6]int8
	Bmul1        [4]uint32
	Bmul1Sf      [4]int8
}{
	{ // numBands=5
		Lnorm1d: [3]uint32{0x66c85a52, 0x4278e587, 0x697dcaff}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x66a61789, 0x5253b8e3, 0x5addad81}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x7525ee90, 0x6e2a1210, 0x6523bb40, 0x59822ead}, Bmul0Sf: [4]int8{-6, -4, -2, 0},
		LnormInv1d: [6]uint32{0x609e4cad, 0x59c7e312, 0x681eecac, 0x440ea893, 0x4a214bb3, 0x53c345a1}, LnormInv1dSf: [6]int8{1, 0, -1, -1, -3, -5},
		Bmul1: [4]uint32{0x7525ee90, 0x58587936, 0x410d0b38, 0x7f1519d6}, Bmul1Sf: [4]int8{-6, -1, 2, 0},
	},
	{ // numBands=6
		Lnorm1d: [3]uint32{0x68943285, 0x4841d2c3, 0x6a6214c7}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x63c5923e, 0x4e906e18, 0x6285af8a}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x7263940b, 0x424a69a5, 0x4ae8383a, 0x517b7730}, Bmul0Sf: [4]int8{-7, -4, -2, 0},
		LnormInv1d: [6]uint32{0x518aee5f, 0x4823a096, 0x43764a39, 0x6e6faf23, 0x61bba44f, 0x59d8b132}, LnormInv1dSf: [6]int8{1, 0, -1, -2, -4, -6},
		Bmul1: [4]uint32{0x7263940b, 0x6757bff2, 0x5bf40fe0, 0x7d6f4292}, Bmul1Sf: [4]int8{-7, -2, 1, 0},
	},
	{ // numBands=7
		Lnorm1d: [3]uint32{0x699b4c3c, 0x4b8b702f, 0x6ae51a4f}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x623a7f49, 0x4ccc91fc, 0x68f048dd}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x7e6ebe18, 0x5701daf2, 0x74a8198b, 0x4b399aa1}, Bmul0Sf: [4]int8{-8, -5, -3, 0},
		LnormInv1d: [6]uint32{0x464a64a6, 0x78e42633, 0x5ee174ba, 0x5d0008c8, 0x455cff0f, 0x6b9100e7}, LnormInv1dSf: [6]int8{1, -1, -2, -2, -4, -7},
		Bmul1: [4]uint32{0x7e6ebe18, 0x42c52efe, 0x45fe401f, 0x7b5808ef}, Bmul1Sf: [4]int8{-8, -2, 1, 0},
	},
	{ // numBands=8
		Lnorm1d: [3]uint32{0x6a3fd9b4, 0x4d99823f, 0x6b372a94}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x614c6ef7, 0x4bd06699, 0x6e59cfca}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x4c389cc5, 0x79686681, 0x5e2544c2, 0x46305b43}, Bmul0Sf: [4]int8{-8, -6, -3, 0},
		LnormInv1d: [6]uint32{0x7b4ca7c6, 0x68270ac5, 0x467c644c, 0x505c1b0f, 0x67a14778, 0x45801767}, LnormInv1dSf: [6]int8{0, -1, -2, -2, -5, -7},
		Bmul1: [4]uint32{0x4c389cc5, 0x5c499ceb, 0x6f863c9f, 0x79059bfc}, Bmul1Sf: [4]int8{-8, -3, 0, 0},
	},
	{ // numBands=9
		Lnorm1d: [3]uint32{0x6aad9988, 0x4ef8ac18, 0x6b6df116}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x60b159b0, 0x4b33f772, 0x72f5573d}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x6206cb18, 0x58a7d8dc, 0x4e0b2d0b, 0x4207ad84}, Bmul0Sf: [4]int8{-9, -6, -3, 0},
		LnormInv1d: [6]uint32{0x6dadadae, 0x5b8b2cfc, 0x6cf61db2, 0x46c3c90b, 0x506314ea, 0x5f034acd}, LnormInv1dSf: [6]int8{0, -1, -3, -2, -5, -8},
		Bmul1: [4]uint32{0x6206cb18, 0x42f8b8de, 0x5bb4776f, 0x769acc79}, Bmul1Sf: [4]int8{-9, -3, 0, 0},
	},
	{ // numBands=10
		Lnorm1d: [3]uint32{0x6afa7252, 0x4feed3ed, 0x6b94504d}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x60467899, 0x4acbafba, 0x76eb327f}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x42415b15, 0x431080da, 0x420f1c32, 0x7d0c1aeb}, Bmul0Sf: [4]int8{-9, -6, -3, -1},
		LnormInv1d: [6]uint32{0x62b2c7a4, 0x51b040a6, 0x56caddb4, 0x7e74a2c8, 0x4030adf5, 0x43d1dc4f}, LnormInv1dSf: [6]int8{0, -1, -3, -3, -5, -8},
		Bmul1: [4]uint32{0x42415b15, 0x64e299b3, 0x4d33b5e8, 0x742cee5f}, Bmul1Sf: [4]int8{-9, -4, 0, 0},
	},
	{ // numBands=11
		Lnorm1d: [3]uint32{0x6b3258bb, 0x50a21233, 0x6bb03c19}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ff997c6, 0x4a82706e, 0x7a5aae36}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x5d2fb4fb, 0x685bddd8, 0x71b5e983, 0x7708c90b}, Bmul0Sf: [4]int8{-10, -7, -4, -1},
		LnormInv1d: [6]uint32{0x59aceea2, 0x49c428a0, 0x46ca5527, 0x724be884, 0x68e586da, 0x643485b6}, LnormInv1dSf: [6]int8{0, -1, -3, -3, -6, -9},
		Bmul1: [4]uint32{0x5d2fb4fb, 0x4e3fad1a, 0x42310ba2, 0x71c8b3ce}, Bmul1Sf: [4]int8{-10, -4, 0, 0},
	},
	{ // numBands=12
		Lnorm1d: [3]uint32{0x6b5c4726, 0x5128a4a8, 0x6bc52ee1}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5fc06618, 0x4a4ce559, 0x7d5c16e9}, LnormiiSf: [3]int8{3, 4, 1},
		Bmul0: [4]uint32{0x43af8342, 0x531533d3, 0x633660a6, 0x71ce6052}, Bmul0Sf: [4]int8{-10, -7, -4, -1},
		LnormInv1d: [6]uint32{0x522373d7, 0x434150cb, 0x75b58afc, 0x68474f2d, 0x575348a5, 0x4c20973f}, LnormInv1dSf: [6]int8{0, -1, -4, -3, -6, -9},
		Bmul1: [4]uint32{0x43af8342, 0x7c4d3d11, 0x732e13db, 0x6f756ac4}, Bmul1Sf: [4]int8{-10, -5, -1, 0},
	},
	{ // numBands=13
		Lnorm1d: [3]uint32{0x6b7c8953, 0x51903fcd, 0x6bd54d2e}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f94abf0, 0x4a2480fa, 0x40013553}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x6501236e, 0x436b9c4e, 0x578d7881, 0x6d34f92e}, Bmul0Sf: [4]int8{-11, -7, -4, -1},
		LnormInv1d: [6]uint32{0x4bc0e2b2, 0x7b9d12ac, 0x636c1c1b, 0x5fe15c2b, 0x49d54879, 0x7662cfa5}, LnormInv1dSf: [6]int8{0, -2, -4, -3, -6, -10},
		Bmul1: [4]uint32{0x6501236e, 0x64b059fe, 0x656d8359, 0x6d370900}, Bmul1Sf: [4]int8{-11, -5, -1, 0},
	},
	{ // numBands=14
		Lnorm1d: [3]uint32{0x6b95e276, 0x51e1b637, 0x6be1f7ed}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f727a1c, 0x4a053e9c, 0x412e528c}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x4d178bd4, 0x6f33b4e8, 0x4e028f7f, 0x691ee104}, Bmul0Sf: [4]int8{-11, -8, -4, -1},
		LnormInv1d: [6]uint32{0x46473d3f, 0x725bd0a6, 0x55199885, 0x58bcc56b, 0x7e7e6288, 0x5ddef6eb}, LnormInv1dSf: [6]int8{0, -2, -4, -3, -7, -10},
		Bmul1: [4]uint32{0x4d178bd4, 0x52ebd467, 0x5a395a6e, 0x6b0f724f}, Bmul1Sf: [4]int8{-11, -5, -1, 0},
	},
	{ // numBands=15
		Lnorm1d: [3]uint32{0x6baa2a22, 0x5222eb91, 0x6bec1a86}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f57393b, 0x49ec8934, 0x423b5b58}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x77fd2486, 0x5cfbdf2c, 0x46153bd1, 0x65757ed9}, Bmul0Sf: [4]int8{-12, -8, -4, -1},
		LnormInv1d: [6]uint32{0x41888ee6, 0x6a661db3, 0x49abc8c8, 0x52965848, 0x6d9301b7, 0x4bb04721}, LnormInv1dSf: [6]int8{0, -2, -4, -3, -7, -10},
		Bmul1: [4]uint32{0x77fd2486, 0x45424c68, 0x50f33cc6, 0x68ff43f0}, Bmul1Sf: [4]int8{-12, -5, -1, 0},
	},
	{ // numBands=16
		Lnorm1d: [3]uint32{0x6bbaa499, 0x5257ed94, 0x6bf456e4}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f412594, 0x49d8a766, 0x432d1dbd}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x5ef5cfde, 0x4eafcd2d, 0x7ed36893, 0x62274b45}, Bmul0Sf: [4]int8{-12, -8, -5, -1},
		LnormInv1d: [6]uint32{0x7ac438f5, 0x637aab21, 0x4067617a, 0x4d3c6ec7, 0x5fd6e0dd, 0x7bd5f024}, LnormInv1dSf: [6]int8{-1, -2, -4, -3, -7, -11},
		Bmul1: [4]uint32{0x5ef5cfde, 0x751d0d4f, 0x492b3c41, 0x67065409}, Bmul1Sf: [4]int8{-12, -6, -1, 0},
	},
	{ // numBands=17
		Lnorm1d: [3]uint32{0x6bc836c9, 0x5283997e, 0x6bfb1f5e}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f2f02b6, 0x49c868e9, 0x44078151}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x4c43b65a, 0x4349dcf6, 0x73799e2d, 0x5f267274}, Bmul0Sf: [4]int8{-12, -8, -5, -1},
		LnormInv1d: [6]uint32{0x73726394, 0x5d68511a, 0x7191bbcc, 0x48898c70, 0x548956e1, 0x66981ce8}, LnormInv1dSf: [6]int8{-1, -2, -5, -3, -7, -11},
		Bmul1: [4]uint32{0x4c43b65a, 0x64131116, 0x429028e2, 0x65240211}, Bmul1Sf: [4]int8{-12, -6, -1, 0},
	},
	{ // numBands=18
		Lnorm1d: [3]uint32{0x6bd3860d, 0x52a80156, 0x6c00c68d}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f1fed86, 0x49baf636, 0x44cdb9dc}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x7c189389, 0x742666d8, 0x69b8c776, 0x5c67e27d}, Bmul0Sf: [4]int8{-13, -9, -5, -1},
		LnormInv1d: [6]uint32{0x6cf1ea76, 0x58095703, 0x64e351a9, 0x4460da90, 0x4b1f8083, 0x55f2d3e1}, LnormInv1dSf: [6]int8{-1, -2, -5, -3, -7, -11},
		Bmul1: [4]uint32{0x7c189389, 0x5651792a, 0x79cb9b3d, 0x635769c0}, Bmul1Sf: [4]int8{-13, -6, -2, 0},
	},
	{ // numBands=19
		Lnorm1d: [3]uint32{0x6bdd0c40, 0x52c6abf6, 0x6c058950}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f133f88, 0x49afb305, 0x45826d73}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x6621a164, 0x6512528e, 0x61449fc8, 0x59e2a0c0}, Bmul0Sf: [4]int8{-13, -9, -5, -1},
		LnormInv1d: [6]uint32{0x6721cadb, 0x53404cd4, 0x5a389e91, 0x40abcbd2, 0x43332f01, 0x48b82e46}, LnormInv1dSf: [6]int8{-1, -2, -5, -3, -7, -11},
		Bmul1: [4]uint32{0x6621a164, 0x4b12cc28, 0x6ffd4df8, 0x619f835e}, Bmul1Sf: [4]int8{-13, -6, -2, 0},
	},
	{ // numBands=20
		Lnorm1d: [3]uint32{0x6be524c5, 0x52e0beb3, 0x6c099552}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5f087c68, 0x49a62bb5, 0x4627d175}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x54ec6afe, 0x58991a42, 0x59e23e8c, 0x578f4ef4}, Bmul0Sf: [4]int8{-13, -9, -5, -1},
		LnormInv1d: [6]uint32{0x61e78f6f, 0x4ef5e1e9, 0x5129c3b8, 0x7ab0f7b2, 0x78efb076, 0x7c2567ea}, LnormInv1dSf: [6]int8{-1, -2, -5, -4, -8, -12},
		Bmul1: [4]uint32{0x54ec6afe, 0x41c7812c, 0x676f6f8d, 0x5ffb383f}, Bmul1Sf: [4]int8{-13, -6, -2, 0},
	},
	{ // numBands=21
		Lnorm1d: [3]uint32{0x6bec1542, 0x52f71929, 0x6c0d0d5e}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5eff45c5, 0x499e092d, 0x46bfc0c9}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x47457a78, 0x4e2d99b3, 0x53637ea5, 0x5567d0e9}, Bmul0Sf: [4]int8{-13, -9, -5, -1},
		LnormInv1d: [6]uint32{0x5d2dc61b, 0x4b1760c8, 0x4967cf39, 0x74b113d8, 0x6d6676b6, 0x6ad114e9}, LnormInv1dSf: [6]int8{-1, -2, -5, -4, -8, -12},
		Bmul1: [4]uint32{0x47457a78, 0x740accaa, 0x5feb6609, 0x5e696f95}, Bmul1Sf: [4]int8{-13, -7, -2, 0},
	},
	{ // numBands=22
		Lnorm1d: [3]uint32{0x6bf21387, 0x530a683c, 0x6c100c59}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ef752ea, 0x499708c6, 0x474bcd1b}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x78a21ab7, 0x45658aec, 0x4da3c4fe, 0x5367094b}, Bmul0Sf: [4]int8{-14, -9, -5, -1},
		LnormInv1d: [6]uint32{0x58e2df6a, 0x4795990e, 0x42b5e0f7, 0x6f408c64, 0x6370bebf, 0x5c91ca85}, LnormInv1dSf: [6]int8{-1, -2, -5, -4, -8, -12},
		Bmul1: [4]uint32{0x78a21ab7, 0x66f951d6, 0x594605bb, 0x5ce91657}, Bmul1Sf: [4]int8{-14, -7, -2, 0},
	},
	{ // numBands=23
		Lnorm1d: [3]uint32{0x6bf749b2, 0x531b3348, 0x6c12a750}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ef06b17, 0x4990f6c9, 0x47cd4c5b}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x66dede36, 0x7bdf90a9, 0x4885b2b9, 0x5188a6b7}, Bmul0Sf: [4]int8{-14, -10, -5, -1},
		LnormInv1d: [6]uint32{0x54f85812, 0x446414ae, 0x79c8d519, 0x6a4c2f31, 0x5ac8325f, 0x50bf9200}, LnormInv1dSf: [6]int8{-1, -2, -6, -4, -8, -12},
		Bmul1: [4]uint32{0x66dede36, 0x5be0d90e, 0x535cc453, 0x5b7923f0}, Bmul1Sf: [4]int8{-14, -7, -2, 0},
	},
	{ // numBands=24
		Lnorm1d: [3]uint32{0x6bfbd91d, 0x5329e580, 0x6c14eeed}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5eea6179, 0x498baa90, 0x4845635d}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x58559b7e, 0x6f1b231f, 0x43f1789b, 0x4fc8fcb8}, Bmul0Sf: [4]int8{-14, -10, -5, -1},
		LnormInv1d: [6]uint32{0x51621775, 0x417881a3, 0x6f9ba9b6, 0x65c412b2, 0x53352c61, 0x46db9caf}, LnormInv1dSf: [6]int8{-1, -2, -6, -4, -8, -12},
		Bmul1: [4]uint32{0x58559b7e, 0x52636003, 0x4e13b316, 0x5a189cdf}, Bmul1Sf: [4]int8{-14, -7, -2, 0},
	},
	{ // numBands=25
		Lnorm1d: [3]uint32{0x6bffdc73, 0x5336d4af, 0x6c16f084}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ee51249, 0x498703cc, 0x48b50e4f}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x4c5616cf, 0x641b9fad, 0x7fa735e0, 0x4e24e57a}, Bmul0Sf: [4]int8{-14, -10, -6, -1},
		LnormInv1d: [6]uint32{0x4e15f47a, 0x7d9481d6, 0x66a82f8a, 0x619ae971, 0x4c8b2f5f, 0x7d09ec11}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -8, -13},
		Bmul1: [4]uint32{0x4c5616cf, 0x4a3770fb, 0x495402de, 0x58c693fa}, Bmul1Sf: [4]int8{-14, -7, -2, 0},
	},
	{ // numBands=26
		Lnorm1d: [3]uint32{0x6c036943, 0x53424625, 0x6c18b6dc}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ee060aa, 0x4982e88a, 0x491d277f}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x425ada5b, 0x5a9368ac, 0x78380a42, 0x4c99aa05}, Bmul0Sf: [4]int8{-14, -10, -6, -1},
		LnormInv1d: [6]uint32{0x4b0b569c, 0x78a420da, 0x5ebdf203, 0x5dc57e63, 0x46a650ff, 0x6ee13fb8}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -8, -13},
		Bmul1: [4]uint32{0x425ada5b, 0x4323073c, 0x450ae92b, 0x57822ad5}, Bmul1Sf: [4]int8{-14, -7, -2, 0},
	},
	{ // numBands=27
		Lnorm1d: [3]uint32{0x6c06911a, 0x534c7261, 0x6c1a4aba}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5edc3524, 0x497f43c0, 0x497e6cd8}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x73fb550e, 0x5244894f, 0x717aad78, 0x4b24ef6c}, Bmul0Sf: [4]int8{-15, -10, -6, -1},
		LnormInv1d: [6]uint32{0x483aebe4, 0x74139116, 0x57b58037, 0x5a3a4f3c, 0x416950fe, 0x62c7f4f2}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -8, -13},
		Bmul1: [4]uint32{0x73fb550e, 0x79efb994, 0x4128cab7, 0x564a919a}, Bmul1Sf: [4]int8{-15, -8, -2, 0},
	},
	{ // numBands=28
		Lnorm1d: [3]uint32{0x6c096264, 0x535587cd, 0x6c1bb355}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ed87c76, 0x497c0439, 0x49d98452}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x65dec5bf, 0x4afd1ba3, 0x6b58b4b3, 0x49c4a7b0}, Bmul0Sf: [4]int8{-15, -10, -6, -1},
		LnormInv1d: [6]uint32{0x459e6eb1, 0x6fd850b7, 0x516e7be9, 0x56f13d05, 0x79785594, 0x58617de7}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -9, -13},
		Bmul1: [4]uint32{0x65dec5bf, 0x6f2168aa, 0x7b41310f, 0x551f0692}, Bmul1Sf: [4]int8{-15, -8, -3, 0},
	},
	{ // numBands=29
		Lnorm1d: [3]uint32{0x6c0be913, 0x535dacd5, 0x6c1cf6a3}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ed526b4, 0x49791bc5, 0x4a2eff99}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x59e44afe, 0x44949ada, 0x65bf36f5, 0x487705a0}, Bmul0Sf: [4]int8{-15, -10, -6, -1},
		LnormInv1d: [6]uint32{0x43307779, 0x6be959c4, 0x4bce2122, 0x53e34d89, 0x7115ff82, 0x4f6421a1}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -9, -13},
		Bmul1: [4]uint32{0x59e44afe, 0x659eab7d, 0x74cea459, 0x53fed574}, Bmul1Sf: [4]int8{-15, -8, -3, 0},
	},
	{ // numBands=30
		Lnorm1d: [3]uint32{0x6c0e2f17, 0x53650181, 0x6c1e199d}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ed2269f, 0x49767e9e, 0x4a7f5f0b}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x4faa4ae6, 0x7dd3bf11, 0x609e2732, 0x473a72e9}, Bmul0Sf: [4]int8{-15, -11, -6, -1},
		LnormInv1d: [6]uint32{0x40ec57c6, 0x683ee147, 0x46be261d, 0x510a7983, 0x698a84cb, 0x4794a927}, LnormInv1dSf: [6]int8{-1, -3, -6, -4, -9, -13},
		Bmul1: [4]uint32{0x4faa4ae6, 0x5d3615ad, 0x6ee74773, 0x52e956a1}, Bmul1Sf: [4]int8{-15, -8, -3, 0},
	},
	{ // numBands=31
		Lnorm1d: [3]uint32{0x6c103cc9, 0x536ba0ac, 0x6c1f2070}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5ecf711e, 0x497422ea, 0x4acb1438}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x46e322ad, 0x73c32f3c, 0x5be7d172, 0x460d8800}, Bmul0Sf: [4]int8{-15, -11, -6, -1},
		LnormInv1d: [6]uint32{0x7d9bf8ad, 0x64d22351, 0x422bdc81, 0x4e6184aa, 0x62ba2375, 0x40c325de}, LnormInv1dSf: [6]int8{-2, -3, -6, -4, -9, -13},
		Bmul1: [4]uint32{0x46e322ad, 0x55bef2a3, 0x697b3135, 0x51ddee4d}, Bmul1Sf: [4]int8{-15, -8, -3, 0},
	},
	{ // numBands=32
		Lnorm1d: [3]uint32{0x6c121933, 0x5371a104, 0x6c200ea0}, Lnorm1dSf: [3]int8{-1, 0, 0},
		Lnormii: [3]uint32{0x5eccfcd3, 0x49720060, 0x4b1283f0}, LnormiiSf: [3]int8{3, 4, 2},
		Bmul0: [4]uint32{0x7ea12a52, 0x6aca3303, 0x579072bf, 0x44ef056e}, Bmul0Sf: [4]int8{-16, -11, -6, -1},
		LnormInv1d: [6]uint32{0x79a3a9ab, 0x619d38fc, 0x7c0f0734, 0x4be3dd5d, 0x5c8d7163, 0x7591065f}, LnormInv1dSf: [6]int8{-2, -3, -7, -4, -9, -14},
		Bmul1: [4]uint32{0x7ea12a52, 0x4f1782a6, 0x647cbcb2, 0x50dc0bb1}, Bmul1Sf: [4]int8{-16, -8, -3, 0},
	},
}

// bsd is the narrowed backsubst_data table (FIXP_CHB == FIXP_SGL). Materialised at
// init via StcNarrow == FX_DBL2FXCONST_SGL, the macro the C CHC() expands to.
var bsd [nNumBands]backsubstData

func init() {
	nar := func(src []uint32, dst []int16) {
		for i, v := range src {
			dst[i] = nativeaac.StcNarrow(int32(v))
		}
	}
	for i := range bsdRaw {
		r := &bsdRaw[i]
		d := &bsd[i]
		nar(r.Lnorm1d[:], d.Lnorm1d[:])
		nar(r.Lnormii[:], d.Lnormii[:])
		nar(r.Bmul0[:], d.Bmul0[:])
		nar(r.LnormInv1d[:], d.LnormInv1d[:])
		nar(r.Bmul1[:], d.Bmul1[:])
		d.Lnorm1dSf = r.Lnorm1dSf
		d.LnormiiSf = r.LnormiiSf
		d.Bmul0Sf = r.Bmul0Sf
		d.LnormInv1dSf = r.LnormInv1dSf
		d.Bmul1Sf = r.Bmul1Sf
	}
}
