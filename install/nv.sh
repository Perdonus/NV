#!/usr/bin/env sh
set -eu

SITE_BASE="${NV_SITE_BASE:-https://sosiskibot.ru}"
API_BASE="${NV_BOOTSTRAP_BASE:-$SITE_BASE/nv/api}"
IS_TERMUX=0
if [ -n "${PREFIX:-}" ] && printf '%s' "$PREFIX" | grep -q '/com.termux/'; then
  IS_TERMUX=1
fi

default_install_root() {
  if [ "$IS_TERMUX" -eq 1 ] && [ -n "${PREFIX:-}" ]; then
    echo "$PREFIX/bin"
    return
  fi
  if [ "$(id -u 2>/dev/null || echo 1)" = "0" ] && [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    echo "/usr/local/bin"
    return
  fi
  echo "$HOME/.local/bin"
}

if [ -z "${NV_INSTALL_ROOT:-}" ]; then
  INSTALL_ROOT="$(default_install_root)"
else
  INSTALL_ROOT="$NV_INSTALL_ROOT"
fi
TARGET="$INSTALL_ROOT/nv"
LEGACY_TERMUX_TARGET="$HOME/.local/bin/nv"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT INT TERM

command -v curl >/dev/null 2>&1 || { echo "не найдена команда: curl" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "не найдена команда: tar" >&2; exit 1; }
command -v install >/dev/null 2>&1 || { echo "не найдена команда: install" >&2; exit 1; }

download_file() {
  curl -fsSL "$1" -o "$2"
}

json_string() {
  key="$1"
  tr '\n' ' ' | sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" | head -n 1
}

detect_platform() {
  machine="$(uname -m 2>/dev/null || echo unknown)"
  case "$machine" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    armv7l|armv7*|armhf) arch="armv7" ;;
    *) echo "архитектура $machine пока не поддерживается" >&2; exit 1 ;;
  esac
  if [ "$IS_TERMUX" -eq 1 ]; then
    echo "nv-termux-$arch"
  else
    echo "nv-linux-$arch"
  fi
}

platform_os() {
  case "$1" in
    nv-termux-*) echo "android" ;;
    *) echo "linux" ;;
  esac
}

mkdir -p "$INSTALL_ROOT"
PLATFORM="${NV_PLATFORM:-$(detect_platform)}"
PLATFORM_OS="$(platform_os "$PLATFORM")"
download_file "$API_BASE/bootstrap/manifest?platform=$PLATFORM" "$TMP_DIR/manifest.json"
NV_URL="$(json_string download_url < "$TMP_DIR/manifest.json")"
VERSION="$(json_string version < "$TMP_DIR/manifest.json")"
FILE_NAME="$(json_string file_name < "$TMP_DIR/manifest.json")"
[ -n "$NV_URL" ] || { echo "артефакт $PLATFORM не найден" >&2; exit 1; }
case "$NV_URL" in
  /*) NV_URL="${SITE_BASE%/}$NV_URL" ;;
esac
[ -n "$VERSION" ] || VERSION="unknown"
[ -n "$FILE_NAME" ] || FILE_NAME="$PLATFORM-$VERSION.tar.gz"
download_file "$NV_URL" "$TMP_DIR/nv.tar.gz"
tar -xzf "$TMP_DIR/nv.tar.gz" -C "$TMP_DIR"
NV_BIN="$(find "$TMP_DIR" -maxdepth 2 -type f -name 'nv' | head -n 1)"
[ -n "$NV_BIN" ] || { echo "payload nv не найден" >&2; exit 1; }
install -m 0755 "$NV_BIN" "$TARGET"
if [ "$IS_TERMUX" -eq 1 ] && [ "$TARGET" != "$LEGACY_TERMUX_TARGET" ]; then
  rm -f "$LEGACY_TERMUX_TARGET"
fi
STATE_ROOT="${XDG_STATE_HOME:-$HOME/.local/state}"
STATE_DIR="$STATE_ROOT/nv"
STATE_PATH="$STATE_DIR/packages.json"
mkdir -p "$STATE_DIR"
if [ ! -s "$STATE_PATH" ]; then
cat > "$STATE_PATH" <<EOF
{
  "schema_version": 1,
  "packages": {
    "nv": {
      "package": {
        "name": "nv",
        "title": "NV",
        "description": "Пакетный менеджер NV.",
        "homepage": "${SITE_BASE%/}/nv/",
        "latest_version": "$VERSION",
        "resolved_version": "$VERSION",
        "variant": {
          "id": "$PLATFORM",
          "label": "$PLATFORM",
          "os": "$PLATFORM_OS",
          "is_default": true,
          "default": false,
          "version": "$VERSION",
          "file_name": "$FILE_NAME",
          "download_url": "$NV_URL",
          "install_command": "curl -fsSL ${SITE_BASE%/}/install/nv.sh | sh",
          "install_strategy": "unix-self-binary",
          "install_root": "$INSTALL_ROOT",
          "binary_name": "nv",
          "metadata": {"bootstrapPlatform": "$PLATFORM"},
          "update_command": "nv i nv",
          "update_policy": "nv-self"
        }
      },
      "installed_at": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
      "updated_at": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
      "install_root": "$INSTALL_ROOT"
    }
  }
}
EOF
fi
echo "Установлен или обновлён nv в $TARGET"

append_path_once() {
  rc_file="$1"
  [ -n "$rc_file" ] || return 0
  mkdir -p "$(dirname "$rc_file")"
  touch "$rc_file"
  if grep -F "NV installer PATH" "$rc_file" >/dev/null 2>&1 || grep -F "$INSTALL_ROOT" "$rc_file" >/dev/null 2>&1; then
    return 0
  fi
  {
    echo ""
    echo "# NV installer PATH"
    echo "case \":\$PATH:\" in"
    echo "  *\":$INSTALL_ROOT:\"*) ;;"
    echo "  *) export PATH=\"$INSTALL_ROOT:\$PATH\" ;;"
    echo "esac"
  } >> "$rc_file"
}

ensure_shell_path() {
  case ":$PATH:" in
    *":$INSTALL_ROOT:"*) return 0 ;;
  esac
  if [ "$IS_TERMUX" -eq 1 ] && [ -n "${PREFIX:-}" ] && [ "$INSTALL_ROOT" = "$PREFIX/bin" ]; then
    return 0
  fi
  append_path_once "$HOME/.profile"
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    bash) append_path_once "$HOME/.bashrc" ;;
    zsh) append_path_once "$HOME/.zshrc" ;;
    fish)
      fish_config="$HOME/.config/fish/config.fish"
      mkdir -p "$(dirname "$fish_config")"
      touch "$fish_config"
      if ! grep -F "$INSTALL_ROOT" "$fish_config" >/dev/null 2>&1; then
        printf '\n# NV installer PATH\nfish_add_path %s\n' "$INSTALL_ROOT" >> "$fish_config"
      fi
      ;;
  esac
}

ensure_shell_path
if FOUND_NV="$(command -v nv 2>/dev/null || true)" && [ -n "$FOUND_NV" ] && [ "$FOUND_NV" != "$TARGET" ]; then
  echo "PATH сейчас находит другой nv: $FOUND_NV" >&2
  echo "Открой новую сессию или выполни: hash -r" >&2
fi
case ":$PATH:" in
  *":$INSTALL_ROOT:"*) ;;
  *) echo "PATH прописан в shell config. Открой новую консоль или выполни: export PATH=\"$INSTALL_ROOT:\$PATH\"" ;;
esac
