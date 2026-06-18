#!/usr/bin/env bash
# Black-box parity check: build the LAME 3.100 CLI (`lame`) from the
# pristine SourceForge release tarball with no local modifications, then
# drive both it and the pure-Go LAME-derived port (internal/nativemp3,
# fenced behind the mp3lame tag) with identical synthetic PCM and assert
# the encoded MP3 frames match byte-for-byte. The decode direction
# compares the pure-Go decoder against `lame --decode`.
#
# Nothing in the vendored C tree (libraries/mp3/liblame/**,
# libraries/mp3/libminimp3/**) or in our cgo helpers participates — the C
# side is the upstream `lame` CLI invoked out-of-process, built from a
# tarball that the vendored libmp3lame port tracks (LAME 3.100).
#
# Safe to re-run. Reuses an existing download+build if present.
set -euo pipefail

# LAME 3.100 — the exact release the pure-Go encoder is a 1:1 port of
# (see libraries/mp3/liblame/libmp3lame/version.h: 3.100.0). Pinned by
# SHA256 so a re-run is a black-box check against an unmodified upstream.
LAME_VERSION="${LAME_VERSION:-3.100}"
LAME_TARBALL="lame-${LAME_VERSION}.tar.gz"
LAME_URL="${LAME_URL:-https://downloads.sourceforge.net/lame/${LAME_TARBALL}}"
LAME_SHA256="${LAME_SHA256:-ddfe36cab873794038ae2c1210557ad34857a4b6bdc515785d1da9e175b1da1e}"
UPSTREAM_DIR="${MP3_UPSTREAM_DIR:-/tmp/lame-upstream}"
SRC_DIR="${UPSTREAM_DIR}/lame-${LAME_VERSION}"

# CFLAGS must match the Go port's target build configuration:
#   -ffp-contract=off + scalar codegen: needed for reproducible float
#   rounding vs the port's explicit FMA-free (mp3_strict) helpers. LAME is
#   FP-heavy; without these the C compiler fuses multiply-adds and
#   autovectorises, changing rounding behaviour vs the scalar path the
#   port targets.
CFLAGS_CLEAN="-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }

step "Pristine LAME ${LAME_VERSION} source at ${SRC_DIR}"
if [[ ! -d "${SRC_DIR}" ]]; then
  mkdir -p "${UPSTREAM_DIR}"
  TARBALL_PATH="${UPSTREAM_DIR}/${LAME_TARBALL}"
  if [[ ! -f "${TARBALL_PATH}" ]]; then
    step "Download ${LAME_URL}"
    curl -fSL "${LAME_URL}" -o "${TARBALL_PATH}"
  fi
  step "Verify tarball SHA256"
  GOT_SHA="$(shasum -a 256 "${TARBALL_PATH}" | awk '{print $1}')"
  if [[ "${GOT_SHA}" != "${LAME_SHA256}" ]]; then
    echo "ERROR: ${LAME_TARBALL} SHA256 mismatch" >&2
    echo "  expected ${LAME_SHA256}" >&2
    echo "  got      ${GOT_SHA}" >&2
    exit 1
  fi
  tar -xzf "${TARBALL_PATH}" -C "${UPSTREAM_DIR}"
fi
test -d "${SRC_DIR}"

LAME_BIN="${SRC_DIR}/frontend/lame"
step "Build upstream lame CLI with matching CFLAGS (scalar / FP-off, no nasm)"
if [[ ! -x "${LAME_BIN}" ]]; then
  # --disable-nasm: the Go port matches the scalar C path. LAME's nasm
  #   assembly (choose_table_asm, fft3dn, etc.) produces different
  #   rounding/results than the portable C. Disable it (and --disable-nasm
  #   is the only SIMD axis LAME's autoconf exposes; the SSE intrinsics in
  #   vector/ are nasm-gated too).
  # --enable-static --disable-shared: a self-contained CLI binary.
  # --disable-frontend? no — we NEED the frontend (the `lame` CLI itself).
  ( cd "${SRC_DIR}" && \
    ./configure \
      --disable-shared --enable-static \
      --disable-nasm \
      --disable-gtktest \
      CFLAGS="${CFLAGS_CLEAN}" && \
    make -j8 )
fi
test -x "${LAME_BIN}"
"${LAME_BIN}" --version 2>&1 | head -n 1 || true

step "Run black-box Go test driving the lame CLI"
cd "${REPO_ROOT}"
export LAME_BIN="${LAME_BIN}"
# The Go port runs under mp3_strict (FMA-free). No cgo is linked by the
# black-box test itself, but export the same scalar flags for consistency
# with the rest of the MP3 parity tooling.
export CGO_CFLAGS="${CFLAGS_CLEAN}"
export CGO_CFLAGS_ALLOW='.*'

go test -tags 'mp3_blackbox,mp3lame,mp3_strict' -v -count=1 -timeout 600s ./libraries/mp3/internal/parity_tests/blackbox/...

step "Black-box MP3 parity complete"
