#!/usr/bin/env bash
# Fetch the RNNoise v0.2 model weights (rnnoise_data.c/.h) that are NOT
# vendored in git — 28 MB of generated float-array C source. They are
# needed ONLY to build the cgo parity oracle
# (denoise/internal/parity_tests/{network,e2e}) and to regenerate the
# embedded weight blob (mise run //libraries/rnnoise:weights).
#
# The shipping pure-Go port embeds denoise/internal/rnnoise/rnnoise_weights.bin
# and needs none of this. Provenance (upstream model_version + SHAs) is in
# librnnoise/VERSION.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src="$here/librnnoise/src"
url="https://media.xiph.org/rnnoise/models/rnnoise_data-0b50c45.tar.gz"
sha_c="522b6a64fded05bf85e58c06206eafe57ce7d94f3af58c725b17628b481d7890"
sha_h="09ff880bddd0fc74a2ae0e5ec6c8d65714031b08d0c3f672493acd9e189c5855"

sha256() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else sha256sum "$1" | awk '{print $1}'; fi
}

if [ -f "$src/rnnoise_data.c" ] && [ "$(sha256 "$src/rnnoise_data.c")" = "$sha_c" ]; then
  echo "rnnoise_data.c already present and verified — nothing to do."
  exit 0
fi

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
echo "fetching $url"
curl -fsSL "$url" -o "$tmp/model.tar.gz"
tar -xzf "$tmp/model.tar.gz" -C "$tmp"

c="$(find "$tmp" -name rnnoise_data.c | head -1)"
h="$(find "$tmp" -name rnnoise_data.h | head -1)"
[ -n "$c" ] && [ -n "$h" ] || { echo "error: rnnoise_data.{c,h} not found in archive" >&2; exit 1; }

got_c="$(sha256 "$c")"; got_h="$(sha256 "$h")"
[ "$got_c" = "$sha_c" ] || { echo "error: rnnoise_data.c SHA mismatch: got $got_c want $sha_c" >&2; exit 1; }
[ "$got_h" = "$sha_h" ] || { echo "error: rnnoise_data.h SHA mismatch: got $got_h want $sha_h" >&2; exit 1; }

cp "$c" "$src/rnnoise_data.c"
cp "$h" "$src/rnnoise_data.h"
echo "installed rnnoise_data.{c,h} into $src (SHA-verified)"
