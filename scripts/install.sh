#!/bin/sh
# agenttop installer — downloads the latest release binary from GitHub.
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
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
else
  fetch() { wget -qO- "$1"; }
fi

tag="$(fetch "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')"
if [ -z "$tag" ]; then
  echo "could not determine latest release; build from source: go install github.com/${OWNER}/${REPO}@latest" >&2
  exit 1
fi

url="https://github.com/${OWNER}/${REPO}/releases/download/${tag}/agenttop-${os}-${arch}"
echo "Downloading agenttop ${tag} (${os}/${arch})..."
fetch "$url" -o /tmp/agenttop-download || { echo "no prebuilt binary for ${os}/${arch}; building from source..." >&2; go install "github.com/${OWNER}/${REPO}@${tag}" && exit 0; }
chmod +x /tmp/agenttop-download

dest="${DESTDIR:-${HOME}/.local/bin}"
mkdir -p "$dest"
mv /tmp/agenttop-download "$dest/agenttop"
echo "installed agenttop to ${dest}/agenttop"
case ":$PATH:" in
  *":$dest:"*) ;;
  *) echo "note: $dest is not on your PATH — add it to your shell profile" >&2 ;;
esac
agenttop --help 2>/dev/null || true
