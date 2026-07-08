package loudness

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
	"github.com/daniel-sullivan/go-mediatoolkit/containers/wav"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────
// Fixture plumbing for the EBU Tech 3341 cases 20–23 true-peak vectors.
//
// Those four cases are 4×-oversampled, splice-and-decimate signals whose exact
// construction the document does not fully specify (transcription ambiguity
// #5 — splice math, anti-alias filter, and fade are all unstated), so unlike
// the synthesizable cases 1–19 they are driven by the OFFICIAL EBU reference
// WAVs (seq-3341-20…23, 48 kHz / 24-bit stereo) from the EBU Loudness test set.
//
// These sequences are NOT vendored into the repository. EBU's "Use of EBU
// Audio Test Sequences" Terms of Use limit the material to internal technical
// testing and explicitly forbid copying/publishing/distributing it, so
// bundling the WAVs into this (public) repo is not permitted. Instead the
// fixtures are resolved at test time from a local cache; the test skips
// cleanly when they are absent so CI never depends on the network. This mirrors
// the opus black-box suite, which fetches its large external artifacts rather
// than committing them.
//
// Copyright of the reference sequences: © EBU (see readme.txt / the EBU Terms
// of Use inside the test-set archive).
// ─────────────────────────────────────────────────────────────────────────

const (
	// ebuTestsetURL is the official EBU Loudness test set (v05) archive. The
	// four seq-3341-20…23 WAVs live inside it (48 kHz / 24-bit stereo, ~0.9 MB
	// each). ~87 MB combined, so only the seq-3341-* entries are extracted.
	ebuTestsetURL = "https://tech.ebu.ch/files/live/sites/tech/files/shared/testmaterial/ebu-loudness-test-setv05.zip"

	// ebuTestsetDirEnv overrides where the extracted WAVs are looked for.
	// Point it at a directory holding the seq-3341-*-24bit.wav.wav files.
	ebuTestsetDirEnv = "EBU_LOUDNESS_TESTSET_DIR"

	// ebuDownloadEnv, when set to a non-empty value, opts in to a best-effort
	// download+extract of the official archive into the cache dir when the
	// fixtures are missing. Off by default so a normal `go test` (and CI)
	// never makes a network request — it simply skips the cases 20–23 subtests.
	ebuDownloadEnv = "EBU_LOUDNESS_DOWNLOAD"

	// ebuCacheSubdir is the cache-relative directory the WAVs are cached in.
	ebuCacheSubdir = "go-mediatoolkit-test/ebu-loudness-test-setv05"
)

// ebuTestsetDir resolves the directory the extracted seq-3341-* WAVs are read
// from: the EBU_LOUDNESS_TESTSET_DIR override if set, else a stable per-user
// cache directory (os.UserCacheDir, falling back to the temp dir).
func ebuTestsetDir() string {
	if d := os.Getenv(ebuTestsetDirEnv); d != "" {
		return d
	}
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, ebuCacheSubdir)
	}
	return filepath.Join(os.TempDir(), ebuCacheSubdir)
}

// ebuCaseFilename is the on-disk name of the case's WAV. The v05 archive ships
// these with a doubled ".wav.wav" extension (verbatim, as distributed) — it is
// preserved here rather than "corrected" so the cached names match the archive.
func ebuCaseFilename(caseNum int) string {
	return fmt.Sprintf("seq-3341-%d-24bit.wav.wav", caseNum)
}

// ebuFetchHint is the operator-facing instruction shown when a fixture is
// missing: how to obtain it, honouring EBU's Terms of Use.
func ebuFetchHint(dir string) string {
	return fmt.Sprintf(
		"place the EBU Loudness test set (v05) seq-3341-20…23 WAVs in %q "+
			"(or set %s to their directory), or re-run with %s=1 to download "+
			"them from %s. The EBU Terms of Use permit this material for internal "+
			"technical testing only — do not redistribute it.",
		dir, ebuTestsetDirEnv, ebuDownloadEnv, ebuTestsetURL)
}

// loadEBUTruePeakCase resolves, decodes, and returns the interleaved float64
// samples for EBU Tech 3341 case caseNum, plus its sample rate and channel
// count. It reads the WAV with the repo's own containers/wav + codec/pcm
// readers (24-bit handled by the pcm decoder). If the fixture is unavailable it
// t.Skips with fetch instructions rather than failing.
func loadEBUTruePeakCase(t *testing.T, caseNum int) (samples []float64, sampleRate, channels int) {
	t.Helper()
	dir := ebuTestsetDir()
	path := filepath.Join(dir, ebuCaseFilename(caseNum))

	if _, err := os.Stat(path); err != nil {
		if os.Getenv(ebuDownloadEnv) == "" {
			t.Skipf("EBU case %d fixture not cached: %s", caseNum, ebuFetchHint(dir))
		}
		if derr := ensureEBUTestset(dir); derr != nil {
			t.Skipf("EBU case %d download failed (%v): %s", caseNum, derr, ebuFetchHint(dir))
		}
		if _, err := os.Stat(path); err != nil {
			t.Skipf("EBU case %d still missing after download: %s", caseNum, ebuFetchHint(dir))
		}
	}

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	r, err := wav.NewReader(f)
	require.NoError(t, err)
	h := r.Header()

	dec, err := pcm.NewDecoder(r.Data(), h.SampleRate, h.Channels, h.SampleFormat)
	require.NoError(t, err)

	buf := make([]float64, 8192)
	for {
		got, err := dec.Read(buf)
		samples = append(samples, got.Data...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	return samples, h.SampleRate, h.Channels
}

// ensureEBUTestset downloads the official EBU archive and extracts just the
// seq-3341-* WAVs into dir. Best-effort and opt-in (guarded by the caller):
// any failure is returned so the test can skip. Only the seq-3341-* entries are
// written to disk; the rest of the ~87 MB archive is discarded.
func ensureEBUTestset(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodGet, ebuTestsetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-mediatoolkit-test")

	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", ebuTestsetURL, resp.StatusCode)
	}

	// archive/zip needs a seekable source (it reads the trailing central
	// directory), so stage the download in a temp file first.
	tmp, err := os.CreateTemp(dir, "download-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, zf := range zr.File {
		name := filepath.Base(zf.Name)
		if !strings.HasPrefix(name, "seq-3341-") || !strings.HasSuffix(name, ".wav") {
			continue
		}
		if err := extractZipEntry(zf, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// extractZipEntry writes a single zip entry to dst.
func extractZipEntry(zf *zip.File, dst string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
