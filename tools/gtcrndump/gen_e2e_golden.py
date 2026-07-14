"""
Generate END-TO-END golden vectors for the GTCRN Go port.

Interpreter: /Users/daniel/ml-venv/bin/python3
Run:  cd /Volumes/Storage/ClaudeProjects/go-mediatoolkit/tools/gtcrndump && \
      /Users/daniel/ml-venv/bin/python3 gen_e2e_golden.py

Emits: denoise/internal/parity_tests/gtcrn_ort/testdata/e2e_golden.json

Pipeline per signal:  torch.stft -> stream gtcrn.onnx frame-by-frame in ORT
(3 zero-init caches carried frame->frame) -> stack enh spectra -> torch.istft.

Signals: the 4 regenerable deterministic signals + the real upstream wav_mix.wav
("mix-clip", int16 -> float32 via /32768, NOT re-vendored). For the mix clip the
istft output is compared against upstream's wav_enh.wav (also int16/32768) on the
interior: max_abs_err_vs_upstream_enh validates the whole STFT/stream convention.
"""
import hashlib
import json
import os
import numpy as np
import torch
import onnxruntime as ort
from scipy.io import wavfile

from signals import canonical_signals, FS

OUT = "/Volumes/Storage/ClaudeProjects/go-mediatoolkit/denoise/internal/parity_tests/gtcrn_ort/testdata/e2e_golden.json"
ONNX = "/tmp/gtcrn_fetch/gtcrn.onnx"
MIX = "/tmp/gtcrn_fetch/wav_mix.wav"
ENH = "/tmp/gtcrn_fetch/wav_enh.wav"
N_FFT, HOP, WIN = 512, 256, 512
WINDOW = torch.hann_window(WIN).pow(0.5)

# Store full enh spectrum for the deterministic signals; for the long mix clip
# store only a head to keep the file within budget (error + caches kept full).
MIX_SPEC_HEAD_FRAMES = 32
MIX_PCM_HEAD_SAMPLES = 16000  # 1 s head; interior error metric vs upstream is stored full


def make_session():
    so = ort.SessionOptions()
    so.intra_op_num_threads = 1
    so.inter_op_num_threads = 1
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    return ort.InferenceSession(ONNX, so, providers=["CPUExecutionProvider"])


def stft(x_np):
    x = torch.from_numpy(x_np.astype(np.float32))
    return torch.stft(x, N_FFT, HOP, WIN, WINDOW, center=True, pad_mode="reflect",
                      normalized=False, onesided=True, return_complex=False)[None]  # (1,257,T,2)


def istft(spec_np, length=None):
    X = torch.from_numpy(spec_np.astype(np.float32))  # (1,257,T,2)
    Xc = torch.view_as_complex(X.contiguous())
    y = torch.istft(Xc, N_FFT, HOP, WIN, WINDOW, center=True, normalized=False,
                    onesided=True, length=length, return_complex=False)
    return y.numpy().astype(np.float32)[0]


def stream(sess, X):
    """X: (1,257,T,2) -> (enh_spec (1,257,T,2), cache_checkpoints dict)."""
    cc = np.zeros([2, 1, 16, 16, 33], dtype=np.float32)
    tc = np.zeros([2, 3, 1, 1, 16], dtype=np.float32)
    ic = np.zeros([2, 1, 33, 16], dtype=np.float32)
    Xnp = X.numpy()
    T = Xnp.shape[2]
    ckpt_frames = sorted(set([0, 4, 16, T - 1]))
    checkpoints = {}
    outs = []
    for i in range(T):
        enh, cc, tc, ic = sess.run(
            [], {"mix": Xnp[:, :, i:i + 1, :], "conv_cache": cc,
                 "tra_cache": tc, "inter_cache": ic})
        outs.append(enh)
        if i in ckpt_frames:
            checkpoints[str(i)] = {
                "conv_cache": {"shape": list(cc.shape), "data": cc.astype(np.float32).tolist()},
                "tra_cache": {"shape": list(tc.shape), "data": tc.astype(np.float32).tolist()},
                "inter_cache": {"shape": list(ic.shape), "data": ic.astype(np.float32).tolist()},
            }
    spec = np.concatenate(outs, axis=2).astype(np.float32)  # (1,257,T,2)
    return spec, checkpoints, ckpt_frames


