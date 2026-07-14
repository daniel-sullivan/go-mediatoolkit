#!/usr/bin/env python3
"""Cross-check: run gtcrn.onnx (un-simplified) and gtcrn_simple.onnx
through onnxruntime over the same streaming frame sequence and report
the max divergence. Decides which model to adopt as the parity oracle.

Hardened ORT session (single-threaded, all graph opts disabled) — the
same config the Go oracle will pin.
"""
import sys
import numpy as np
import onnxruntime as ort

FULL = "gtcrn.onnx"
SIMPLE = "gtcrn_simple.onnx"


def make_session(path):
    so = ort.SessionOptions()
    so.intra_op_num_threads = 1
    so.inter_op_num_threads = 1
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    return ort.InferenceSession(path, so, providers=["CPUExecutionProvider"])


def zero_caches():
    return {
        "conv_cache": np.zeros((2, 1, 16, 16, 33), dtype=np.float32),
        "tra_cache": np.zeros((2, 3, 1, 1, 16), dtype=np.float32),
        "inter_cache": np.zeros((2, 1, 33, 16), dtype=np.float32),
    }


def run_stream(sess, frames):
    caches = zero_caches()
    outs = []
    for f in frames:
        res = sess.run(
            ["enh", "conv_cache_out", "tra_cache_out", "inter_cache_out"],
            {"mix": f, **caches},
        )
        outs.append(res[0])
        caches = {"conv_cache": res[1], "tra_cache": res[2], "inter_cache": res[3]}
    return np.concatenate(outs, axis=2), caches


def main():
    rng = np.random.default_rng(0xC0FFEE)
    T = 40
    frames = [rng.standard_normal((1, 257, 1, 2)).astype(np.float32) * 0.1 for _ in range(T)]

    sf = make_session(FULL)
    ss = make_session(SIMPLE)
    print("ORT version:", ort.__version__)
    print("full inputs:", [(i.name, i.shape) for i in sf.get_inputs()])
    print("full outputs:", [(o.name, o.shape) for o in sf.get_outputs()])

    of, cf = run_stream(sf, frames)
    os_, cs = run_stream(ss, frames)

    d = np.abs(of - os_)
    print(f"\nenh: max|Δ|={d.max():.3e} mean|Δ|={d.mean():.3e}  (over {T} frames)")
    for k in cf:
        dk = np.abs(cf[k] - cs[k])
        print(f"cache {k}: max|Δ|={dk.max():.3e}")
    print(f"\nfull enh magnitude range: [{of.min():.4f}, {of.max():.4f}]")


if __name__ == "__main__":
    main()
