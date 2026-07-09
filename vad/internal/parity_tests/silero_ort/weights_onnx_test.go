//go:build cgo

package silero_ort

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

// TestWeightsMatchONNXInitializers asserts the promise VERSION makes:
// the embedded safetensors' float32 payloads are byte-identical to the
// onnx oracle's initializers, tensor for tensor (we converted them
// ourselves precisely because the upstream safetensors is a different
// checkpoint). The onnx side is read with a minimal protobuf walk —
// no onnxruntime needed, so this runs on every cgo test pass.
func TestWeightsMatchONNXInitializers(t *testing.T) {
	raw, err := os.ReadFile(modelPath)
	require.NoError(t, err)
	inits, err := parseONNXInitializers(raw)
	require.NoError(t, err)

	tensors, err := silero.Tensors()
	require.NoError(t, err)
	require.Len(t, inits, len(tensors), "initializer count")

	for name, tn := range tensors {
		init, ok := inits[name]
		require.True(t, ok, "onnx model has no initializer %q", name)

		wantShape := make([]int, len(init.dims))
		for i, d := range init.dims {
			wantShape[i] = int(d)
		}
		assert.Equal(t, wantShape, tn.Shape, "%s shape", name)

		require.Len(t, init.rawData, 4*len(tn.Data), "%s byte length", name)
		for i, v := range tn.Data {
			if math.Float32bits(v) != binary.LittleEndian.Uint32(init.rawData[4*i:]) {
				t.Fatalf("%s: element %d differs (safetensors %x, onnx %x)",
					name, i, math.Float32bits(v), binary.LittleEndian.Uint32(init.rawData[4*i:]))
			}
		}
	}
}

// onnxTensor is the subset of onnx TensorProto this check needs.
type onnxTensor struct {
	dims    []int64
	rawData []byte
}

// parseONNXInitializers walks the onnx protobuf just far enough to
// collect GraphProto.initializer (ModelProto field 7 → GraphProto
// field 5 → TensorProto fields 1 dims / 2 data_type / 8 name /
// 9 raw_data). Wire format only — varints and length-delimited
// records; everything else is skipped by wire type.
func parseONNXInitializers(model []byte) (map[string]onnxTensor, error) {
	graph, err := protoField(model, 7)
	if err != nil {
		return nil, fmt.Errorf("silero_ort: ModelProto.graph: %w", err)
	}
	inits := make(map[string]onnxTensor)
	if err := protoEach(graph, 5, func(msg []byte) error {
		var tn onnxTensor
		var name string
		var dataType uint64 = 0
		if err := protoWalk(msg, func(field uint64, wire int, varint uint64, blob []byte) error {
			switch {
			case field == 1 && wire == 0: // dims, unpacked varint
				tn.dims = append(tn.dims, int64(varint))
			case field == 1 && wire == 2: // dims, packed
				for len(blob) > 0 {
					v, n := binary.Uvarint(blob)
					if n <= 0 {
						return fmt.Errorf("bad packed dims varint")
					}
					tn.dims = append(tn.dims, int64(v))
					blob = blob[n:]
				}
			case field == 2 && wire == 0:
				dataType = varint
			case field == 8 && wire == 2:
				name = string(blob)
			case field == 9 && wire == 2:
				tn.rawData = blob
			}
			return nil
		}); err != nil {
			return err
		}
		if dataType != 1 { // onnx.TensorProto.FLOAT
			return fmt.Errorf("initializer %q has data_type %d, want FLOAT(1)", name, dataType)
		}
		if name == "" || tn.rawData == nil {
			return fmt.Errorf("initializer missing name or raw_data (name=%q)", name)
		}
		inits[name] = tn
		return nil
	}); err != nil {
		return nil, err
	}
	return inits, nil
}

// protoWalk decodes one protobuf message, invoking fn per field with
// its number, wire type, and either the varint value (wire 0) or the
// length-delimited payload (wire 2). Wire types 1/5 are skipped.
func protoWalk(msg []byte, fn func(field uint64, wire int, varint uint64, blob []byte) error) error {
	for len(msg) > 0 {
		tag, n := binary.Uvarint(msg)
		if n <= 0 {
			return fmt.Errorf("bad field tag")
		}
		msg = msg[n:]
		field, wire := tag>>3, int(tag&7)
		switch wire {
		case 0:
			v, n := binary.Uvarint(msg)
			if n <= 0 {
				return fmt.Errorf("bad varint in field %d", field)
			}
			msg = msg[n:]
			if err := fn(field, 0, v, nil); err != nil {
				return err
			}
		case 1:
			if len(msg) < 8 {
				return fmt.Errorf("truncated fixed64 in field %d", field)
			}
			msg = msg[8:]
		case 2:
			l, n := binary.Uvarint(msg)
			if n <= 0 || uint64(len(msg)-n) < l {
				return fmt.Errorf("bad length-delimited field %d", field)
			}
			blob := msg[n : uint64(n)+l]
			msg = msg[uint64(n)+l:]
			if err := fn(field, 2, 0, blob); err != nil {
				return err
			}
		case 5:
			if len(msg) < 4 {
				return fmt.Errorf("truncated fixed32 in field %d", field)
			}
			msg = msg[4:]
		default:
			return fmt.Errorf("unsupported wire type %d (field %d)", wire, field)
		}
	}
	return nil
}

// protoField returns the payload of the FIRST length-delimited
// occurrence of field in msg.
func protoField(msg []byte, field uint64) ([]byte, error) {
	var out []byte
	err := protoWalk(msg, func(f uint64, wire int, _ uint64, blob []byte) error {
		if f == field && wire == 2 && out == nil {
			out = blob
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("field %d not found", field)
	}
	return out, nil
}

// protoEach invokes fn for every length-delimited occurrence of field.
func protoEach(msg []byte, field uint64, fn func(msg []byte) error) error {
	return protoWalk(msg, func(f uint64, wire int, _ uint64, blob []byte) error {
		if f == field && wire == 2 {
			return fn(blob)
		}
		return nil
	})
}
