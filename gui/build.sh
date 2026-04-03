#!/bin/bash
set -e

# Build Good TURN GUI app with bundled Hysteria2
# Usage: ./build.sh [--os darwin|windows] [--arch amd64|arm64]

OS="${GOOS:-$(go env GOOS)}"
ARCH="${GOARCH:-$(go env GOARCH)}"
HY_VERSION="app/v2.6.1"  # update as needed

while [[ $# -gt 0 ]]; do
  case $1 in
    --os)   OS="$2"; shift 2;;
    --arch) ARCH="$2"; shift 2;;
    *) echo "Unknown: $1"; exit 1;;
  esac
done

echo "Building Good TURN GUI for ${OS}/${ARCH}"

# --- 1. Download Hysteria2 ---
HY_NAME="hysteria-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  HY_NAME="${HY_NAME}.exe"
fi
HY_URL="https://github.com/apernet/hysteria/releases/download/${HY_VERSION}/${HY_NAME}"

echo "[1/3] Downloading Hysteria2..."
mkdir -p .cache
if [ ! -f ".cache/${HY_NAME}" ]; then
  curl -fSL -o ".cache/${HY_NAME}" "$HY_URL"
  chmod +x ".cache/${HY_NAME}"
  echo "  Downloaded ${HY_NAME}"
else
  echo "  Using cached ${HY_NAME}"
fi

# --- 2. Build with Wails ---
echo "[2/3] Building with Wails..."
wails build -platform "${OS}/${ARCH}" -clean

# --- 3. Bundle Hysteria2 ---
echo "[3/3] Bundling Hysteria2..."

if [ "$OS" = "darwin" ]; then
  # macOS: put inside .app bundle next to main binary
  APP_DIR="build/bin/good-turn.app/Contents/MacOS"
  cp ".cache/${HY_NAME}" "${APP_DIR}/hysteria"
  chmod +x "${APP_DIR}/hysteria"
  echo "  Bundled into ${APP_DIR}/hysteria"

  # Create DMG
  if command -v create-dmg &>/dev/null; then
    create-dmg \
      --volname "Good TURN" \
      --no-internet-enable \
      "build/bin/Good-TURN-${ARCH}.dmg" \
      "build/bin/good-turn.app"
    echo "  Created DMG: build/bin/Good-TURN-${ARCH}.dmg"
  else
    echo "  Tip: brew install create-dmg for .dmg packaging"
  fi

elif [ "$OS" = "windows" ]; then
  # Windows: put next to .exe
  BIN_DIR="build/bin"
  cp ".cache/${HY_NAME}" "${BIN_DIR}/hysteria.exe"
  echo "  Bundled into ${BIN_DIR}/hysteria.exe"

  # Create ZIP
  cd build/bin
  zip -r "Good-TURN-windows-${ARCH}.zip" good-turn.exe hysteria.exe
  cd ../..
  echo "  Created ZIP: build/bin/Good-TURN-windows-${ARCH}.zip"
fi

echo ""
echo "Done! Output in build/bin/"
ls -lh build/bin/
