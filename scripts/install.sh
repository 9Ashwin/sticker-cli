#!/usr/bin/env bash
set -euo pipefail

repo="9Ashwin/sticker-cli"
version="${1:-${STICKER_VERSION:-}}"
install_dir="${STICKER_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"

fail() {
  printf 'sticker install: %s\n' "$*" >&2
  exit 1
}

[[ -n "$install_dir" && "$install_dir" != "/" ]] || fail "STICKER_INSTALL_DIR must be a writable user directory"

download() {
  local url=$1
  local output=$2
  if command -v curl >/dev/null 2>&1; then
    curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location --retry 3 --connect-timeout 10 "$url" -o "$output"
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only --timeout=10 --tries=3 --output-document="$output" "$url"
  else
    fail "curl or wget is required"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print tolower($1)}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print tolower($1)}'
  else
    fail "sha256sum or shasum is required"
  fi
}

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "this installer supports Linux and macOS; use install.ps1 on Windows" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported CPU architecture: $(uname -m)" ;;
esac

if [[ -z "$version" ]]; then
  metadata=$(mktemp)
  trap 'rm -f "$metadata"' EXIT
  download "https://api.github.com/repos/${repo}/releases/latest" "$metadata"
  version=$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata" | head -n 1)
fi
version="v${version#v}"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "version must be a release version such as v1.2.3"

asset="sticker_${os}_${arch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${version}"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/sticker-install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

download "${base_url}/${asset}" "${tmp}/${asset}"
download "${base_url}/checksums.txt" "${tmp}/checksums.txt"
expected=$(awk -v file="$asset" '$2 == file || $2 == "*" file {print tolower($1); exit}' "$tmp/checksums.txt")
[[ "$expected" =~ ^[0-9a-f]{64}$ ]] || fail "release checksum is missing for ${asset}"
[[ "$expected" == "$(sha256 "${tmp}/${asset}")" ]] || fail "release checksum verification failed"

mkdir -p "$tmp/unpacked"
while IFS= read -r entry; do
  case "$entry" in
    sticker|LICENSE|VERSION|checksums.txt) ;;
    *) fail "release archive contains an unexpected path: ${entry}" ;;
  esac
done < <(tar -tzf "${tmp}/${asset}")
tar -xzf "${tmp}/${asset}" -C "$tmp/unpacked"

[[ -f "$tmp/unpacked/sticker" && -f "$tmp/unpacked/checksums.txt" ]] || fail "release archive is incomplete"
expected_binary=$(awk '$2 == "sticker" || $2 == "*sticker" {print tolower($1); exit}' "$tmp/unpacked/checksums.txt")
[[ "$expected_binary" =~ ^[0-9a-f]{64}$ ]] || fail "binary checksum is missing from the archive"
[[ "$expected_binary" == "$(sha256 "$tmp/unpacked/sticker")" ]] || fail "binary checksum verification failed"

mkdir -p "$install_dir"
temporary="$install_dir/.sticker-install.$$"
cleanup() { rm -f "$temporary"; rm -rf "$tmp"; }
trap cleanup EXIT
install -m 0755 "$tmp/unpacked/sticker" "$temporary"
mv -f "$temporary" "$install_dir/sticker"

printf 'installed sticker %s to %s\n' "$version" "$install_dir/sticker"
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) printf 'add %s to PATH to run sticker directly\n' "$install_dir" ;;
esac
