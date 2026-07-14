package gtcrn

import (
	"fmt"
	"strings"
)

// Typed weight bindings for the forward pass, resolved once from the
// embedded tensor map (manifest.json decodes the opaque onnx names).

type convBlockW struct { // encoder ConvBlock (fused Conv+BN) + PReLU
	w, b  []float32
	slope float32
}

type traW struct {
	gruW, gruR, gruB []float32 // ONNX GRU, hidden 16, in 8
	fcW, fcB         []float32 // att_fc 16->8
}

type bnW struct{ g, b, mean, variance []float32 }

type gtEncW struct {
	dil            int
	pc1W, pc1B     []float32 // fused Conv 24->16
	pc1Slope       float32
	depthW, depthB []float32 // fused depth Conv 16->16
	depthSlope     float32
	pc2W, pc2B     []float32 // fused Conv 16->8
	tra            traW
}

type gtDecW struct {
	dil            int
	pc1W, pc1B     []float32 // ConvTranspose 24->16 (1x1)
	pbn1           bnW
	pc1Slope       float32
	depthW, depthB []float32 // ConvTranspose depth 16->16
	dbn            bnW
	depthSlope     float32
	pc2W, pc2B     []float32 // ConvTranspose 16->8 (1x1)
	pbn2           bnW
	tra            traW
}

type dpgrnnW struct {
	// intra: bidirectional grouped GRU over freq (rnn1/rnn2, hid 4, in 8)
	i1W, i1R, i1B []float32
	i2W, i2R, i2B []float32
	intraFCW      []float32
	intraFCB      []float32
	intraLNg      []float32
	intraLNb      []float32
	// inter: unidirectional grouped GRU over time (rnn1/rnn2, hid 8, in 8)
	t1W, t1R, t1B []float32
	t2W, t2R, t2B []float32
	interFCW      []float32
	interFCB      []float32
	interLNg      []float32
	interLNb      []float32
}

type decConvW struct { // decoder ConvBlock (ConvTranspose + explicit BN) + act
	w, b  []float32
	bn    bnW
	slope float32 // used by de3 (PReLU); de4 is Tanh (is_last)
}

type weights struct {
	erbFC  []float32 // [192,64]
	ierbFC []float32 // [64,192]
	enc    [2]convBlockW
	gtEnc  [3]gtEncW
	dp     [2]dpgrnnW
	gtDec  [3]gtDecW
	dec    [2]decConvW // de3, de4
}

