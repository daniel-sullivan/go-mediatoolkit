"""
Generate STFT/ISTFT independent golden vectors for the GTCRN Go port.

Interpreter: /Users/daniel/ml-venv/bin/python3
Run:  cd /Volumes/Storage/ClaudeProjects/go-mediatoolkit/tools/gtcrndump && \
      /Users/daniel/ml-venv/bin/python3 gen_stft_golden.py

Emits: denoise/internal/gtcrn/testdata/stft_golden.json
Pins the EXACT torch.stft / torch.istft framing convention (sqrt of periodic
Hann, n_fft=512, hop=256, center=True/reflect, onesided, not-normalized) with
NO model involved. This is the #1 silent-divergence guard for the port.
"""
import json
import os
import numpy as np
import torch

from signals import canonical_signals, FS

OUT = "/Volumes/Storage/ClaudeProjects/go-mediatoolkit/denoise/internal/gtcrn/testdata/stft_golden.json"

N_FFT, HOP, WIN = 512, 256, 512
WINDOW = torch.hann_window(WIN).pow(0.5)  # sqrt of PERIODIC Hann (torch default periodic=True)


def stft(x_np):
    x = torch.from_numpy(x_np.astype(np.float32))
    X = torch.stft(x, N_FFT, HOP, WIN, WINDOW, center=True,
                   pad_mode="reflect", normalized=False, onesided=True,
                   return_complex=False)  # (F=257, T, 2)
    return X


def istft(X, length=None):
    # X is real (F,T,2); torch>=2.x istft requires complex input.
    Xc = torch.view_as_complex(X.contiguous())
    y = torch.istft(Xc, N_FFT, HOP, WIN, WINDOW, center=True,
                    normalized=False, onesided=True, length=length,
                    return_complex=False)
    return y


def main():
    sigs = canonical_signals()
    out = {
        "metadata": {
            "torch_version": torch.__version__,
            "n_fft": N_FFT, "hop": HOP, "win_length": WIN,
            "window": "sqrt_hann_periodic", "center": True,
            "pad_mode": "reflect", "normalized": False, "onesided": True,
            "fs": FS,
            "description": "torch.stft/istft framing golden, no model. "
                           "roundtrip error measured on interior (n_fft trimmed each end).",
        },
        "signals": {},
    }

    max_rt = 0.0
    for name, (x, meta) in sigs.items():
        X = stft(x)                       # (257, T, 2)
        T = X.shape[1]
        y = istft(X, length=len(x))       # reconstruct to original length
        y_np = y.detach().numpy().astype(np.float32)

        # interior roundtrip error: trim n_fft from each end to avoid edge/COLA taper
        trim = N_FFT
        a = x[trim:len(x) - trim]
        b = y_np[trim:len(y_np) - trim]
        rt = float(np.max(np.abs(a - b))) if len(a) > 0 else float("nan")
        max_rt = max(max_rt, rt)
        assert rt < 1e-4, (
            f"ROUNDTRIP FAIL for {name}: interior max|istft(stft(x))-x|={rt} >= 1e-4. "
            "sqrt-Hann window / center convention is wrong.")

        Xr = X[:, :, 0].detach().numpy().astype(np.float32)
        Xi = X[:, :, 1].detach().numpy().astype(np.float32)
        out["signals"][name] = {
            "signal_meta": meta,
            "input": {"shape": [len(x)], "data": x.astype(np.float32).tolist()},
            "stft_real": {"shape": [257, T], "data": Xr.tolist()},
            "stft_imag": {"shape": [257, T], "data": Xi.tolist()},
            "stft_shape_FT2": [257, T, 2],
            "istft": {"shape": [len(y_np)], "data": y_np.tolist()},
            "roundtrip_max_abs_err": rt,
            "n_frames": T,
        }
        print(f"{name}: T={T} frames, roundtrip_interior_max_abs_err={rt:.3e}")

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump(out, f, separators=(",", ":"))
    sz = os.path.getsize(OUT)
    print(f"\nwrote {OUT}  ({sz} bytes, {sz/1e6:.2f} MB)")
    print(f"overall interior roundtrip max_abs_err = {max_rt:.3e}")


if __name__ == "__main__":
    main()
