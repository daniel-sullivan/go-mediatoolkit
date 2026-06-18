#!/usr/bin/env bash
# Black-box parity check: build the xiph/flac 1.5.0 reference CLI (`flac`)
# from pristine upstream sources with no local modifications, then drive
# both it and the pure-Go libFLAC port with identical synthetic PCM and
# assert the encoded .flac bitstreams match byte-for-byte and the decoded
# PCM matches bit-for-bit.
#
# Nothing in the vendored C tree (libraries/flac/libflac/**) or in our
# cgo oracle helpers participates — the C side is the upstream CLI invoked
# out-of-process.
#
# Safe to re-run. Reuses an existing clone+build if present.
set -euo pipefail

UPSTREAM_URL="https://github.com/xiph/flac.git"
# Our vendored tree (libraries/flac/libflac) is reference libFLAC 1.5.0
# (config.h: PACKAGE_VERSION "1.5.0"; format.c vendor "reference libFLAC
# 1.5.0 20250211"). Pin the black-box reference to the matching release
# tag. If the vendored version moves, bump FLAC_UPSTREAM_REF.
UPSTREAM_REF="${FLAC_UPSTREAM_REF:-1.5.0}"
UPSTREAM_DIR="${FLAC_UPSTREAM_DIR:-/tmp/flac-upstream/flac-1.5.0}"
# CFLAGS must match the Go port's target build configuration:
#   -ffp-contract=off + scalar codegen: needed for reproducible float
#     rounding in the LPC/window FP paths the encoder uses.
#   -DFLAC__NO_ASM: the vendored config.h leaves FLAC__HAS_X86INTRIN /
#     FLAC__HAS_NEONINTRIN undefined and builds the scalar kernels only,
#     which is the path the Go port was translated from. Belt-and-braces
#     with --disable-asm-optimizations below.
CFLAGS_CLEAN="-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops -DFLAC__NO_ASM"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }

step "Pristine upstream clone at ${UPSTREAM_DIR} (ref ${UPSTREAM_REF})"
if [[ ! -d "${UPSTREAM_DIR}/.git" ]]; then
  mkdir -p "$(dirname "${UPSTREAM_DIR}")"
  git clone "${UPSTREAM_URL}" "${UPSTREAM_DIR}"
fi
( cd "${UPSTREAM_DIR}" && git fetch --tags origin && git checkout --detach "${UPSTREAM_REF}" )

step "Verify tracked upstream files are pristine"
if ! ( cd "${UPSTREAM_DIR}" && git diff --quiet HEAD ); then
  ( cd "${UPSTREAM_DIR}" && git diff --stat HEAD ) >&2
  echo "ERROR: upstream clone at ${UPSTREAM_DIR} has local edits to tracked files — rerun will not be a black-box check." >&2
  exit 1
fi

FLAC_BIN="${UPSTREAM_DIR}/src/flac/flac"
step "Build upstream flac CLI with matching CFLAGS"
if [[ ! -x "${FLAC_BIN}" ]]; then
  # --disable-asm-optimizations: our Go port matches the scalar C path.
  #   Upstream's autoconf enables NEON / SSE kernels by default, which
  #   produce different rounding / fusion than the scalar path the Go
  #   port targets. Disable them (plus -DFLAC__NO_ASM in CFLAGS) so the
  #   upstream binary is scalar-equivalent to the vendored config.
  # --disable-ogg: we compare native FLAC framing, not the Ogg transport;
  #   avoids a libogg build dependency.
  ( cd "${UPSTREAM_DIR}" && \
    ./autogen.sh && \
    ./configure \
      --disable-shared --enable-static \
      --disable-ogg \
      --disable-asm-optimizations \
      --disable-doxygen-docs --disable-examples \
      CFLAGS="${CFLAGS_CLEAN}" && \
    make -j8 )
fi
test -x "${FLAC_BIN}"
"${FLAC_BIN}" --version

step "Run black-box Go test driving the flac CLI"
cd "${REPO_ROOT}"
export FLAC_BIN="${FLAC_BIN}"
export CGO_CFLAGS="${CFLAGS_CLEAN}"
export CGO_CFLAGS_ALLOW='.*'

go test -tags 'flac_blackbox,flac_strict' -v -count=1 -timeout 300s ./libraries/flac/internal/parity_tests/blackbox/...

step "Black-box parity complete"