def main():
    sess = make_session()
    sha = hashlib.sha256(open(ONNX, "rb").read()).hexdigest()

    signals = {}
    for name, (x, meta) in canonical_signals().items():
        signals[name] = (x, meta, False)

    # mix-clip: real upstream asset int16 -> float32 (soundfile convention: /32768)
    sr, mi = wavfile.read(MIX)
    assert sr == FS and mi.dtype == np.int16
    mix = (mi.astype(np.float32) / 32768.0)
    signals["mix-clip"] = (mix, {"generator": "upstream-asset", "source": "wav_mix.wav",
                                 "int16_to_float": "x/32768.0", "n": len(mix), "fs": FS}, True)

    out = {
        "metadata": {
            "torch_version": torch.__version__,
            "onnxruntime_version": ort.__version__,
            "model": "gtcrn.onnx",
            "model_sha256": sha,
            "ort_session_config": {
                "intra_op_num_threads": 1, "inter_op_num_threads": 1,
                "graph_optimization_level": "ORT_DISABLE_ALL",
                "providers": ["CPUExecutionProvider"]},
            "stft": {"n_fft": N_FFT, "hop": HOP, "win_length": WIN,
                     "window": "sqrt_hann_periodic", "center": True,
                     "pad_mode": "reflect", "normalized": False, "onesided": True},
            "istft": {"deterministic_signals": "length=len(signal)",
                      "mix-clip": "length=None (torch default -> hop*(T-1))"},
            "caches_init": "all zero: conv_cache(2,1,16,16,33) tra_cache(2,3,1,1,16) "
                           "inter_cache(2,1,33,16)",
            "cache_checkpoint_frames": "{0,4,16,last} (output caches AFTER that frame)",
            "mix_spec_head_frames": MIX_SPEC_HEAD_FRAMES,
            "signal_definitions": {n: m for n, (_, m, _) in signals.items()},
        },
        "signals": {},
    }

    mix_err = None
    for name, (x, meta, is_mix) in signals.items():
        X = stft(x)
        spec, checkpoints, ckpt_frames = stream(sess, X)  # (1,257,T,2)
        T = spec.shape[2]

        if is_mix:
            pcm = istft(spec, length=None)  # match upstream enh.wav length hop*(T-1)
            # compare to upstream enh reference on the interior
            sr2, ei = wavfile.read(ENH)
            enh_ref = (ei.astype(np.float32) / 32768.0)
            n = min(len(pcm), len(enh_ref))
            trim = N_FFT
            a = pcm[trim:n - trim]
            b = enh_ref[trim:n - trim]
            mix_err = float(np.max(np.abs(a - b)))
            print(f"mix-clip: T={T}, pcm_len={len(pcm)}, enh_ref_len={len(enh_ref)}, "
                  f"max_abs_err_vs_upstream_enh(interior)={mix_err:.3e}")
            assert mix_err < 1e-2, (
                f"PIPELINE FAIL: mix-clip vs upstream enh interior max_abs_err="
                f"{mix_err} >= 1e-2. STFT/streaming convention is off.")
            spec_store = spec[:, :, :MIX_SPEC_HEAD_FRAMES, :]
            spec_desc = f"HEAD first {MIX_SPEC_HEAD_FRAMES} of {T} frames"
            pcm_head = pcm[:MIX_PCM_HEAD_SAMPLES]
            entry = {
                "signal_meta": meta,
                "n_frames": T,
                "enh_pcm_full_len": len(pcm),
                "enh_pcm_head": {"shape": [len(pcm_head)], "data": pcm_head.tolist(),
                                 "note": f"HEAD first {len(pcm_head)} of {len(pcm)} samples"},
                "enh_spec_head": {"shape": list(spec_store.shape), "data": spec_store.tolist(),
                                  "note": spec_desc},
                "cache_checkpoints": checkpoints,
                "cache_checkpoint_frames": ckpt_frames,
                "max_abs_err_vs_upstream_enh": mix_err,
            }
        else:
            pcm = istft(spec, length=len(x))
            entry = {
                "signal_meta": meta,
                "n_frames": T,
                "enh_pcm": {"shape": [len(pcm)], "data": pcm.tolist()},
                "enh_spec": {"shape": list(spec.shape), "data": spec.tolist()},
                "cache_checkpoints": checkpoints,
                "cache_checkpoint_frames": ckpt_frames,
            }
            print(f"{name}: T={T}, pcm_len={len(pcm)}")
        out["signals"][name] = entry

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        json.dump(out, f, separators=(",", ":"))
    sz = os.path.getsize(OUT)
    print(f"\nwrote {OUT}  ({sz} bytes, {sz/1e6:.2f} MB)")
    print(f"max_abs_err_vs_upstream_enh (mix-clip) = {mix_err:.3e}")


if __name__ == "__main__":
    main()
