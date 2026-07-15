package rnnoise

import "math"

// Pitch analysis, ported 1:1 from librnnoise/src/pitch.c (float build).
// This is the highest-risk slice: remove_doubling's candidate selection
// is driven by float comparisons, so a 1-ulp divergence can flip control
// flow and pick a different pitch lag. Every multiply-accumulate goes
// through the strict primitives; compute_pitch_gain's sqrt is done in
// float64 like the C (double sqrt of a float argument).

var secondCheck = [16]int{0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// maxf / minf mirror MAX32/MIN32's ternary exactly ((a>b)?a:b), so NaN
// handling matches the C.
func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// xcorrKernel is pitch.h xcorr_kernel: four lagged correlations of x
// against y in one pass, accumulating into sum. x and y read from index 0.
func xcorrKernel(x, y []float32, sum *[4]float32, length int) {
	var y0, y1, y2, y3 float32
	xp, yp := 0, 0
	y0 = y[yp]
	yp++
	y1 = y[yp]
	yp++
	y2 = y[yp]
	yp++
	j := 0
	for ; j < length-3; j += 4 {
		var tmp float32
		tmp = x[xp]
		xp++
		y3 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y0)
		sum[1] = mla(sum[1], tmp, y1)
		sum[2] = mla(sum[2], tmp, y2)
		sum[3] = mla(sum[3], tmp, y3)
		tmp = x[xp]
		xp++
		y0 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y1)
		sum[1] = mla(sum[1], tmp, y2)
		sum[2] = mla(sum[2], tmp, y3)
		sum[3] = mla(sum[3], tmp, y0)
		tmp = x[xp]
		xp++
		y1 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y2)
		sum[1] = mla(sum[1], tmp, y3)
		sum[2] = mla(sum[2], tmp, y0)
		sum[3] = mla(sum[3], tmp, y1)
		tmp = x[xp]
		xp++
		y2 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y3)
		sum[1] = mla(sum[1], tmp, y0)
		sum[2] = mla(sum[2], tmp, y1)
		sum[3] = mla(sum[3], tmp, y2)
	}
	if j < length {
		j++
		tmp := x[xp]
		xp++
		y3 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y0)
		sum[1] = mla(sum[1], tmp, y1)
		sum[2] = mla(sum[2], tmp, y2)
		sum[3] = mla(sum[3], tmp, y3)
	}
	if j < length {
		j++
		tmp := x[xp]
		xp++
		y0 = y[yp]
		yp++
		sum[0] = mla(sum[0], tmp, y1)
		sum[1] = mla(sum[1], tmp, y2)
		sum[2] = mla(sum[2], tmp, y3)
		sum[3] = mla(sum[3], tmp, y0)
	}
	if j < length {
		tmp := x[xp]
		y1 = y[yp]
		sum[0] = mla(sum[0], tmp, y2)
		sum[1] = mla(sum[1], tmp, y3)
		sum[2] = mla(sum[2], tmp, y0)
		sum[3] = mla(sum[3], tmp, y1)
	}
	_ = y1
}

// celtInnerProd is pitch.h celt_inner_prod.
func celtInnerProd(x, y []float32, n int) float32 {
	var xy float32
	for i := 0; i < n; i++ {
		xy = mla(xy, x[i], y[i])
	}
	return xy
}

// dualInnerProd is pitch.h dual_inner_prod.
func dualInnerProd(x, y01, y02 []float32, n int) (xy1, xy2 float32) {
	for i := 0; i < n; i++ {
		xy1 = mla(xy1, x[i], y01[i])
		xy2 = mla(xy2, x[i], y02[i])
	}
	return xy1, xy2
}

// pitchXcorr is pitch.c rnn_pitch_xcorr (unrolled version).
func pitchXcorr(x, y, xcorr []float32, length, maxPitch int) {
	i := 0
	for ; i < maxPitch-3; i += 4 {
		var sum [4]float32
		xcorrKernel(x, y[i:], &sum, length)
		xcorr[i] = sum[0]
		xcorr[i+1] = sum[1]
		xcorr[i+2] = sum[2]
		xcorr[i+3] = sum[3]
	}
	for ; i < maxPitch; i++ {
		xcorr[i] = celtInnerProd(x, y[i:], length)
	}
}

