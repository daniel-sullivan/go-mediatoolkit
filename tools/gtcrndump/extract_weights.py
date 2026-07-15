#!/usr/bin/env python3
"""Extract GTCRN weights from the vendored gtcrn.onnx oracle.

Emits two artifacts:
  1. gtcrn_weights.safetensors  — every float32 initializer of gtcrn.onnx,
     verbatim names (incl. opaque `onnx::Conv_*` / `onnx::GRU_*`). Embedded
     into the Go port and byte-verified against the oracle's initializers
     (the Silero precedent: runtime weights ARE the oracle's initializers).
  2. manifest.json — human map from each weight-consuming node (Conv,
     ConvTranspose, GRU, PRelu, BatchNormalization, MatMul) to its logical
     role + the initializer names/shapes it consumes, in graph order. This
     is the porting key that decodes the opaque names.

Usage:
    ~/ml-venv/bin/python3 extract_weights.py /tmp/gtcrn_fetch/gtcrn.onnx OUTDIR
"""
import json
import struct
import sys
import numpy as np
import onnx
from onnx import numpy_helper


def attrs_of(n):
    out = {}
    for a in n.attribute:
        if a.type == onnx.AttributeProto.INT:
            out[a.name] = a.i
        elif a.type == onnx.AttributeProto.INTS:
            out[a.name] = list(a.ints)
        elif a.type == onnx.AttributeProto.STRING:
            out[a.name] = a.s.decode()
    return out


def main():
    path, outdir = sys.argv[1], sys.argv[2]
    m = onnx.load(path)
    g = m.graph
    inits = {i.name: numpy_helper.to_array(i) for i in g.initializer}
    # Exact onnx dims per initializer (preserve rank-0 scalars as shape []
    # so the embedded shapes byte-match the oracle's initializers).
    onnx_dims = {i.name: list(i.dims) for i in g.initializer}
    f32 = {k: v for k, v in inits.items() if v.dtype == np.float32}

    # --- safetensors payload of every float32 initializer, sorted ---
    header, blobs, off = {}, [], 0
    for name in sorted(f32):
        a = np.ascontiguousarray(f32[name].astype("<f4"))
        raw = a.tobytes()
        header[name] = {"dtype": "F32", "shape": onnx_dims[name],
                        "data_offsets": [off, off + len(raw)]}
        blobs.append(raw)
        off += len(raw)
    hjson = json.dumps(header, sort_keys=True, separators=(",", ":")).encode()
    with open(f"{outdir}/gtcrn_weights.safetensors", "wb") as f:
        f.write(struct.pack("<Q", len(hjson)))
        f.write(hjson)
        for b in blobs:
            f.write(b)

    # --- manifest: weight-consuming nodes in graph order ---
    entries = []
    for idx, n in enumerate(g.node):
        if n.op_type not in ("Conv", "ConvTranspose", "GRU", "PRelu",
                             "BatchNormalization", "MatMul"):
            continue
        consumed = []
        for inp in n.input:
            if inp in inits:
                consumed.append({"name": inp, "shape": list(inits[inp].shape),
                                 "dtype": str(inits[inp].dtype)})
        entries.append({
            "order": idx, "op": n.op_type, "node": n.name,
            "attrs": attrs_of(n),
            "inputs": list(n.input), "outputs": list(n.output),
            "weights": consumed,
        })

    # ERB linear matrices + LayerNorm params by name (they carry PyTorch names or are MatMul consts)
    erb = {k: list(f32[k].shape) for k in f32 if "erb" in k.lower()}
    ln = {k: list(f32[k].shape) for k in f32 if "_ln." in k}
    fc = {k: list(f32[k].shape) for k in f32 if "_fc." in k}

    manifest = {
        "source": path,
        "n_float32_initializers": len(f32),
        "total_float32_params": int(sum(v.size for v in f32.values())),
        "weight_nodes": entries,
        "erb_like_initializers": erb,
        "layernorm_initializers": ln,
        "fc_initializers": fc,
    }
    with open(f"{outdir}/manifest.json", "w") as f:
        json.dump(manifest, f, indent=1)

    print(f"wrote {outdir}/gtcrn_weights.safetensors "
          f"({8 + len(hjson) + off} bytes, {len(f32)} tensors, "
          f"{manifest['total_float32_params']} params)")
    print(f"wrote {outdir}/manifest.json ({len(entries)} weight nodes)")
    print("ERB-like initializers:", erb)
    print("LayerNorm initializers:", ln)
    print("FC initializers (named):", fc)


if __name__ == "__main__":
    main()
