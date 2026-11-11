#!/usr/bin/env bash
# Install an exact Mio release. Fails (never substitutes) when the version,
# platform or checksum does not match. Supported: linux-amd64, darwin-arm64,
# windows-amd64, as published in the release assets.
set -euo pipefail

version="${MIO_SETUP_VERSION:-}"
dist="${MIO_SETUP_DISTRIBUTION:-full}"
repo="${MIO_SETUP_REPOSITORY:-mio-kobo/mio}"
fail() { echo "setup-mio: $*" >&2; exit 1; }

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "version must be an exact x.y.z release, got '${version}'"
[[ "$dist" == full || "$dist" == core ]] || fail "distribution must be 'full' or 'core', got '${dist}'"
[[ "$repo" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]] || fail "invalid repository '${repo}'"

case "$(uname -s)" in
  Linux) os=linux; ext=tar.gz; exe=mio ;;
  Darwin) os=darwin; ext=tar.gz; exe=mio ;;
  MINGW*|MSYS*|CYGWIN*) os=windows; ext=zip; exe=mio.exe ;;
  *) fail "unsupported operating system $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64|AMD64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac
case "${os}-${arch}" in
  linux-amd64|darwin-arm64|windows-amd64) ;;
  *) fail "no official Mio ${version} release for ${os}-${arch}; supported: linux-amd64, darwin-arm64, windows-amd64" ;;
esac

name="mio-${version}-${os}-${arch}-${dist}"
asset="${name}.${ext}"
cache_root="${RUNNER_TOOL_CACHE:-${TMPDIR:-/tmp}/mio-toolcache}"
dest="${cache_root}/mio/${version}/${os}-${arch}-${dist}"
hit=false

if [[ -x "${dest}/${name}/bin/${exe}" && -f "${dest}/.verified" ]]; then
  hit=true
else
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  command -v gh >/dev/null || fail "the gh CLI is required on the runner"
  gh release download "v${version}" --repo "$repo" --dir "$work" --pattern "$asset" --pattern SHA256SUMS \
    || fail "release v${version} or asset ${asset} not found in ${repo}"
  expected="$(awk -v a="$asset" '$2==a {print $1}' "$work/SHA256SUMS")"
  [[ -n "$expected" ]] || fail "${asset} is not listed in SHA256SUMS"
  if command -v sha256sum >/dev/null; then actual="$(sha256sum "$work/$asset" | awk '{print $1}')"
  else actual="$(shasum -a 256 "$work/$asset" | awk '{print $1}')"; fi
  [[ "$actual" == "$expected" ]] || fail "checksum mismatch for ${asset}: expected ${expected}, got ${actual}"
  mkdir -p "$dest.tmp"
  if [[ "$ext" == zip ]]; then
    if command -v unzip >/dev/null; then unzip -q "$work/$asset" -d "$dest.tmp"
    else powershell -NoProfile -Command "Expand-Archive -LiteralPath '$(cygpath -w "$work/$asset")' -DestinationPath '$(cygpath -w "$dest.tmp")'"; fi
  else
    tar -xf "$work/$asset" -C "$dest.tmp"
  fi
  [[ -f "$dest.tmp/${name}/bin/${exe}" ]] || fail "archive does not contain ${name}/bin/${exe}"
  rm -rf "$dest"; mv "$dest.tmp" "$dest"
  : > "${dest}/.verified"
fi

bin="${dest}/${name}/bin"
"${bin}/${exe}" version >/dev/null 2>&1 || "${bin}/${exe}" --version >/dev/null 2>&1 || fail "installed binary does not run"
reported="$("${bin}/${exe}" version 2>&1 | head -1 || true)"
echo "$reported" | grep -q "${version}" || fail "installed binary reports '${reported}', expected ${version}"

pathout="$bin"; command -v cygpath >/dev/null && pathout="$(cygpath -w "$bin")"
[[ -z "${GITHUB_PATH:-}" ]] || echo "$pathout" >> "$GITHUB_PATH"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  { echo "version=${version}"; echo "path=${pathout}"; echo "cache-hit=${hit}"; } >> "$GITHUB_OUTPUT"
fi
echo "setup-mio: ${reported} (${os}-${arch}, ${dist}, cache-hit=${hit})"
