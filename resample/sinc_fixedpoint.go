package resample

// Fixed-point arithmetic for sinc filter coefficient indexing.
// Uses 12-bit fractional precision with int32 representation.
//
// Bit layout of an increment value:
//   bits 31-12: integer part
//   bits 11-0:  fractional part (4096 sub-steps)

const (
	shiftBits = 12
	fpOne     = float64(int32(1) << shiftBits) // 4096.0
	invFPOne  = 1.0 / fpOne
)

// increment is a fixed-point value with 12-bit fractional precision.
type increment int32

func doubleToFP(x float64) increment {
	return increment(lrint(x * fpOne))
}

func intToFP(x int) increment {
	return increment(int32(x) << shiftBits)
}

func fpToInt(x increment) int {
	return int(int32(x) >> shiftBits)
}

func fpFractionPart(x increment) increment {
	return x & increment((int32(1)<<shiftBits)-1)
}

func fpToDouble(x increment) float64 {
	return float64(fpFractionPart(x)) * invFPOne
}
