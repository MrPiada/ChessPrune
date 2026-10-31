#!/usr/bin/env bash
# Create every release archive on macOS using Go's native cross-compiler.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="${1:-v0.1.0-go}"
DIST="${DIST_DIR:-$ROOT/dist}"
mkdir -p "$DIST"
# Using a fresh staging area avoids accidentally packaging private PGNs or reports.
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
for target in darwin/arm64 darwin/amd64 windows/amd64 linux/amd64 linux/arm64; do
  OS="${target%/*}"
  ARCH="${target#*/}"
  NAME="ChessPrune-${VERSION}-${OS}-${ARCH}"
  DIR="$STAGING/$NAME"
  mkdir -p "$DIR"
  EXE="ChessPrune"
  if [[ "$OS" == windows ]]; then EXE="ChessPrune.exe"; fi
  echo "==> Building $target"
  CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" -o "$DIR/$EXE" .
  cp LICENSE "$DIR/LICENSE"
  cp packaging/PORTABLE_README.txt "$DIR/README.txt"
  if [[ "$OS" == darwin ]]; then
    APP="$DIR/ChessPrune.app"
    mkdir -p "$APP/Contents/MacOS"
    mv "$DIR/$EXE" "$APP/Contents/MacOS/ChessPrune"
    APP_VERSION="${VERSION#v}"
    APP_VERSION="${APP_VERSION%%-*}"
    sed "s/__VERSION__/$APP_VERSION/g" packaging/Info.plist > "$APP/Contents/Info.plist"
    mkdir -p "$APP/Contents/Resources"
    cp LICENSE "$APP/Contents/Resources/LICENSE"
    # Create a native Finder icon when running this script on macOS.
    if [[ "$(uname -s)" == Darwin ]] && command -v iconutil >/dev/null 2>&1 && command -v sips >/dev/null 2>&1; then
      ICONSET="$STAGING/ChessPrune.iconset"
      mkdir -p "$ICONSET"
      for SIZE in 16 32 128 256 512; do
        sips -s format png -z "$SIZE" "$SIZE" assets/ChessPrune_Icon.png --out "$ICONSET/icon_${SIZE}x${SIZE}.png" >/dev/null
        DOUBLE=$((SIZE*2))
        sips -s format png -z "$DOUBLE" "$DOUBLE" assets/ChessPrune_Icon.png --out "$ICONSET/icon_${SIZE}x${SIZE}@2x.png" >/dev/null
      done
      iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/ChessPrune.icns"
    fi
    # Ad-hoc signature is not notarization. Signing with Developer ID is a separate release step.
    if [[ "$(uname -s)" == Darwin ]] && command -v codesign >/dev/null 2>&1; then
      codesign --force --sign - "$APP" >/dev/null 2>&1 || true
    fi
    if [[ "$(uname -s)" == Darwin ]] && command -v ditto >/dev/null 2>&1; then
      (cd "$DIR" && ditto -c -k --sequesterRsrc --keepParent ChessPrune.app "$DIST/${NAME}.zip")
    else
      (cd "$DIR" && zip -qr "$DIST/${NAME}.zip" ChessPrune.app LICENSE README.txt)
    fi
    # DMGs can only be created on macOS. These installers are unsigned unless separately notarized.
    if [[ "$(uname -s)" == Darwin ]] && command -v hdiutil >/dev/null 2>&1; then
      hdiutil create -quiet -volname "ChessPrune" -srcfolder "$DIR/ChessPrune.app" \
        -ov -format UDZO "$DIST/${NAME}.dmg"
    fi
  elif [[ "$OS" == windows ]]; then
    (cd "$DIR" && zip -qr "$DIST/${NAME}.zip" "$EXE" LICENSE README.txt)
    # Optional native Windows Setup.exe created on the Mac via NSIS.
    if command -v makensis >/dev/null 2>&1; then
      makensis -V2 -DINPUT_EXE="$DIR/$EXE" -DOUTFILE="$DIST/${NAME}-Setup.exe" \
        -DLICENSE_PATH="$ROOT/LICENSE" packaging/WindowsInstaller.nsi
    fi
  else
    (cd "$STAGING" && tar czf "$DIST/${NAME}.tar.gz" "$NAME")
  fi
done
(cd "$DIST" && shasum -a 256 ChessPrune-"$VERSION"-* > SHA256SUMS.txt)
echo
printf 'Release assets generated in %s\n' "$DIST"
ls -lh "$DIST"/*
