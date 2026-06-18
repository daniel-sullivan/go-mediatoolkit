package nativeopus

// Thin external accessors for SILK-macro parity tests. Same pattern as
// export_for_testing.go; see that file for rationale. All wrappers are
// one-liners calling the unexported snake_case port.

func ExportTestSilkSMULWB(a, b int32) int32    { return silk_SMULWB(a, b) }
func ExportTestSilkSMLAWB(a, b, c int32) int32 { return silk_SMLAWB(a, b, c) }
func ExportTestSilkSMULWT(a, b int32) int32    { return silk_SMULWT(a, b) }
func ExportTestSilkSMLAWT(a, b, c int32) int32 { return silk_SMLAWT(a, b, c) }
func ExportTestSilkSMULBB(a, b int32) int32    { return silk_SMULBB(a, b) }
func ExportTestSilkSMLABB(a, b, c int32) int32 { return silk_SMLABB(a, b, c) }
func ExportTestSilkSMULBT(a, b int32) int32    { return silk_SMULBT(a, b) }
func ExportTestSilkSMLABT(a, b, c int32) int32 { return silk_SMLABT(a, b, c) }
func ExportTestSilkSMLAL(a int64, b, c int32) int64 {
	return silk_SMLAL(a, b, c)
}
func ExportTestSilkSMULWW(a, b int32) int32    { return silk_SMULWW(a, b) }
func ExportTestSilkSMLAWW(a, b, c int32) int32 { return silk_SMLAWW(a, b, c) }
func ExportTestSilkSMMUL(a, b int32) int32     { return silk_SMMUL(a, b) }
func ExportTestSilkSMULL(a, b int32) int64     { return silk_SMULL(a, b) }

func ExportTestSilkCLZ16(v int16) int32 { return silk_CLZ16(v) }
func ExportTestSilkCLZ32(v int32) int32 { return silk_CLZ32(v) }
func ExportTestSilkCLZ64(v int64) int32 { return silk_CLZ64(v) }

func ExportTestSilkADDSAT32(a, b int32) int32 { return silk_ADD_SAT32(a, b) }
func ExportTestSilkSUBSAT32(a, b int32) int32 { return silk_SUB_SAT32(a, b) }
func ExportTestSilkADDSAT64(a, b int64) int64 { return silk_ADD_SAT64(a, b) }
func ExportTestSilkSUBSAT64(a, b int64) int64 { return silk_SUB_SAT64(a, b) }

func ExportTestSilkROR32(a int32, rot int) int32 { return silk_ROR32(a, rot) }

func ExportTestSilkRSHIFTROUND(a int32, s int) int32   { return silk_RSHIFT_ROUND(a, s) }
func ExportTestSilkRSHIFTROUND64(a int64, s int) int64 { return silk_RSHIFT_ROUND64(a, s) }

func ExportTestSilkRAND(seed int32) int32              { return silk_RAND(seed) }
func ExportTestSilkSQRTAPPROX(x int32) int32           { return silk_SQRT_APPROX(x) }
func ExportTestSilkDIV32varQ(a, b int32, Q int) int32  { return silk_DIV32_varQ(a, b, Q) }
func ExportTestSilkINVERSE32varQ(b int32, Q int) int32 { return silk_INVERSE32_varQ(b, Q) }
