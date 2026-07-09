#!/usr/bin/env bash
# Run ALL AEC3 parity slices (smoke, fft, blocking, bandsplit, ...)
# against the locally fetched+built C++ oracle (see ../../oracle/fetch.sh
# and ../../oracle/VERSION). Every package under aec/internal/parity_tests/
# shares this one script: the oracle's include/library paths are
# host-dependent (per-user cache, not vendored) so they can't be static
# #cgo directives — this script resolves them once and passes them via
# CGO_CXXFLAGS/CGO_LDFLAGS to a single `go test` over all slices.
#
# Does NOT fetch the oracle itself — that's the separate `oracle:fetch`
# mise task (aec/oracle/fetch.sh) — so invoking this script (or the
# `parity` mise task that wraps it) never triggers a network fetch on
# its own. Without the oracle built, it fails fast with instructions
# rather than falling back to a plain (oracle-less) `go test`, since
# the whole point of this task is to run the real parity check.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORACLE_DIR="$(cd "${SCRIPT_DIR}/../../oracle" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
# shellcheck source=../../oracle/paths.sh
source "${ORACLE_DIR}/paths.sh"

if ! aec_oracle_ready; then
  echo "ERROR: AEC3 C++ oracle not built. Run:" >&2
  echo "  mise run //aec:oracle:fetch" >&2
  echo "(or directly: ${ORACLE_DIR}/fetch.sh)" >&2
  exit 1
fi

SRC_DIR="$(aec_oracle_src_dir)"
BUILD_DIR="$(aec_oracle_build_dir)"
ABSEIL_DIR="$(aec_oracle_abseil_dir)"

WEBRTC_LIB="$(find "${BUILD_DIR}" -name 'libwebrtc-audio-processing-*.a' | head -1)"
if [[ -z "${WEBRTC_LIB}" ]]; then
  echo "ERROR: no libwebrtc-audio-processing-*.a found under ${BUILD_DIR}" >&2
  exit 1
fi

# The 14-ish abseil component archives (base, strings, hash, ...) are
# discovered rather than hand-enumerated, since exactly which ones
# meson builds is an abseil-cpp-side implementation detail, not
# something this repo should hardcode and risk drifting from.
mapfile -t ABSEIL_LIBS < <(find "${BUILD_DIR}" -name 'libabsl_*.a' | sort)
if [[ "${#ABSEIL_LIBS[@]}" -eq 0 ]]; then
  echo "ERROR: no libabsl_*.a archives found under ${BUILD_DIR}" >&2
  exit 1
fi

LDFLAGS="-L$(dirname "${WEBRTC_LIB}") -l$(basename "${WEBRTC_LIB}" .a | sed 's/^lib//')"
for lib in "${ABSEIL_LIBS[@]}"; do
  LDFLAGS="${LDFLAGS} ${lib}"
done

case "$(uname -s)" in
  Darwin) LDFLAGS="${LDFLAGS} -framework CoreFoundation" ;;
esac

# Platform defines: the WebRTC headers hard-require these (e.g.
# rtc_base/platform_thread_types.h only defines PlatformThreadRef
# under a WEBRTC_POSIX/WEBRTC_WIN/... branch, so omitting them breaks
# shim compilation at the very first include). Mirror the -D set meson
# passes when compiling the library itself — verifiable against
# compile_commands.json in the cached build dir — so the shim
# translation units see the same configuration the archive was built
# with. (meson's remaining -Ds from that set — -DNDEBUG, the libc++
# hardening mode, -D_WINSOCKAPI_ — are optimization/Windows noise, not
# header-gating, and are deliberately not replicated here.)
DEFINES="-DWEBRTC_LIBRARY_IMPL -DWEBRTC_APM_DEBUG_DUMP=0"
case "$(uname -s)" in
  Darwin) DEFINES="${DEFINES} -DWEBRTC_POSIX -DWEBRTC_MAC" ;;
  Linux)  DEFINES="${DEFINES} -DWEBRTC_POSIX -DWEBRTC_LINUX" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) DEFINES="${DEFINES} -DWEBRTC_ARCH_ARM64" ;;
esac

# -ffp-contract=off: the same FP convention the oracle archive itself
# is built with (see oracle/fetch.sh) — shim code that touches floats
# must not FMA-fuse either, or it would diverge from the Go side's
# plain float32 ops.
export CGO_CXXFLAGS="${DEFINES} -ffp-contract=off -I${SRC_DIR}/webrtc -I${ABSEIL_DIR}"
export CGO_CXXFLAGS_ALLOW='.*'
export CGO_LDFLAGS="${LDFLAGS}"
export CGO_LDFLAGS_ALLOW='.*'

cd "${REPO_ROOT}"
# aec_strict: route the Go port's a*b+c expressions through
# separate-rounding helpers so they match the -ffp-contract=off oracle
# bit-for-bit (see aec/internal/aec3/fp_strict.go).
go test -tags=aec_oracle,aec_strict -v -count=1 ./aec/internal/parity_tests/...
