#!/usr/bin/env sh
set -eu

REPO="Perdonus/NV"
RAW_BASE="https://raw.githubusercontent.com/${REPO}/linux-builds"
INSTALL_ROOT="${NV_INSTALL_ROOT:-$HOME/.local/bin}"
TARGET="$INSTALL_ROOT/nv"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT INT TERM

command -v curl >/dev/null 2>&1 || { echo "не найдена команда: curl" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "не найдена команда: tar" >&2; exit 1; }
command -v install >/dev/null 2>&1 || { echo "не найдена команда: install" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "не найдена команда: python3" >&2; exit 1; }

mkdir -p "$INSTALL_ROOT"
curl -fsSL "$RAW_BASE/manifest.json" -o "$TMP_DIR/manifest.json"
NV_URL="$(python3 - <<'PY' "$TMP_DIR/manifest.json"
import json,sys
with open(sys.argv[1], 'r', encoding='utf-8') as fh:
    manifest=json.load(fh)
for item in manifest.get('artifacts', []):
    if item.get('platform') == 'nv-linux':
        print(item.get('download_url') or '')
        break
PY
)"
[ -n "$NV_URL" ] || { echo "артефакт nv-linux не найден" >&2; exit 1; }
curl -fsSL "$NV_URL" -o "$TMP_DIR/nv-linux.tar.gz"
tar -xzf "$TMP_DIR/nv-linux.tar.gz" -C "$TMP_DIR"
NV_BIN="$(find "$TMP_DIR" -maxdepth 2 -type f -name 'nv' | head -n 1)"
[ -n "$NV_BIN" ] || { echo "payload nv не найден" >&2; exit 1; }
install -m 0755 "$NV_BIN" "$TARGET"
echo "Установлен или обновлен nv в $TARGET"