// findBestPitch is pitch.c find_best_pitch (float branch).
func findBestPitch(xcorr, y []float32, length, maxPitch int) [2]int {
	var Syy float32 = 1
	bestNum := [2]float32{-1, -1}
	bestDen := [2]float32{0, 0}
	bestPitch := [2]int{0, 1}
	for j := 0; j < length; j++ {
		Syy = add32(Syy, mul32(y[j], y[j]))
	}
	for i := 0; i < maxPitch; i++ {
		if xcorr[i] > 0 {
			xcorr16 := xcorr[i]
			xcorr16 = mul32(xcorr16, 1e-12)
			num := mul32(xcorr16, xcorr16)
			if mul32(num, bestDen[1]) > mul32(bestNum[1], Syy) {
				if mul32(num, bestDen[0]) > mul32(bestNum[0], Syy) {
					bestNum[1] = bestNum[0]
					bestDen[1] = bestDen[0]
					bestPitch[1] = bestPitch[0]
					bestNum[0] = num
					bestDen[0] = Syy
					bestPitch[0] = i
				} else {
					bestNum[1] = num
					bestDen[1] = Syy
					bestPitch[1] = i
				}
			}
		}
		Syy = add32(Syy, sub32(mul32(y[i+length], y[i+length]), mul32(y[i], y[i])))
		Syy = maxf(1, Syy)
	}
	return bestPitch
}

// celtFir5 is pitch.c celt_fir5. x and y may be the same buffer.
func celtFir5(x, num, y []float32, n int, mem []float32) {
	num0, num1, num2, num3, num4 := num[0], num[1], num[2], num[3], num[4]
	mem0, mem1, mem2, mem3, mem4 := mem[0], mem[1], mem[2], mem[3], mem[4]
	for i := 0; i < n; i++ {
		xi := x[i]
		sum := xi
		sum = mla(sum, num0, mem0)
		sum = mla(sum, num1, mem1)
		sum = mla(sum, num2, mem2)
		sum = mla(sum, num3, mem3)
		sum = mla(sum, num4, mem4)
		mem4 = mem3
		mem3 = mem2
		mem2 = mem1
		mem1 = mem0
		mem0 = xi
		y[i] = sum
	}
	mem[0], mem[1], mem[2], mem[3], mem[4] = mem0, mem1, mem2, mem3, mem4
}

// pitchDownsample is pitch.c rnn_pitch_downsample for C==1 (single
// channel): decimate-by-2 with a half-band pre-filter derived from a
// 4th-order LPC, into xLp of length len/2.
func pitchDownsample(x, xLp []float32, length int) {
	var ac [5]float32
	tmp := float32(1)
	var lpc [4]float32
	var mem [5]float32
	var lpc2 [5]float32
	const c1 float32 = 0.8

	for i := 1; i < length>>1; i++ {
		xLp[i] = mul32(0.5, add32(mul32(0.5, add32(x[2*i-1], x[2*i+1])), x[2*i]))
	}
	xLp[0] = mul32(0.5, add32(mul32(0.5, x[1]), x[0]))

	rnnAutocorr(xLp[:length>>1], ac[:], 4, length>>1)
	ac[0] = mul32(ac[0], 1.0001)
	for i := 1; i <= 4; i++ {
		t := mul32(0.008, float32(i))
		ac[i] = sub32(ac[i], mul32(mul32(ac[i], t), t))
	}
	rnnLpc(lpc[:], ac[:], 4)
	for i := 0; i < 4; i++ {
		tmp = mul32(0.9, tmp)
		lpc[i] = mul32(lpc[i], tmp)
	}
	lpc2[0] = add32(lpc[0], 0.8)
	lpc2[1] = add32(lpc[1], mul32(c1, lpc[0]))
	lpc2[2] = add32(lpc[2], mul32(c1, lpc[1]))
	lpc2[3] = add32(lpc[3], mul32(c1, lpc[2]))
	lpc2[4] = mul32(c1, lpc[3])
	celtFir5(xLp[:length>>1], lpc2[:], xLp[:length>>1], length>>1, mem[:])
}

// pitchSearch is pitch.c rnn_pitch_search (float branch). Returns the
// coarse pitch lag. xLp and y may alias (in the pipeline y is the base
// buffer and xLp is offset by PITCH_MAX_PERIOD/2).
func pitchSearch(xLp, y []float32, length, maxPitch int) int {
	lag := length + maxPitch
	var xLp4 [PitchFrameSize >> 2]float32
	var yLp4 [(PitchFrameSize + PitchMaxPeriod) >> 2]float32
	var xcorr [PitchMaxPeriod >> 1]float32

	for j := 0; j < length>>2; j++ {
		xLp4[j] = xLp[2*j]
	}
	for j := 0; j < lag>>2; j++ {
		yLp4[j] = y[2*j]
	}

	pitchXcorr(xLp4[:length>>2], yLp4[:], xcorr[:], length>>2, maxPitch>>2)
	bestPitch := findBestPitch(xcorr[:], yLp4[:], length>>2, maxPitch>>2)

	for i := 0; i < maxPitch>>1; i++ {
		xcorr[i] = 0
		if iabs(i-2*bestPitch[0]) > 2 && iabs(i-2*bestPitch[1]) > 2 {
			continue
		}
		sum := celtInnerProd(xLp, y[i:], length>>1)
		xcorr[i] = maxf(-1, sum)
	}
	bestPitch = findBestPitch(xcorr[:], y, length>>1, maxPitch>>1)

	offset := 0
	if bestPitch[0] > 0 && bestPitch[0] < (maxPitch>>1)-1 {
		a := xcorr[bestPitch[0]-1]
		b := xcorr[bestPitch[0]]
		c := xcorr[bestPitch[0]+1]
		if sub32(c, a) > mul32(0.7, sub32(b, a)) {
			offset = 1
		} else if sub32(a, c) > mul32(0.7, sub32(b, c)) {
			offset = -1
		} else {
			offset = 0
		}
	}
	return 2*bestPitch[0] - offset
}

