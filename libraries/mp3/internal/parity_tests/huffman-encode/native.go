// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package huffmanencode

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 bit writer through helpers shaped
// like the cgo oracle wrappers, so parity_test.go can compare the two sides
// through a uniform surface. These import only internal/nativemp3 (never
// libraries/mp3).

// nativeEnc holds a nativemp3.LameInternalFlags whose Bs.Buf is sized to match the C
// oracle's malloc'd output buffer.
type nativeEnc struct {
	gfc     nativemp3.LameInternalFlags
	bufSize int
}

func newNativeEnc(bufSize int) *nativeEnc {
	n := &nativeEnc{bufSize: bufSize}
	n.gfc.Bs.Buf = make([]byte, bufSize)
	n.gfc.Bs.BufSize = bufSize
	return n
}

func (n *nativeEnc) setSideinfoLen(c int) { n.gfc.Cfg.SideinfoLen = c }

func (n *nativeEnc) primeHeader(slot, writeTiming int, buf []byte) {
	n.gfc.SvEnc.Header[slot].WriteTiming = writeTiming
	copy(n.gfc.SvEnc.Header[slot].Buf[:], buf)
}

func (n *nativeEnc) setWPtr(w int) { n.gfc.SvEnc.WPtr = w }

// disarmHeaders sets every header slot's WriteTiming to a sentinel the running
// Totbit never reaches, mirroring mp3parity_enc_disarm_headers, so an unprimed
// slot never triggers a spurious splice in PutBits2.
func (n *nativeEnc) disarmHeaders(sentinel int) {
	for i := range n.gfc.SvEnc.Header {
		n.gfc.SvEnc.Header[i].WriteTiming = sentinel
	}
}

func (n *nativeEnc) putBits2(val, j int)         { n.gfc.PutBits2(val, j) }
func (n *nativeEnc) putBitsNoHeaders(val, j int) { n.gfc.PutBitsNoHeaders(val, j) }
func (n *nativeEnc) putHeaderBits()              { n.gfc.PutHeaderBits() }

func (n *nativeEnc) totbit() int     { return n.gfc.Bs.Totbit }
func (n *nativeEnc) bufByteIdx() int { return n.gfc.Bs.BufByteIdx }
func (n *nativeEnc) bufBitIdx() int  { return n.gfc.Bs.BufBitIdx }
func (n *nativeEnc) wPtr() int       { return n.gfc.SvEnc.WPtr }

func (n *nativeEnc) bytes(c int) []byte {
	return append([]byte(nil), n.gfc.Bs.Buf[:c]...)
}
