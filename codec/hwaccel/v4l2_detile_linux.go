//go:build linux

// De-tiler for the Broadcom "SAND128" column-tiled NV12 the Pi-5
// rpi-hevc-dec hardware writes (V4L2_PIX_FMT_NV12_COL128, FourCC "NC12").
// The decoder's CAPTURE buffers are not linear: the image is split into
// vertical stripes 128 bytes wide, and within each stripe the rows are
// stored contiguously (row-major), stripes laid left-to-right. video.Frame
// requires linear NV12, so each decoded picture is de-tiled here.
//
// # SAND128 layout
//
// The image is split into vertical columns 128 bytes wide. Each column
// stores its rows contiguously (row-major within the column), columns laid
// left to right, so the column stride is colWidth * colHeight bytes. The
// rpivid driver packs the luma and chroma of an NV12 picture into a single
// run of columns of height colHeight (= the plane's reported bytesperline):
// the top H rows of every column are luma, and the next H/2 rows of the
// same columns are the interleaved (Cb,Cr) chroma. Verified bit-exact on a
// Pi-5 640x480 decode (colHeight = 720 = 480 luma + 240 chroma).
//
// For a column height colHeight, the byte of linear coordinate (x, y) in a
// region whose first row sits at rowOffset within the column is:
//
//	stripe   = x / 128
//	colInCol = x % 128
//	offset   = base + stripe*(128*colHeight) + (rowOffset + y)*128 + colInCol
//
// Luma uses rowOffset = 0; chroma uses rowOffset = codedH (it follows the
// luma rows in the same columns).

package hwaccel

// sandColWidth is the SAND128 column width in bytes.
const sandColWidth = 128

// detileSANDRegion de-tiles one SAND128 region (luma or chroma) into a
// tightly-packed linear plane of visW bytes per row and visH rows.
// colHeight is the shared column height (bytesperline); rowOffset is the
// row within each column where this region begins (0 for luma, codedH for
// chroma).
func detileSANDRegion(src []byte, visW, visH, colHeight, rowOffset int) []byte {
	dst := make([]byte, visW*visH)
	colStride := sandColWidth * colHeight
	for y := 0; y < visH; y++ {
		dstRow := y * visW
		rowInCol := (rowOffset + y) * sandColWidth
		x := 0
		for x < visW {
			stripe := x / sandColWidth
			colInStripe := x % sandColWidth
			base := stripe*colStride + rowInCol + colInStripe
			run := sandColWidth - colInStripe
			if x+run > visW {
				run = visW - x
			}
			if base+run > len(src) {
				run = len(src) - base
				if run <= 0 {
					break
				}
			}
			copy(dst[dstRow+x:dstRow+x+run], src[base:base+run])
			x += run
		}
	}
	return dst
}

// detileNV12 de-tiles a SAND128 NV12 picture into linear NV12 planes sized
// to the visible width/height. yTiled and cTiled may alias the same buffer
// (the single-plane packing) — only the rowOffset differs: luma starts at
// row 0, chroma at row codedH within each 128-wide column. colHeight is the
// driver-reported column height (bytesperline) shared by both regions.
func detileNV12(yTiled, cTiled []byte, visW, visH, colHeight, codedH int) (y, c []byte) {
	y = detileSANDRegion(yTiled, visW, visH, colHeight, 0)
	// Chroma rows live at column-row offset codedH in the same columns; when
	// cTiled is the single combined buffer the rowOffset places them, when
	// it is a separate plane the rowOffset is 0 within that plane.
	chromaRowOff := codedH
	if len(cTiled) != len(yTiled) {
		chromaRowOff = 0
	}
	c = detileSANDRegion(cTiled, visW, visH/2, colHeight, chromaRowOff)
	return y, c
}
