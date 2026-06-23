#!/bin/sh
# agenttop installer — downloads the latest release from GitHub.
# Supports macOS (x86_64 + arm64) and Linux (x86_64 + arm64).
set -e

OWNER="SalzDevs"
REPO="agenttop"

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "unsupported OS: $OS" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) arch="x64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
  download() { curl -fsSL -o "$1" "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
  download() { wget -qO "$1" "$2"; }
else
  echo "need curl or wget" >&2; exit 1
fi

tag="$(fetch "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')"
if [ -z "$tag" ]; then
  echo "could not determine latest release; install via bunx:" >&2
  echo "  bunx ${OWNER}/${REPO}" >&2
  exit 1
fi

binary="agenttop-${os}-${arch}"
url="https://github.com/${OWNER}/${REPO}/releases/download/${tag}/${binary}"
echo "Downloading agenttop ${tag} (${os}/${arch})..."
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

if ! download "${tmpdir}/${binary}" "$url"; then
  echo "no prebuilt binary for ${os}/${arch}; install via bunx:" >&2
  echo "  bunx ${OWNER}/${REPO}" >&2
  exit 1
fi

dest="${DESTDIR:-${HOME}/.local/bin}"
mkdir -p "$dest"
mv "${tmpdir}/${binary}" "${dest}/agenttop"
chmod +x "${dest}/agenttop"
echo "installed agenttop ${tag} to ${dest}/agenttop"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "note: $dest is not on your PATH — add it to your shell profile" >&2 ;;
esac
