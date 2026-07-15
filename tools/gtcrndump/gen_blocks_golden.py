"""
Generate per-block INTERMEDIATE golden vectors for the GTCRN Go port.

Interpreter: /Users/daniel/ml-venv/bin/python3
Run:  cd /Volumes/Storage/ClaudeProjects/go-mediatoolkit/tools/gtcrndump && \
      /Users/daniel/ml-venv/bin/python3 gen_blocks_golden.py

Emits: denoise/internal/gtcrn/testdata/blocks_golden.json

Method (a): a torch StreamGTCRN (DNS3 weights, converted with the committed
Version-2 StreamConvTranspose2d) is stepped over the first 8 frames of the
white-noise STFT; after each named sub-block the intermediate tensor is captured
per frame. CROSS-VALIDATION: the same 8 frames are run through the ONNX oracle
(gtcrn.onnx) in ORT and max|enh_torch - enh_ort| is recorded in metadata.

CAVEAT: the committed convolution.py is the newer Version-2 StreamConvTranspose2d
(Conv2d-with-weight-flip), numerically ~equal but NOT bit-identical to the ONNX's
real ConvTranspose. So these torch intermediates are a TOLERANCE reference
(~1e-4), not bit-exact. The Go port's oracle is gtcrn.onnx (ORT); these blocks
only localize WHICH block diverges.
"""
import json
import os
import sys
import numpy as np
import torch

sys.path.insert(0, "/tmp/gtcrn_fetch/streampkg")
from signals import canonical_signals

OUT = "/Volumes/Storage/ClaudeProjects/go-mediatoolkit/denoise/internal/gtcrn/testdata/blocks_golden.json"
CKPT = "/tmp/gtcrn_fetch/model_trained_on_dns3.tar"
ONNX = "/tmp/gtcrn_fetch/gtcrn.onnx"
N_FFT, HOP, WIN = 512, 256, 512
WINDOW = torch.hann_window(WIN).pow(0.5)
N_FRAMES = 8


def build_stream_model():
    # gtcrn_stream.py does `from einops import rearrange` at module top, but the
    # streaming path never calls rearrange (shuffle uses .view). einops is not
    # installed here, so provide a harmless stub to satisfy the import.
    import types
    if "einops" not in sys.modules:
        stub = types.ModuleType("einops")
        def _rearrange(*a, **k):
            raise NotImplementedError("einops.rearrange stub (unused in stream path)")
        stub.rearrange = _rearrange
        sys.modules["einops"] = stub

    from gtcrn_stream import StreamGTCRN
    from modules.convert import convert_to_stream

    # The DNS3 checkpoint's ['model'] IS the offline GTCRN state_dict; feed it to
    # convert_to_stream via a shim so we never import gtcrn.py (which needs einops).
    src_sd = torch.load(CKPT, map_location="cpu")["model"]

    class _Src:
        def state_dict(self_):
            return src_sd

    sm = StreamGTCRN()
    convert_to_stream(sm, _Src())
    sm.eval()
    return sm


def forward_capture(sm, spec, conv_cache, tra_cache, inter_cache):
    """Mirror of StreamGTCRN.forward, capturing each named sub-block."""
    cap = {}
    spec_ref = spec
    spec_real = spec[..., 0].permute(0, 2, 1)
    spec_imag = spec[..., 1].permute(0, 2, 1)
    spec_mag = torch.sqrt(spec_real ** 2 + spec_imag ** 2 + 1e-12)
    feat = torch.stack([spec_mag, spec_real, spec_imag], dim=1)  # (B,3,T,257)

    feat = sm.erb.bm(feat); cap["erb_bm"] = feat            # (B,3,T,129)
    feat = sm.sfe(feat);    cap["sfe"] = feat               # (B,9,T,129)

    feat, en_outs, conv_cache[0], tra_cache[0] = sm.encoder(feat, conv_cache[0], tra_cache[0])
    for i, e in enumerate(en_outs):
        cap[f"en_out{i}"] = e                               # 5 encoder-block outputs

    feat, inter_cache[0] = sm.dpgrnn1(feat, inter_cache[0]); cap["dpgrnn1"] = feat
    feat, inter_cache[1] = sm.dpgrnn2(feat, inter_cache[1]); cap["dpgrnn2"] = feat

    m_feat, conv_cache[1], tra_cache[1] = sm.decoder(feat, en_outs, conv_cache[1], tra_cache[1])
    cap["m_feat"] = m_feat                                  # (B,2,T,129)

    m = sm.erb.bs(m_feat); cap["mask"] = m                  # (B,2,T,257)

    spec_enh = sm.mask(m, spec_ref.permute(0, 3, 2, 1))
    spec_enh = spec_enh.permute(0, 3, 2, 1); cap["enh"] = spec_enh  # (B,257,T,2)
    return spec_enh, conv_cache, tra_cache, inter_cache, cap


