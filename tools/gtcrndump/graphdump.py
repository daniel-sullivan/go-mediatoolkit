#!/usr/bin/env python3
"""Dump the full GTCRN streaming ONNX graph: I/O tensors, every node in
topological order with attributes, and every initializer's name/shape.

This is the porting spec generator. It resolves the hand-port traps:
GRU gate order + linear_before_reset, ConvTranspose pads/output_padding,
Unfold/Im2Col ordering, the LayerNorm decomposition (opset 11 has no
LayerNormalization op), PReLU slopes, and the three recurrent caches'
names/shapes/wiring.

Usage:
    ~/ml-venv/bin/python3 graphdump.py /path/to/gtcrn_simple.onnx
"""
import sys
import onnx
from onnx import numpy_helper
import numpy as np


def attr_val(a):
    t = a.type
    if t == onnx.AttributeProto.INT:
        return a.i
    if t == onnx.AttributeProto.FLOAT:
        return a.f
    if t == onnx.AttributeProto.STRING:
        return a.s.decode()
    if t == onnx.AttributeProto.INTS:
        return list(a.ints)
    if t == onnx.AttributeProto.FLOATS:
        return list(a.floats)
    if t == onnx.AttributeProto.TENSOR:
        arr = numpy_helper.to_array(a.t)
        return f"tensor{list(arr.shape)}={arr.flatten()[:8].tolist()}{'...' if arr.size > 8 else ''}"
    if t == onnx.AttributeProto.GRAPH:
        return f"<subgraph {a.g.name}, {len(a.g.node)} nodes>"
    return f"<attr type {t}>"


def shape_of(vi):
    dims = []
    for d in vi.type.tensor_type.shape.dim:
        dims.append(d.dim_value if d.HasField("dim_value") else (d.dim_param or "?"))
    return dims


def main():
    path = sys.argv[1]
    m = onnx.load(path)
    onnx.checker.check_model(m)
    g = m.graph

    print(f"### MODEL: {path}")
    print(f"ir_version={m.ir_version} opset={[ (o.domain or 'ai.onnx')+':'+str(o.version) for o in m.opset_import]} producer={m.producer_name} {m.producer_version}")
    print(f"nodes={len(g.node)} initializers={len(g.initializer)} inputs={len(g.input)} outputs={len(g.output)}")

    init_names = {i.name for i in g.initializer}

    print("\n### INPUTS")
    for vi in g.input:
        tag = " (initializer)" if vi.name in init_names else ""
        print(f"  {vi.name}: {shape_of(vi)}{tag}")
    print("\n### OUTPUTS")
    for vi in g.output:
        print(f"  {vi.name}: {shape_of(vi)}")

    print("\n### INITIALIZERS (name: shape dtype)")
    for i in sorted(g.initializer, key=lambda x: x.name):
        arr = numpy_helper.to_array(i)
        print(f"  {i.name}: {list(arr.shape)} {arr.dtype}")

    print("\n### NODES (topological)")
    for idx, n in enumerate(g.node):
        attrs = {a.name: attr_val(a) for a in n.attribute}
        # mark which inputs are initializers
        ins = [f"{x}{'*' if x in init_names else ''}" for x in n.input]
        print(f"[{idx:3d}] {n.op_type:18s} {n.name}")
        print(f"       in : {ins}")
        print(f"       out: {list(n.output)}")
        if attrs:
            print(f"       attr: {attrs}")

    # Focused trap reports
    print("\n### TRAP REPORT: GRU nodes")
    for n in g.node:
        if n.op_type == "GRU":
            attrs = {a.name: attr_val(a) for a in n.attribute}
            print(f"  {n.name}: attrs={attrs}")
            print(f"    inputs (X,W,R,B,seq,init_h): {list(n.input)}")
            for inp in n.input:
                if inp in init_names:
                    arr = numpy_helper.to_array(next(i for i in g.initializer if i.name == inp))
                    print(f"      {inp}: shape {list(arr.shape)}")

    print("\n### TRAP REPORT: Conv / ConvTranspose nodes")
    for n in g.node:
        if n.op_type in ("Conv", "ConvTranspose"):
            attrs = {a.name: attr_val(a) for a in n.attribute}
            wshape = None
            if len(n.input) > 1 and n.input[1] in init_names:
                wshape = list(numpy_helper.to_array(next(i for i in g.initializer if i.name == n.input[1])).shape)
            print(f"  {n.op_type} {n.name}: w={wshape} attrs={attrs}")

    print("\n### TRAP REPORT: PReLU slopes")
    for n in g.node:
        if n.op_type == "PRelu":
            slope = n.input[1]
            if slope in init_names:
                arr = numpy_helper.to_array(next(i for i in g.initializer if i.name == slope))
                print(f"  {n.name}: slope {slope} shape {list(arr.shape)} vals[:4]={arr.flatten()[:4].tolist()}")
            else:
                print(f"  {n.name}: slope {slope} (not initializer)")

    print("\n### TRAP REPORT: Unfold / Im2Col / Reshape / Transpose ops present")
    from collections import Counter
    c = Counter(n.op_type for n in g.node)
    for op, cnt in sorted(c.items()):
        print(f"  {op}: {cnt}")


if __name__ == "__main__":
    main()
