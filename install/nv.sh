#!/usr/bin/env sh
set -eu

SITE_BASE="${NV_SITE_BASE:-https://sosiskibot.ru}"
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

download_file() {
python3 - <<'PY' "$1" "$2"
import shutil
import sys
import urllib.request

url, target = sys.argv[1], sys.argv[2]
with urllib.request.urlopen(url) as response, open(target, "wb") as out:
    shutil.copyfileobj(response, out)
PY
}

mkdir -p "$INSTALL_ROOT"
download_file "$API_BASE/bootstrap/manifest?platform=nv-linux" "$TMP_DIR/manifest.json"
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
download_file "$NV_URL" "$TMP_DIR/nv-linux.tar.gz"
tar -xzf "$TMP_DIR/nv-linux.tar.gz" -C "$TMP_DIR"
NV_BIN="$(find "$TMP_DIR" -maxdepth 2 -type f -name 'nv' | head -n 1)"
[ -n "$NV_BIN" ] || { echo "payload nv не найден" >&2; exit 1; }
install -m 0755 "$NV_BIN" "$TARGET"
python3 - <<'PY' "$TMP_DIR/manifest.json" "$SITE_BASE" "$INSTALL_ROOT"
import json
import os
import pathlib
import time

manifest_path, site_base, install_root = map(str, __import__("sys").argv[1:4])
site_base = site_base.rstrip("/")

with open(manifest_path, "r", encoding="utf-8") as fh:
    manifest = json.load(fh)

artifact = {}
for item in manifest.get("artifacts", []):
    if item.get("platform") == "nv-linux":
        artifact = item
        break

version = str(artifact.get("version") or "").strip() or "unknown"
download_url = str(artifact.get("download_url") or "").strip()
if download_url.startswith("/"):
    download_url = site_base + download_url
file_name = str(artifact.get("file_name") or "").strip() or f"nv-linux-{version}.tar.gz"
install_command = f"curl -fsSL {site_base}/install/nv.sh | sh"

xdg_state_home = os.environ.get("XDG_STATE_HOME", "").strip()
home = pathlib.Path.home()
if xdg_state_home:
    state_path = pathlib.Path(xdg_state_home) / "nv" / "packages.json"
else:
    state_path = home / ".local" / "state" / "nv" / "packages.json"
state_path.parent.mkdir(parents=True, exist_ok=True)

payload = {"schema_version": 1, "packages": {}}
if state_path.exists():
    try:
        payload = json.loads(state_path.read_text("utf-8"))
    except Exception:
        payload = {"schema_version": 1, "packages": {}}

packages = payload.get("packages")
if not isinstance(packages, dict):
    packages = {}
payload["packages"] = packages
payload["schema_version"] = 1

existing = packages.get("nv") or packages.get("@lvls/nv") or {}
installed_at = str(existing.get("installed_at") or "").strip() or time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
updated_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

packages.pop("@lvls/nv", None)
packages["nv"] = {
    "package": {
        "name": "nv",
        "title": "NV",
        "description": "Пакетный менеджер NV.",
        "homepage": f"{site_base}/nv/",
        "latest_version": version,
        "resolved_version": version,
        "variant": {
            "id": "nv-linux",
            "label": "Linux",
            "os": "linux",
            "is_default": True,
            "default": False,
            "version": version,
            "file_name": file_name,
            "download_url": download_url,
            "install_command": install_command,
            "install_strategy": "unix-self-binary",
            "install_root": install_root,
            "binary_name": "nv",
            "metadata": {"bootstrapPlatform": "nv-linux"},
            "update_command": "nv i nv",
            "update_policy": "nv-self",
        },
    },
    "installed_at": installed_at,
    "updated_at": updated_at,
    "install_root": install_root,
}

state_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", "utf-8")
PY
echo "Установлен или обновлён nv в $TARGET"