def ort_session():
    import onnxruntime as ort
    so = ort.SessionOptions()
    so.intra_op_num_threads = 1
    so.inter_op_num_threads = 1
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    return ort.InferenceSession(ONNX, so, providers=["CPUExecutionProvider"])


def main():
    import onnxruntime as ort
    sm = build_stream_model()

    # white-noise STFT -> (1,257,T,2)
    x = torch.from_numpy(canonical_signals()["white-noise"][0])
    X = torch.stft(x, N_FFT, HOP, WIN, WINDOW, center=True, pad_mode="reflect",
                   normalized=False, onesided=True, return_complex=False)[None]

    block_order = ["erb_bm", "sfe", "en_out0", "en_out1", "en_out2", "en_out3",
                   "en_out4", "dpgrnn1", "dpgrnn2", "m_feat", "mask", "enh"]

    # torch stream
    conv_cache = torch.zeros(2, 1, 16, 16, 33)
    tra_cache = torch.zeros(2, 3, 1, 1, 16)
    inter_cache = torch.zeros(2, 1, 33, 16)
    frames = []
    torch_enh = []
    with torch.no_grad():
        for i in range(N_FRAMES):
            xi = X[:, :, i:i + 1, :]
            enh, conv_cache, tra_cache, inter_cache, cap = forward_capture(
                sm, xi, conv_cache, tra_cache, inter_cache)
            torch_enh.append(enh.numpy())
            fr = {"frame": i, "blocks": {}}
            for name in block_order:
                t = cap[name].detach().numpy().astype(np.float32)
                fr["blocks"][name] = {"shape": list(t.shape), "data": t.tolist()}
            frames.append(fr)
    torch_enh = np.concatenate(torch_enh, axis=2)  # (1,257,8,2)

    # ORT oracle over the same 8 frames
    sess = ort_session()
    cc = np.zeros([2, 1, 16, 16, 33], dtype=np.float32)
    tc = np.zeros([2, 3, 1, 1, 16], dtype=np.float32)
    ic = np.zeros([2, 1, 33, 16], dtype=np.float32)
    Xnp = X.numpy()
    ort_enh = []
    for i in range(N_FRAMES):
        enh, cc, tc, ic = sess.run(
            [], {"mix": Xnp[:, :, i:i + 1, :], "conv_cache": cc,
                 "tra_cache": tc, "inter_cache": ic})
        ort_enh.append(enh)
    ort_enh = np.concatenate(ort_enh, axis=2)  # (1,257,8,2)

    err = float(np.max(np.abs(torch_enh - ort_enh)))
    print(f"max_abs_err_torch_vs_ort (enh, {N_FRAMES} frames) = {err:.3e}")

    out = {
        "metadata": {
            "torch_version": torch.__version__,
            "onnxruntime_version": ort.__version__,
            "method": "torch StreamGTCRN (DNS3 weights, Version-2 "
                      "StreamConvTranspose2d) forward-capture",
            "signal": "white-noise",
            "n_frames": N_FRAMES,
            "stft": {"n_fft": N_FFT, "hop": HOP, "win_length": WIN,
                     "window": "sqrt_hann_periodic", "center": True,
                     "pad_mode": "reflect"},
            "block_order": block_order,
            "block_shapes": {name: list(frames[0]["blocks"][name]["shape"])
                             for name in block_order},
            "tolerance_note": "Version-2 StreamConvTranspose2d != ONNX real "
                              "ConvTranspose; torch intermediates are a ~1e-4 "
                              "tolerance reference, not bit-exact. Oracle = gtcrn.onnx.",
            "max_abs_err_torch_vs_ort_enh": err,
            "cross_validation": "final enh (torch Version-2) vs ONNX oracle in ORT "
                                "over the same 8 frames",
        },
        # torch intermediate blocks (tolerance reference)
        "frames": frames,
        # oracle-exact final enh from ORT, all 8 frames
        "ort_enh": {"shape": list(ort_enh.shape), "data": ort_enh.astype(np.float32).tolist()},
    }

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump(out, f, separators=(",", ":"))
    sz = os.path.getsize(OUT)
    print(f"wrote {OUT}  ({sz} bytes, {sz/1e6:.2f} MB)")


if __name__ == "__main__":
    main()