// buildWeights resolves every named tensor, collecting missing names.
func buildWeights(t map[string]Tensor) (*weights, error) {
	var missing []string
	d := func(name string) []float32 {
		tn, ok := t[name]
		if !ok {
			missing = append(missing, name)
			return nil
		}
		return tn.Data
	}
	s := func(name string) float32 {
		v := d(name)
		if v == nil {
			return 0
		}
		return v[0]
	}
	bn := func(prefix string) bnW {
		return bnW{d(prefix + ".weight"), d(prefix + ".bias"),
			d(prefix + ".running_mean"), d(prefix + ".running_var")}
	}

	w := new(weights)
	w.erbFC = d("onnx::MatMul_3414")
	w.ierbFC = d("onnx::MatMul_3932")

	w.enc[0] = convBlockW{d("onnx::Conv_3380"), d("onnx::Conv_3381"), s("onnx::PRelu_3422")}
	w.enc[1] = convBlockW{d("onnx::Conv_3383"), d("onnx::Conv_3384"), s("onnx::PRelu_3423")}

	// Encoder GTConvBlocks (dilations 1, 2, 5).
	encConv := [3][]string{
		{"onnx::Conv_3386", "onnx::Conv_3387", "onnx::PRelu_3431", "onnx::Conv_3389", "onnx::Conv_3390", "onnx::PRelu_3432", "onnx::Conv_3392", "onnx::Conv_3393", "onnx::GRU_3451", "onnx::GRU_3452", "onnx::GRU_3453", "onnx::MatMul_3454", "encoder.en_convs.2.tra.att_fc.bias"},
		{"onnx::Conv_3395", "onnx::Conv_3396", "onnx::PRelu_3472", "onnx::Conv_3398", "onnx::Conv_3399", "onnx::PRelu_3473", "onnx::Conv_3401", "onnx::Conv_3402", "onnx::GRU_3492", "onnx::GRU_3493", "onnx::GRU_3494", "onnx::MatMul_3495", "encoder.en_convs.3.tra.att_fc.bias"},
		{"onnx::Conv_3404", "onnx::Conv_3405", "onnx::PRelu_3513", "onnx::Conv_3407", "onnx::Conv_3408", "onnx::PRelu_3514", "onnx::Conv_3410", "onnx::Conv_3411", "onnx::GRU_3533", "onnx::GRU_3534", "onnx::GRU_3535", "onnx::MatMul_3536", "encoder.en_convs.4.tra.att_fc.bias"},
	}
	dils := [3]int{1, 2, 5}
	for i, n := range encConv {
		w.gtEnc[i] = gtEncW{
			dil:  dils[i],
			pc1W: d(n[0]), pc1B: d(n[1]), pc1Slope: s(n[2]),
			depthW: d(n[3]), depthB: d(n[4]), depthSlope: s(n[5]),
			pc2W: d(n[6]), pc2B: d(n[7]),
			tra: traW{d(n[8]), d(n[9]), d(n[10]), d(n[11]), d(n[12])},
		}
	}

	// DPGRNN 1 and 2.
	dpNames := [2]struct{ intra1, intra2, intraFC, intraLN, inter1, inter2, interFC, interLN string }{
		{"3595|3596|3594", "3638|3639|3637", "onnx::MatMul_3640", "dpgrnn1.intra", "3661|3662|3663", "3681|3682|3683", "onnx::MatMul_3684", "dpgrnn1.inter"},
		{"3733|3734|3732", "3776|3777|3775", "onnx::MatMul_3778", "dpgrnn2.intra", "3799|3800|3801", "3819|3820|3821", "onnx::MatMul_3822", "dpgrnn2.inter"},
	}
	gru3 := func(spec string) (W, R, B []float32) {
		p := strings.Split(spec, "|")
		return d("onnx::GRU_" + p[0]), d("onnx::GRU_" + p[1]), d("onnx::GRU_" + p[2])
	}
	for i, n := range dpNames {
		var dp dpgrnnW
		dp.i1W, dp.i1R, dp.i1B = gru3(n.intra1)
		dp.i2W, dp.i2R, dp.i2B = gru3(n.intra2)
		dp.intraFCW, dp.intraFCB = d(n.intraFC), d(n.intraLN+"_fc.bias")
		dp.intraLNg, dp.intraLNb = d(n.intraLN+"_ln.weight"), d(n.intraLN+"_ln.bias")
		dp.t1W, dp.t1R, dp.t1B = gru3(n.inter1)
		dp.t2W, dp.t2R, dp.t2B = gru3(n.inter2)
		dp.interFCW, dp.interFCB = d(n.interFC), d(n.interLN+"_fc.bias")
		dp.interLNg, dp.interLNb = d(n.interLN+"_ln.weight"), d(n.interLN+"_ln.bias")
		w.dp[i] = dp
	}

	// Decoder GTConvBlocks (dilations 5, 2, 1).
	decDil := [3]int{5, 2, 1}
	decPrelu := [3][2]string{{"onnx::PRelu_3834", "onnx::PRelu_3835"}, {"onnx::PRelu_3867", "onnx::PRelu_3868"}, {"onnx::PRelu_3900", "onnx::PRelu_3901"}}
	decGRU := [3]string{"3854|3855|3856", "3887|3888|3889", "3920|3921|3922"}
	decFC := [3]string{"onnx::MatMul_3857", "onnx::MatMul_3890", "onnx::MatMul_3923"}
	for i := 0; i < 3; i++ {
		p := fmt.Sprintf("decoder.de_convs.%d", i)
		gw, gr, gb := gru3(decGRU[i])
		w.gtDec[i] = gtDecW{
			dil:  decDil[i],
			pc1W: d(p + ".point_conv1.weight"), pc1B: d(p + ".point_conv1.bias"),
			pbn1: bn(p + ".point_bn1"), pc1Slope: s(decPrelu[i][0]),
			depthW: d(p + ".depth_conv.ConvTranspose2d.weight"), depthB: d(p + ".depth_conv.ConvTranspose2d.bias"),
			dbn: bn(p + ".depth_bn"), depthSlope: s(decPrelu[i][1]),
			pc2W: d(p + ".point_conv2.weight"), pc2B: d(p + ".point_conv2.bias"),
			pbn2: bn(p + ".point_bn2"),
			tra:  traW{gw, gr, gb, d(decFC[i]), d(p + ".tra.att_fc.bias")},
		}
	}

	w.dec[0] = decConvW{d("decoder.de_convs.3.conv.weight"), d("decoder.de_convs.3.conv.bias"), bn("decoder.de_convs.3.bn"), s("onnx::PRelu_3927")}
	w.dec[1] = decConvW{d("decoder.de_convs.4.conv.weight"), d("decoder.de_convs.4.conv.bias"), bn("decoder.de_convs.4.bn"), 0}

	if len(missing) > 0 {
		return nil, fmt.Errorf("gtcrn: missing %d weight tensors: %s", len(missing), strings.Join(missing[:min(len(missing), 8)], ", "))
	}
	return w, nil
}

// convCacheOffset returns the [start,len) of a dilation's slice within
// the size-16 temporal cache axis (dil1->[0:2], dil2->[2:6], dil5->[6:16]).
func convCacheOffset(dil int) (start, length int) {
	switch dil {
	case 1:
		return 0, 2
	case 2:
		return 2, 4
	default: // 5
		return 6, 10
	}
}
