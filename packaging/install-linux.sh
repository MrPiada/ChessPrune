#!/bin/sh
# Install a launcher and icon for this extracted Linux package in the user's home.
set -eu
HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
APP="$HOME/.local/share/chessprune"
mkdir -p "$APP" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/256x256/apps"
cp "$HERE/ChessPrune" "$APP/ChessPrune"
chmod +x "$APP/ChessPrune"
cp "$HERE/share/icons/hicolor/256x256/apps/chessprune.png" "$HOME/.local/share/icons/hicolor/256x256/apps/chessprune.png"
cat > "$HOME/.local/share/applications/ChessPrune.desktop" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=ChessPrune
Comment=Opening repertoire analyzer and graph
Exec=$APP/ChessPrune
Icon=chessprune
Terminal=true
Categories=Game;Education;
StartupNotify=false
EOF
printf 'Installed ChessPrune and desktop launcher in %s\n' "$APP"
