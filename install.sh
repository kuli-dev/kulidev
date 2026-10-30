#!/bin/sh
# Install KuliDev (the `kuli` binary) from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/kuli-dev/kulidev/main/install.sh | sh
#
# Options (environment):
#   KULI_VERSION=v0.1.0      a specific release (default: latest)
#   KULI_BIN_DIR=/usr/local/bin   where to install (default: ~/.local/bin)
# While the repository is private, the GitHub CLI (`gh auth login`) is used to download.
set -eu

REPO="kuli-dev/kulidev"
VERSION="${KULI_VERSION:-latest}"
BIN_DIR="${KULI_BIN_DIR:-$HOME/.local/bin}"

say() { printf 'kuli-install: %s\n' "$*" >&2; }
die() { say "$*"; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS $(uname -s); on Windows download the zip from https://github.com/$REPO/releases or use WSL" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

use_gh=false
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then use_gh=true; fi

if [ "$VERSION" = latest ]; then
  if $use_gh; then
    VERSION=$(gh release view --repo "$REPO" --json tagName -q .tagName) || die "could not find the latest release"
  else
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
    [ -n "$VERSION" ] || die "could not find the latest release (private repository? install the GitHub CLI and run 'gh auth login')"
  fi
fi

asset="kuli_${VERSION#v}_${os}_${arch}.tar.gz"
say "installing KuliDev $VERSION ($os/$arch) into $BIN_DIR"
if $use_gh; then
  gh release download "$VERSION" --repo "$REPO" --pattern "$asset" --pattern checksums.txt --dir "$tmp" || die "download failed"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
  curl -fsSL -o "$tmp/$asset" "$base/$asset" || die "download failed: $base/$asset"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "checksum download failed"
fi

# Verify before running anything we downloaded.
expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || die "$asset is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1); fi
[ "$expected" = "$actual" ] || die "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/kuli" "$BIN_DIR/kuli"
say "installed $("$BIN_DIR/kuli" version)"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "add $BIN_DIR to your PATH, e.g.  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.bashrc" ;;
esac
