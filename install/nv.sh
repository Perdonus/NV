#!/usr/bin/env sh
set -eu

SITE_BASE="${NV_SITE_BASE:-https://neuralvv.org}"
API_BASE="${NV_BOOTSTRAP_BASE:-$SITE_BASE/nv/api}"
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
curl -fsSL "$API_BASE/bootstrap/manifest?platform=nv-linux" -o "$TMP_DIR/manifest.json"
NV_URL="$(python3 - <<'PY' "$TMP_DIR/manifest.json" "$SITE_BASE"
import json,sys
with open(sys.argv[1], 'r', encoding='utf-8') as fh:
    manifest=json.load(fh)
site_base=sys.argv[2].rstrip('/')
url=''
for item in manifest.get('artifacts', []):
    if item.get('platform') == 'nv-linux':
        url=(item.get('download_url') or '').strip()
        break
if url.startswith('/'):
    url=site_base+url
print(url)
PY
)"
[ -n "$NV_URL" ] || { echo "артефакт nv-linux не найден" >&2; exit 1; }
curl -fsSL "$NV_URL" -o "$TMP_DIR/nv-linux.tar.gz"
tar -xzf "$TMP_DIR/nv-linux.tar.gz" -C "$TMP_DIR"
NV_BIN="$(find "$TMP_DIR" -maxdepth 2 -type f -name 'nv' | head -n 1)"
[ -n "$NV_BIN" ] || { echo "payload nv не найден" >&2; exit 1; }
install -m 0755 "$NV_BIN" "$TARGET"
echo "Установлен или обновлён nv в $TARGET"
