#!/usr/bin/env bash
# Black-box parity check: build the xiph/opus v1.5.2 CLI (`opus_demo`)
# from pristine upstream sources with no local modifications, then
# drive both it and the pure-Go libopus port with identical synthetic
# PCM and assert the encoded bitstreams match byte-for-byte and the
# decoded PCM matches bit-for-bit.
#
# Nothing in the vendored C tree (libraries/opus/libopus/**) or in our
# cgo helpers participates — the C side is the upstream CLI invoked
# out-of-process.
#
# Safe to re-run. Reuses an existing clone+build if present.
set -euo pipefail

UPSTREAM_URL="https://gitlab.xiph.org/xiph/opus.git"
# Our vendored tree tracks upstream main (currently at 788cc89c "Init
# 'up' to fix clang-cl uninitialized warning"), with a handful of
# forward-declaration scaffolding lines added for the cgo amalgamation
# build. If the Go port is bit-exact against upstream main and the
# tag moves, pin UPSTREAM_REF to the commit we were last green at.
UPSTREAM_REF="${OPUS_UPSTREAM_REF:-788cc89ce4f2c42025d8c70ec1b4457dc89cd50f}"
UPSTREAM_DIR="${OPUS_UPSTREAM_DIR:-/tmp/opus-upstream/opus-main}"
# CFLAGS must match the Go port's target build configuration:
#   -ffp-contract=off + scalar codegen: needed for reproducible float rounding
#   -DENABLE_RES24=1:  selects the 24-bit internal resolution variant the
#                      Go port was written against (MAX_ENCODING_DEPTH=24).
#                      Upstream defaults to 16-bit internal res, which
#                      produces a different (but equally valid) bitstream.
#                      No source edits — this is just a compile-time flag.
CFLAGS_CLEAN="-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops -DENABLE_RES24=1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }

step "Pristine upstream clone at ${UPSTREAM_DIR} (ref ${UPSTREAM_REF})"
if [[ ! -d "${UPSTREAM_DIR}/.git" ]]; then
  mkdir -p "$(dirname "${UPSTREAM_DIR}")"
  git clone "${UPSTREAM_URL}" "${UPSTREAM_DIR}"
fi
( cd "${UPSTREAM_DIR}" && git fetch origin && git checkout --detach "${UPSTREAM_REF}" )

step "Verify tracked upstream files are pristine"
if ! ( cd "${UPSTREAM_DIR}" && git diff --quiet HEAD ); then
  ( cd "${UPSTREAM_DIR}" && git diff --stat HEAD ) >&2
  echo "ERROR: upstream clone at ${UPSTREAM_DIR} has local edits to tracked files — rerun will not be a black-box check." >&2
  exit 1
fi

DEMO_BIN="${UPSTREAM_DIR}/opus_demo"
step "Build upstream opus_demo with matching CFLAGS"
if [[ ! -x "${DEMO_BIN}" ]]; then
  # --disable-intrinsics: our Go port matches the scalar C path.
  # Upstream's autoconf enables NEON + DOTPROD on aarch64 by default,
  # and those kernels (celt_float2int16_neon, xcorr_kernel_neon_float,
  # etc.) produce different rounding / fusion than the scalar path.
  # Disable them to keep the upstream binary scalar-equivalent.
  ( cd "${UPSTREAM_DIR}" && \
    ./autogen.sh && \
    ./configure \
      --disable-shared --enable-static \
      --disable-doc \
      --disable-intrinsics \
      --disable-dred --disable-osce --disable-bwe --disable-deep-plc \
      CFLAGS="${CFLAGS_CLEAN}" && \
    make -j8 )
fi
test -x "${DEMO_BIN}"
"${DEMO_BIN}" 2>&1 | head -n 1 || true

step "Run black-box Go test driving opus_demo"
cd "${REPO_ROOT}"
export OPUS_DEMO_BIN="${DEMO_BIN}"
export CGO_CFLAGS="${CFLAGS_CLEAN}"
export CGO_CFLAGS_ALLOW='.*'

go test -tags 'opus_blackbox,opus_strict' -v -count=1 -timeout 300s ./libraries/opus/internal/parity_tests/blackbox/...

step "Black-box parity complete"
