//go:build cgo && aacfdk

package psdecapply

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

func TestPsInitRotParity(t *testing.T) {
	r := rand.New(rand.NewSource(424242))
	const bufBytes = 256
	applied := 0
	for it := 0; it < 60000 && applied < 100; it++ {
		payload := make([]byte, bufBytes)
		nRand := 10 + r.Intn(10)
		for i := 0; i < nRand; i++ {
			payload[i] = byte(r.Intn(256))
		}
		validBits := bufBytes * 8
		ch11, ch12, ch21, ch22, cd11, cd12, cd21, cd22, cf := cInitRot(payload, validBits, 40)
		gh11, gh12, gh21, gh22, gd11, gd12, gd21, gd22, gf := sbr.PsInitRotRun(payload, uint32(bufBytes), uint32(validBits), 40)
		require.Equalf(t, cf, gf, "it=%d flag", it)
		if cf != 1 {
			continue
		}
		applied++
		require.Equalf(t, ch11, gh11, "it=%d H11", it)
		require.Equalf(t, ch12, gh12, "it=%d H12", it)
		require.Equalf(t, ch21, gh21, "it=%d H21", it)
		require.Equalf(t, ch22, gh22, "it=%d H22", it)
		require.Equalf(t, cd11, gd11, "it=%d D11", it)
		require.Equalf(t, cd12, gd12, "it=%d D12", it)
		require.Equalf(t, cd21, gd21, "it=%d D21", it)
		require.Equalf(t, cd22, gd22, "it=%d D22", it)
	}
	t.Logf("init-rot exercised on %d payloads", applied)
}
