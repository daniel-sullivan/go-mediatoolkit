#!/usr/bin/env bash
# Shared version pins + cache-path resolution for the AEC3 C++ oracle.
# Sourced (not executed) by fetch.sh and by the smoke parity slice's
# run.sh, so the two never let their pins or layout drift apart.
#
# Nothing here is vendored into the repo (see aec/oracle/VERSION for
# the rationale): freedesktop's webrtc-audio-processing is ~25kLOC of
# C++ plus an abseil-cpp dependency, well beyond this repo's
# vendor-small-C convention (contrast libopus/libFLAC/libebur128/
# libminimp3, all a few thousand lines of plain C checked into the
# tree). Instead this script fetches a pinned release tarball into a
# per-user cache directory and builds it there, mirroring the opus
# black-box suite's pattern of fetching large external build artifacts
# on demand rather than committing them.
set -euo pipefail

# webrtc-audio-processing v2.1 (tracks WebRTC M131). Release tarball +
# sha256 verified by hand against a byte-for-byte re-download
# (2026-07-08); see VERSION for the full provenance note.
AEC_WEBRTC_APM_VERSION="2.1"
AEC_WEBRTC_APM_URL="http://freedesktop.org/software/pulseaudio/webrtc-audio-processing/webrtc-audio-processing-${AEC_WEBRTC_APM_VERSION}.tar.xz"
AEC_WEBRTC_APM_SHA256="ae9302824b2038d394f10213cab05312c564a038434269f11dbf68f511f9f9fe"

# abseil-cpp pin taken from the release's own
# subprojects/abseil-cpp.wrap (wrapdb entry abseil-cpp_20240722.0-3).
# Forced via `--force-fallback-for=abseil-cpp` at meson-setup time so
# the build always uses this exact abseil regardless of what (if
# anything) is already installed system-wide via pkg-config on the
# build host — a stray newer/older system abseil must not silently
# change what the oracle links against.
AEC_ABSEIL_VERSION="20240722.0"

# aec_oracle_cache_root: the per-user cache directory everything below
# lives under. AEC_ORACLE_DIR overrides it wholesale (parallels
# OPUS_UPSTREAM_DIR / EBU_LOUDNESS_TESTSET_DIR elsewhere in this repo).
aec_oracle_cache_root() {
  if [[ -n "${AEC_ORACLE_DIR:-}" ]]; then
    echo "${AEC_ORACLE_DIR}"
    return
  fi
  local cache_base="${XDG_CACHE_HOME:-${HOME}/Library/Caches}"
  echo "${cache_base}/go-mediatoolkit-oracle/webrtc-apm-v${AEC_WEBRTC_APM_VERSION}"
}

# aec_oracle_archive: where the downloaded tarball is kept (retained
# after extraction so re-runs can verify-and-skip instead of
# re-downloading).
aec_oracle_archive() {
  echo "$(aec_oracle_cache_root)/webrtc-audio-processing-${AEC_WEBRTC_APM_VERSION}.tar.xz"
}

# aec_oracle_src_dir: the extracted upstream source tree (meson.build,
# webrtc/, subprojects/ all live directly under here).
aec_oracle_src_dir() {
  echo "$(aec_oracle_cache_root)/src/webrtc-audio-processing-${AEC_WEBRTC_APM_VERSION}"
}

# aec_oracle_build_dir: the meson/ninja build directory (holds the
# compiled static libs the cgo shim links against).
aec_oracle_build_dir() {
  echo "$(aec_oracle_src_dir)/build"
}

# aec_oracle_abseil_dir: the abseil-cpp subproject checkout, for the
# cgo shim's second -I path (absl headers).
aec_oracle_abseil_dir() {
  echo "$(aec_oracle_src_dir)/subprojects/abseil-cpp-${AEC_ABSEIL_VERSION}"
}

# aec_oracle_ready: exit 0 iff a built static lib is present, i.e. the
# oracle has been fetched *and* built (not just downloaded).
aec_oracle_ready() {
  [[ -f "$(aec_oracle_build_dir)/webrtc/modules/audio_processing/libwebrtc-audio-processing-2.a" ]]
}

# aec_oracle_ensure_meson_ninja: make meson/ninja available on PATH,
# falling back to a `pip3 install --user` if neither is already
# installed. Per-repo policy: no brew auto-install (would mutate the
# host's package manager state); pip3 --user is the approved fallback
# since it's an isolated per-user install. If that also fails to
# produce working binaries, print instructions and return non-zero —
# callers should surface this to the user rather than thrashing.
aec_oracle_ensure_meson_ninja() {
  if command -v meson >/dev/null 2>&1 && command -v ninja >/dev/null 2>&1; then
    return 0
  fi

  local user_bin
  user_bin="$(python3 -m site --user-base 2>/dev/null)/bin"
  if [[ -x "${user_bin}/meson" && -x "${user_bin}/ninja" ]]; then
    export PATH="${user_bin}:${PATH}"
    return 0
  fi

  echo "meson/ninja not found on PATH; attempting 'pip3 install --user meson ninja'..." >&2
  if ! pip3 install --user --break-system-packages meson ninja >&2; then
    echo "ERROR: pip3 install of meson/ninja failed. Install them manually" >&2
    echo "  (e.g. 'brew install meson ninja', or your distro's packages)" >&2
    echo "  and re-run." >&2
    return 1
  fi
  export PATH="${user_bin}:${PATH}"
  if ! command -v meson >/dev/null 2>&1 || ! command -v ninja >/dev/null 2>&1; then
    echo "ERROR: meson/ninja still not on PATH after pip3 install (checked ${user_bin})." >&2
    return 1
  fi
}
