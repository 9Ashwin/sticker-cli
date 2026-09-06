#!/usr/bin/env bash
set -euo pipefail

repo="9Ashwin/sticker-cli"
version="${STICKER_VERSION:-}"
install_dir="${STICKER_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"
skill_dir="${STICKER_SKILL_DIR:-$HOME/.agents/skills/sticker}"
pack=""
pack_source=""
skill_dir_explicit=false

fail() {
  printf 'sticker install: %s\n' "$*" >&2
  exit 1
}

[[ -n "$install_dir" && "$install_dir" != "/" ]] || fail "STICKER_INSTALL_DIR must be a writable user directory"

usage() {
  cat <<'EOF'
Usage: install.sh [VERSION] [--pack curated|all] [--source DIR_OR_URL] [--skill-dir DIR]

Install the sticker CLI and its Agent Skill. The installer never downloads
sticker images unless --pack is provided; choose a pack later with `sticker setup`.

Environment:
  STICKER_VERSION       release version, for example v1.0.0
  STICKER_INSTALL_DIR   binary destination (default: $HOME/.local/bin)
  STICKER_SKILL_DIR     direct Skill destination when set
  STICKER_PACK_SOURCE   default pack directory or HTTPS source for setup
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --pack)
      [[ $# -ge 2 ]] || fail "--pack requires curated or all"
      case "$2" in
        curated|all) pack=$2 ;;
        *) fail "--pack must be curated or all" ;;
      esac
      shift
      ;;
    --source)
      [[ $# -ge 2 ]] || fail "--source requires a directory or HTTPS URL"
      pack_source=$2
      shift
      ;;
    --skill-dir)
      [[ $# -ge 2 ]] || fail "--skill-dir requires a directory"
      skill_dir=$2
      skill_dir_explicit=true
      shift
      ;;
    --*)
      fail "unknown option: $1"
      ;;
    *)
      [[ -z "$version" || "$version" == "$1" ]] || fail "release version specified more than once"
      version=$1
      ;;
  esac
  shift
done

if [[ -n "${STICKER_SKILL_DIR:-}" ]]; then
  skill_dir_explicit=true
fi
[[ -z "$pack_source" || -n "$pack" ]] || fail "--source requires --pack curated or --pack all"

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
  if ! download "https://api.github.com/repos/${repo}/releases/latest" "$metadata"; then
    fail "could not read a published release; pass STICKER_VERSION=vX.Y.Z or use 'go install github.com/${repo}/cmd/sticker@latest'"
  fi
  version=$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata" | head -n 1)
fi
[[ -n "$version" ]] || fail "no GitHub release is published yet; install with 'go install github.com/${repo}/cmd/sticker@latest' or pass STICKER_VERSION=vX.Y.Z"
version="v${version#v}"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "version must be a release version such as v1.2.3"

asset="sticker_${os}_${arch}.tar.gz"
base_url="${STICKER_RELEASE_BASE_URL:-https://github.com/${repo}/releases/download/${version}}"
base_url="${base_url%/}"
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

install_skill_file() {
  local destination=$1
  local parent
  local skill_tmp
  parent=$(dirname "$destination")

  if [[ -e "$destination" ]]; then
    if [[ -f "$destination/SKILL.md" ]]; then
      printf 'sticker skill already exists at %s; skipped\n' "$destination"
      return
    fi
    fail "skill destination exists and is not a sticker skill: $destination"
  fi

  skill_tmp="$tmp/sticker-skill.md"
  download "https://raw.githubusercontent.com/${repo}/${version}/skills/sticker/SKILL.md" "$skill_tmp"
  grep -Eq '^name:[[:space:]]*sticker[[:space:]]*$' "$skill_tmp" || fail "downloaded sticker Skill is invalid"

  mkdir -p "$parent"
  if ! mkdir "$destination" 2>/dev/null; then
    if [[ -f "$destination/SKILL.md" ]]; then
      printf 'sticker skill already exists at %s; skipped\n' "$destination"
      return
    fi
    fail "could not create Skill destination: $destination"
  fi
  install -m 0644 "$skill_tmp" "$destination/SKILL.md"
  printf 'installed sticker skill at %s\n' "$destination"
}

install_agent_skill() {
  local installed_skills
  if [[ "$skill_dir_explicit" == false ]] && [[ -e "$skill_dir" ]]; then
    install_skill_file "$skill_dir"
    return 0
  fi

  if [[ "$skill_dir_explicit" == false ]] && command -v npx >/dev/null 2>&1; then
    installed_skills=$(npx --yes skills ls --global --json 2>/dev/null || true)
    if grep -Eq '"name"[[:space:]]*:[[:space:]]*"sticker"' <<<"$installed_skills"; then
      printf 'sticker skill is already installed for an Agent client; skipped\n'
      return 0
    fi
    if npx --yes skills add "https://github.com/${repo}/tree/${version}" --skill sticker --global --yes --copy; then
      printf 'installed sticker skill for supported Agent clients\n'
      return 0
    fi
    printf 'skills manager unavailable; installing the Skill at %s instead\n' "$skill_dir" >&2
  fi

  install_skill_file "$skill_dir"
}

install_agent_skill

if [[ -n "$pack" ]]; then
  setup_args=(setup --pack "$pack")
  if [[ -n "$pack_source" ]]; then
    setup_args+=(--source "$pack_source")
  elif [[ -n "${STICKER_PACK_SOURCE:-}" ]]; then
    setup_args+=(--source "$STICKER_PACK_SOURCE")
  fi
  if ! "$install_dir/sticker" "${setup_args[@]}"; then
    fail "CLI installed, but ${pack} pack setup failed; rerun sticker setup --pack ${pack} after fixing the source"
  fi
fi
