#!/bin/sh
# Samuel v2 installer
# Usage: curl -sSL https://raw.githubusercontent.com/samuelpkg/samuel/main/install.sh | sh

set -e

GITHUB_REPO="samuelpkg/samuel"
BINARY_NAME="samuel"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()    { printf "${GREEN}→${NC} %s\n" "$1"; }
warn()    { printf "${YELLOW}⚠${NC} %s\n" "$1"; }
error()   { printf "${RED}✗${NC} %s\n" "$1" >&2; exit 1; }
success() { printf "${GREEN}✓${NC} %s\n" "$1"; }

detect_os() {
  case "$(uname -s)" in
    Linux*)   echo linux ;;
    Darwin*)  echo darwin ;;
    *)        error "Unsupported OS: $(uname -s) (Samuel v2 targets macOS and Linux; Windows on the post-2.0 roadmap)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *)             error "Unsupported architecture: $(uname -m)" ;;
  esac
}

get_latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -sSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
  else
    error "Neither curl nor wget found. Please install one of them."
  fi
}

download() {
  url="$1"; out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -sSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$out"
  else
    error "Neither curl nor wget found."
  fi
}

verify_checksum() {
  file="$1"; expected="$2"
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$file" | cut -d ' ' -f 1)
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$file" | cut -d ' ' -f 1)
  else
    warn "Unable to verify checksum (sha256sum/shasum not found)"
    return 0
  fi
  if [ "$actual" != "$expected" ]; then
    error "Checksum verification failed!\nExpected: $expected\nActual: $actual"
  fi
}

main() {
  echo ""
  echo " Samuel v2 — Rails for AI coding assistants"
  echo ""

  OS=$(detect_os)
  ARCH=$(detect_arch)
  info "Detected platform: ${OS}/${ARCH}"

  VERSION="${VERSION:-$(get_latest_version)}"
  [ -z "$VERSION" ] && error "Could not determine latest version. Set VERSION env var to override."
  info "Installing version: ${VERSION}"

  ARCHIVE="${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
  ARCHIVE_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${ARCHIVE}"
  CHECKSUMS_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/checksums.txt"

  TMP=$(mktemp -d); trap "rm -rf $TMP" EXIT

  info "Downloading ${ARCHIVE}..."
  download "$ARCHIVE_URL" "$TMP/$ARCHIVE"

  info "Verifying checksum..."
  download "$CHECKSUMS_URL" "$TMP/checksums.txt"
  EXPECTED=$(grep "$ARCHIVE" "$TMP/checksums.txt" | cut -d ' ' -f 1)
  if [ -n "$EXPECTED" ]; then
    verify_checksum "$TMP/$ARCHIVE" "$EXPECTED"
    success "Checksum verified"
  else
    warn "Could not find checksum for $ARCHIVE"
  fi

  info "Extracting..."
  cd "$TMP" && tar -xzf "$ARCHIVE"

  info "Installing to ${INSTALL_DIR}..."
  if [ ! -d "$INSTALL_DIR" ]; then
    mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
  fi
  if [ -w "$INSTALL_DIR" ]; then
    mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
  else
    sudo mv "${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
  fi

  if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    success "Installation complete!"
    echo ""
    "$BINARY_NAME" version
    echo ""
    echo "Run 'samuel init' to get started."
  else
    success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
    warn "Ensure ${INSTALL_DIR} is in your PATH:"
    echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
  fi
}

main "$@"