// computePitchGain is pitch.c compute_pitch_gain (float branch):
// xy/sqrt(1+xx*yy). The 1+xx*yy is float; sqrt is double (the float
// argument is promoted), matching the C.
func computePitchGain(xy, xx, yy float32) float32 {
	inner := add32(1, mul32(xx, yy))
	return float32(float64(xy) / math.Sqrt(float64(inner)))
}

// removeDoubling is pitch.c rnn_remove_doubling (float branch). x is the
// downsampled pitch buffer; it internally offsets by maxperiod/2. Returns
// the pitch gain and the updated lag (the C writes back through T0_).
func removeDoubling(x []float32, maxperiod, minperiod, n, t0In, prevPeriod int, prevGain float32) (pg float32, t0Out int) {
	var xcorr [3]float32

	minperiod0 := minperiod
	maxperiod /= 2
	minperiod /= 2
	t0 := t0In / 2
	prevPeriod /= 2
	n /= 2
	base := maxperiod // x += maxperiod
	if t0 >= maxperiod {
		t0 = maxperiod - 1
	}

	tVar := t0
	// dual_inner_prod(x, x, x-T0, N, &xx, &xy)
	xx, xy := dualInnerProd(x[base:], x[base:], x[base-t0:], n)

	var yyLookup [PitchMaxPeriod + 1]float32
	yyLookup[0] = xx
	yy := xx
	for i := 1; i <= maxperiod; i++ {
		yy = sub32(add32(yy, mul32(x[base-i], x[base-i])), mul32(x[base+n-i], x[base+n-i]))
		yyLookup[i] = maxf(0, yy)
	}
	yy = yyLookup[t0]
	bestXy := xy
	bestYy := yy
	g := computePitchGain(xy, xx, yy)
	g0 := g

	for k := 2; k <= 15; k++ {
		var cont float32
		t1 := (2*t0 + k) / (2 * k)
		if t1 < minperiod {
			break
		}
		var t1b int
		if k == 2 {
			if t1+t0 > maxperiod {
				t1b = t0
			} else {
				t1b = t0 + t1
			}
		} else {
			t1b = (2*secondCheck[k]*t0 + k) / (2 * k)
		}
		xyA, xy2 := dualInnerProd(x[base:], x[base-t1:], x[base-t1b:], n)
		xyH := mul32(0.5, add32(xyA, xy2))
		yyH := mul32(0.5, add32(yyLookup[t1], yyLookup[t1b]))
		g1 := computePitchGain(xyH, xx, yyH)
		if iabs(t1-prevPeriod) <= 1 {
			cont = prevGain
		} else if iabs(t1-prevPeriod) <= 2 && 5*k*k < t0 {
			cont = mul32(0.5, prevGain)
		} else {
			cont = 0
		}
		thresh := maxf(0.3, sub32(mul32(0.7, g0), cont))
		if t1 < 3*minperiod {
			thresh = maxf(0.4, sub32(mul32(0.85, g0), cont))
		} else if t1 < 2*minperiod {
			thresh = maxf(0.5, sub32(mul32(0.9, g0), cont))
		}
		if g1 > thresh {
			bestXy = xyH
			bestYy = yyH
			tVar = t1
			g = g1
		}
	}
	bestXy = maxf(0, bestXy)
	if bestYy <= bestXy {
		pg = 1
	} else {
		pg = bestXy / add32(bestYy, 1)
	}

	for k := 0; k < 3; k++ {
		xcorr[k] = celtInnerProd(x[base:], x[base-(tVar+k-1):], n)
	}
	var offset int
	if sub32(xcorr[2], xcorr[0]) > mul32(0.7, sub32(xcorr[1], xcorr[0])) {
		offset = 1
	} else if sub32(xcorr[0], xcorr[2]) > mul32(0.7, sub32(xcorr[1], xcorr[2])) {
		offset = -1
	} else {
		offset = 0
	}
	if pg > g {
		pg = g
	}
	t0Out = 2*tVar + offset
	if t0Out < minperiod0 {
		t0Out = minperiod0
	}
	return pg, t0Out
}
