#!/bin/sh
# Install Release Planner from a GitHub release, verifying the archive's checksum.
#
#   curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh -s -- --version v0.1.0
#
# Options:
#   --version <tag>       Release to install (default: the latest release)
#   --install-dir <dir>   Where to put the binary (default: $HOME/.local/bin)
set -eu

repo_url="https://github.com/fabricahq/release-planner"
download_url="${RELEASE_PLANNER_DOWNLOAD_URL:-$repo_url/releases/download}"
latest_url="${RELEASE_PLANNER_LATEST_URL:-$repo_url/releases/latest}"
version=""
install_dir="${HOME}/.local/bin"

fail() { echo "release-planner install: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || fail "--version needs a value"; version="$2"; shift 2 ;;
    --install-dir) [ $# -ge 2 ] || fail "--install-dir needs a value"; install_dir="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0" 2>/dev/null || true; exit 0 ;;
    *) fail "unknown option $1" ;;
  esac
done

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported operating system $(uname -s); release-planner supports Linux and macOS" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "sha256sum or shasum is required to verify the download"
fi

if [ -z "$version" ]; then
  # GitHub redirects the latest-release page to the tag it names.
  resolved=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$latest_url") || fail "could not find the latest release"
  version="${resolved##*/}"
fi
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) fail "\"$version\" is not a release version such as v0.1.0" ;;
esac

archive="release-planner_${version#v}_${os}_${arch}.tar.gz"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

curl -fsSL -o "$work/SHA256SUMS" "$download_url/$version/SHA256SUMS" || fail "could not download SHA256SUMS for $version"
curl -fsSL -o "$work/$archive" "$download_url/$version/$archive" || fail "could not download $archive"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$work/SHA256SUMS")
[ -n "$expected" ] || fail "SHA256SUMS for $version does not list $archive"
[ "$(sha256 "$work/$archive")" = "$expected" ] || fail "checksum mismatch for $archive; not installing"

tar -xzf "$work/$archive" -C "$work" release-planner
mkdir -p "$install_dir"
mv "$work/release-planner" "$install_dir/release-planner"
chmod 0755 "$install_dir/release-planner"

echo "Installed release-planner $version to $install_dir/release-planner"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to your PATH to run release-planner." ;;
esac
