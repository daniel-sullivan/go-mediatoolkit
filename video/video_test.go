package video

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodecString(t *testing.T) {
	tests := []struct {
		c    Codec
		want string
	}{
		{H264, "h264"},
		{H265, "h265"},
		{VP9, "vp9"},
		{AV1, "av1"},
		{CodecUnknown, "unknown"},
		{Codec(99), "unknown"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.c.String())
	}
}

func TestPixelFormatPlanes(t *testing.T) {
	tests := []struct {
		p      PixelFormat
		planes int
		str    string
	}{
		{NV12, 2, "nv12"},
		{I420, 3, "i420"},
		{PixelFormatUnknown, 0, "unknown"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.planes, tc.p.Planes())
		assert.Equal(t, tc.str, tc.p.String())
	}
}
