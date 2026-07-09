// This file ports the direct-form-1 cascaded biquad filter:
//   - modules/audio_processing/utility/cascaded_biquad_filter.{h,cc}
package aec3

// BiQuadParam specifies a biquad section by its zero/pole locations
// in the complex plane (each a conjugate pair unless
// MirrorZeroAlongIAxis is set) plus an overall gain. Port of
// CascadedBiQuadFilter::BiQuadParam.
type BiQuadParam struct {
	ZeroR, ZeroI         float32
	PoleR, PoleI         float32
	Gain                 float32
	MirrorZeroAlongIAxis bool
}

// biQuadCoefficients holds the resulting direct-form-1 transfer
// function coefficients. C: CascadedBiQuadFilter::BiQuadCoefficients.
type biQuadCoefficients struct {
	b [3]float32
	a [2]float32
}

// biQuad is one direct-form-1 biquad section with its running state.
// C: CascadedBiQuadFilter::BiQuad.
type biQuad struct {
	coefficients biQuadCoefficients
	x            [2]float32
	y            [2]float32
}

// newBiQuadFromCoefficients mirrors the BiQuad(coefficients) ctor.
func newBiQuadFromCoefficients(c biQuadCoefficients) *biQuad {
	return &biQuad{coefficients: c}
}

// newBiQuadFromParam converts a BiQuadParam's zero/pole/gain
// specification into direct-form-1 coefficients. C: BiQuad::BiQuad(const
// BiQuadParam&).
func newBiQuadFromParam(param BiQuadParam) *biQuad {
	zR, zI := param.ZeroR, param.ZeroI
	pR, pI := param.PoleR, param.PoleI
	gain := param.Gain

	var c biQuadCoefficients
	if param.MirrorZeroAlongIAxis {
		// Assuming zeroes at z_r and -z_r.
		c.b[0] = mul32(gain, 1)
		c.b[1] = 0
		c.b[2] = mul32(gain, -mul32(zR, zR))
	} else {
		// Assuming zeros at (z_r + z_i*i) and (z_r - z_i*i).
		c.b[0] = mul32(gain, 1)
		c.b[1] = mul32(mul32(gain, -2), zR)
		c.b[2] = mul32(gain, add32(mul32(zR, zR), mul32(zI, zI)))
	}

	// Assuming poles at (p_r + p_i*i) and (p_r - p_i*i).
	c.a[0] = mul32(-2, pR)
	c.a[1] = add32(mul32(pR, pR), mul32(pI, pI))

	return &biQuad{coefficients: c}
}

// reset zeros the biquad's running state. C: BiQuad::Reset.
func (b *biQuad) reset() {
	b.x[0], b.x[1] = 0, 0
	b.y[0], b.y[1] = 0, 0
}

// CascadedBiQuadFilter applies a number of biquads in a cascaded
// manner, direct form 1. Port of CascadedBiQuadFilter.
type CascadedBiQuadFilter struct {
	biquads []*biQuad
}

// NewCascadedBiQuadFilterCoefficients builds num identical biquads
// from a single coefficient set. C: CascadedBiQuadFilter(coefficients,
// num_biquads).
func NewCascadedBiQuadFilterCoefficients(b [3]float32, a [2]float32, num int) *CascadedBiQuadFilter {
	c := biQuadCoefficients{b: b, a: a}
	f := &CascadedBiQuadFilter{biquads: make([]*biQuad, num)}
	for i := range f.biquads {
		f.biquads[i] = newBiQuadFromCoefficients(c)
	}
	return f
}

// NewCascadedBiQuadFilter builds one biquad per param, in order. C:
// CascadedBiQuadFilter(const std::vector<BiQuadParam>&).
func NewCascadedBiQuadFilter(params []BiQuadParam) *CascadedBiQuadFilter {
	f := &CascadedBiQuadFilter{biquads: make([]*biQuad, 0, len(params))}
	for _, p := range params {
		f.biquads = append(f.biquads, newBiQuadFromParam(p))
	}
	return f
}

// applyBiQuad filters x into y through one biquad, updating its state.
// C: CascadedBiQuadFilter::ApplyBiQuad. The five-term direct-form-1
// recurrence is expressed via the package's mul32/add32/sub32/mla/
// muladd primitives (see fp_strict.go/fp_default.go) so that under
// aec_strict it reproduces the oracle's -ffp-contract=off left-to-right
// rounding bit-for-bit.
func applyBiQuad(x []float32, y []float32, bq *biQuad) {
	cA0, cA1 := bq.coefficients.a[0], bq.coefficients.a[1]
	cB0, cB1, cB2 := bq.coefficients.b[0], bq.coefficients.b[1], bq.coefficients.b[2]
	mX0, mX1 := bq.x[0], bq.x[1]
	mY0, mY1 := bq.y[0], bq.y[1]

	for k := range x {
		tmp := x[k]
		s := muladd(cB0, tmp, cB1, mX0)
		s = mla(s, cB2, mX1)
		s = sub32(s, mul32(cA0, mY0))
		s = sub32(s, mul32(cA1, mY1))
		y[k] = s
		mX1 = mX0
		mX0 = tmp
		mY1 = mY0
		mY0 = y[k]
	}

	bq.x[0], bq.x[1] = mX0, mX1
	bq.y[0], bq.y[1] = mY0, mY1
}

// Process applies the biquads on the values in x, forming the output
// in y (x and y may not alias). C: CascadedBiQuadFilter::Process(x, y).
func (f *CascadedBiQuadFilter) Process(x []float32, y []float32) {
	if len(f.biquads) > 0 {
		applyBiQuad(x, y, f.biquads[0])
		for k := 1; k < len(f.biquads); k++ {
			applyBiQuad(y, y, f.biquads[k])
		}
	} else {
		copy(y, x)
	}
}

// ProcessInPlace applies the biquads on y in an in-place manner. C:
// CascadedBiQuadFilter::Process(y).
func (f *CascadedBiQuadFilter) ProcessInPlace(y []float32) {
	for _, bq := range f.biquads {
		applyBiQuad(y, y, bq)
	}
}

// Reset resets the filter to its initial state. C:
// CascadedBiQuadFilter::Reset.
func (f *CascadedBiQuadFilter) Reset() {
	for _, bq := range f.biquads {
		bq.reset()
	}
}
