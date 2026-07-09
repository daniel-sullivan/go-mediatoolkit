#!/usr/bin/env bash
# Fetch + meson-build the AEC3 C++ oracle: freedesktop's
# webrtc-audio-processing v2.1 (BSD-3 + PATENTS grant), which vendors
# WebRTC's AEC3 echo canceller. NOT vendored into this repo — see
# ./VERSION for why (~25kLOC C++ + an abseil-cpp dependency, well past
# this repo's vendor-small-C convention) and paths.sh for the pinned
# URLs/hashes and cache layout this script and the smoke parity slice
# both resolve against.
#
# Idempotent and safe to re-run: each step is skipped if its output
# already exists, so a second invocation after a successful build is a
# fast no-op (ninja itself no-ops on an unchanged tree).
#
# Usage:
#   ./fetch.sh
#   AEC_ORACLE_DIR=/some/other/cache ./fetch.sh   # override cache root
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=paths.sh
source "${SCRIPT_DIR}/paths.sh"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }

CACHE_ROOT="$(aec_oracle_cache_root)"
ARCHIVE="$(aec_oracle_archive)"
SRC_DIR="$(aec_oracle_src_dir)"
BUILD_DIR="$(aec_oracle_build_dir)"

mkdir -p "${CACHE_ROOT}"

step "Download webrtc-audio-processing ${AEC_WEBRTC_APM_VERSION} (cache: ${CACHE_ROOT})"
verify_sha256() {
  local file="$1" want="$2" got
  if command -v shasum >/dev/null 2>&1; then
    got="$(shasum -a 256 "${file}" | awk '{print $1}')"
  else
    got="$(sha256sum "${file}" | awk '{print $1}')"
  fi
  [[ "${got}" == "${want}" ]]
}

if [[ -f "${ARCHIVE}" ]] && verify_sha256 "${ARCHIVE}" "${AEC_WEBRTC_APM_SHA256}"; then
  echo "already downloaded + verified: ${ARCHIVE}"
else
  rm -f "${ARCHIVE}"
  curl -sL -o "${ARCHIVE}" "${AEC_WEBRTC_APM_URL}"
  if ! verify_sha256 "${ARCHIVE}" "${AEC_WEBRTC_APM_SHA256}"; then
    echo "ERROR: sha256 mismatch for ${ARCHIVE}" >&2
    echo "  expected: ${AEC_WEBRTC_APM_SHA256}" >&2
    if command -v shasum >/dev/null 2>&1; then
      echo "  got:      $(shasum -a 256 "${ARCHIVE}" | awk '{print $1}')" >&2
    fi
    rm -f "${ARCHIVE}"
    exit 1
  fi
  echo "downloaded + verified: ${ARCHIVE}"
fi

step "Extract to ${SRC_DIR}"
if [[ -f "${SRC_DIR}/meson.build" ]]; then
  echo "already extracted"
else
  mkdir -p "$(dirname "${SRC_DIR}")"
  tmp_extract="$(mktemp -d)"
  tar xf "${ARCHIVE}" -C "${tmp_extract}"
  rm -rf "${SRC_DIR}"
  mv "${tmp_extract}/webrtc-audio-processing-${AEC_WEBRTC_APM_VERSION}" "${SRC_DIR}"
  rmdir "${tmp_extract}"
fi

step "Ensure meson + ninja are available"
aec_oracle_ensure_meson_ninja

step "meson setup (static lib, abseil forced to the pinned subproject, NEON disabled)"
# --force-fallback-for=abseil-cpp: ignore any system/pkg-config abseil
#   (this host may have a newer one installed for unrelated reasons —
#   verified during development: pkg-config found abseil 20260107 on
#   the dev machine, six years newer than the pin) and always build
#   the exact ${AEC_ABSEIL_VERSION} the release was tested against.
# -Dneon=disabled: matches the opus black-box oracle's
#   --disable-intrinsics choice — this build targets scalar-equivalent
#   codegen so a future bit-exact Go-port parity oracle compares
#   apples-to-apples. Irrelevant to *this* smoke slice's own
#   determinism (a single build's output is deterministic regardless
#   of SIMD use — no threads, no unseeded randomness), but avoids
#   baking in a NEON-vs-scalar mismatch we'd have to redo later.
# -ffp-contract=off (c_args + cpp_args): clang at -O3 defaults to
#   -ffp-contract=on and fuses a*b+c into FMA instructions on arm64,
#   which rounds once instead of twice and so diverges bit-wise from
#   the Go port's plain float32 ops. Same convention as this repo's
#   opus and flac parity oracles: the oracle must be compiled with
#   contraction off for bit-exact comparison. A build configured
#   without this flag is detected below and reconfigured in place.
MESON_ARGS=(
  --default-library=static
  --buildtype=release
  -Dneon=disabled
  --force-fallback-for=abseil-cpp
  -Dc_args=-ffp-contract=off
  -Dcpp_args=-ffp-contract=off
)
if [[ ! -f "${BUILD_DIR}/build.ninja" ]]; then
  ( cd "${SRC_DIR}" && meson setup build "${MESON_ARGS[@]}" )
elif ! grep -q 'ffp-contract=off' "${BUILD_DIR}/meson-info/intro-buildoptions.json" 2>/dev/null; then
  echo "existing build lacks -ffp-contract=off; reconfiguring"
  ( cd "${SRC_DIR}" && meson setup build --reconfigure "${MESON_ARGS[@]}" )
else
  echo "build already configured"
fi

step "ninja build"
ninja -C "${BUILD_DIR}"

step "Verify the oracle static lib is present"
if ! aec_oracle_ready; then
  echo "ERROR: build completed but expected static lib is missing:" >&2
  echo "  $(aec_oracle_build_dir)/webrtc/modules/audio_processing/libwebrtc-audio-processing-2.a" >&2
  exit 1
fi
echo "oracle ready: ${BUILD_DIR}"
