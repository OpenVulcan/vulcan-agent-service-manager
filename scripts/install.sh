#!/bin/sh
# Download, verify, and launch only the standalone vasm manager.
# 仅下载、校验并启动独立的 vasm 管理器。
set -eu

# SOURCE selects the canonical GitHub transfer or an explicit mirror proxy.
# SOURCE 选择权威 GitHub 传输或显式镜像代理。
SOURCE="${VASM_SOURCE:-github}"
# MIRROR_BASE is an optional HTTPS GitHub proxy prefix.
# MIRROR_BASE 是可选的 HTTPS GitHub 代理前缀。
MIRROR_BASE="${VASM_MIRROR_BASE:-https://gh-proxy.com}"
# VERSION is latest or a fixed manager release tag.
# VERSION 为 latest 或固定的管理器发布标签。
VERSION="${VASM_VERSION:-latest}"

# Parse only the documented bootstrap choices; service installation is handled by vasm.
# 仅解析已公开的引导选项；服务安装由 vasm 负责。
while [ "$#" -gt 0 ]; do
  case "$1" in
    --source) SOURCE="$2"; shift 2 ;;
    --mirror-base) MIRROR_BASE="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    *) echo "Unsupported bootstrap option: $1" >&2; exit 2 ;;
  esac
done

# Validate a tag before it enters a Release URL.
# 在标签进入发布地址前校验其格式。
case "$VERSION" in
  latest) ;;
  v*) if ! printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then echo "Invalid manager tag" >&2; exit 2; fi ;;
  *) echo "Invalid manager tag" >&2; exit 2 ;;
esac

# Map the running machine to one of the five published manager archives.
# 将当前机器映射到五种已发布管理器归档之一。
OS_NAME="$(uname -s)"
ARCH_NAME="$(uname -m)"
case "$OS_NAME/$ARCH_NAME" in
  Linux/x86_64|Linux/amd64) PLATFORM=linux-x64 ;;
  Linux/aarch64|Linux/arm64) PLATFORM=linux-arm64 ;;
  Darwin/x86_64) PLATFORM=macos-x64 ;;
  Darwin/arm64) PLATFORM=macos-arm64 ;;
  *) echo "Unsupported platform: $OS_NAME/$ARCH_NAME" >&2; exit 2 ;;
esac
ASSET="vasm-$PLATFORM.tar.gz"
RELEASE_BASE="https://github.com/OpenVulcan/vulcan-agent-service-manager/releases"
if [ "$VERSION" = latest ]; then
  OFFICIAL_URL="$RELEASE_BASE/latest/download/$ASSET"
else
  OFFICIAL_URL="$RELEASE_BASE/download/$VERSION/$ASSET"
fi

# A small checksum sidecar is fetched from GitHub even when the archive uses a proxy.
# 即使归档通过代理传输，也从 GitHub 获取体积很小的校验文件。
case "$SOURCE" in
  github) ARCHIVE_URL="$OFFICIAL_URL" ;;
  mirror)
    case "$MIRROR_BASE" in https://*) ;; *) echo "Mirror base must use HTTPS" >&2; exit 2 ;; esac
    ARCHIVE_URL="${MIRROR_BASE%/}/$OFFICIAL_URL"
    ;;
  *) echo "Source must be github or mirror" >&2; exit 2 ;;
esac

# Keep bootstrap files isolated and remove them on success or failure.
# 隔离引导文件，并在成功或失败时清理。
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM
curl -fLsS --retry 3 --connect-timeout 15 --max-time 60 --proto '=https' --proto-redir '=https' "$OFFICIAL_URL.sha256" -o "$TEMP_DIR/$ASSET.sha256"
curl -fLsS --retry 3 --connect-timeout 15 --max-time 900 --proto '=https' --proto-redir '=https' "$ARCHIVE_URL" -o "$TEMP_DIR/$ASSET"
EXPECTED="$(awk -v name="$ASSET" '$2 == name {print $1}' "$TEMP_DIR/$ASSET.sha256")"
if [ "${#EXPECTED}" -ne 64 ]; then
  echo "Invalid manager checksum sidecar" >&2
  exit 1
fi
case "$EXPECTED" in *[!0-9a-f]*) echo "Invalid manager checksum sidecar" >&2; exit 1 ;; esac
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TEMP_DIR/$ASSET" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TEMP_DIR/$ASSET" | awk '{print $1}')"
else
  echo "A SHA-256 utility is required" >&2
  exit 1
fi
if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Manager archive SHA-256 verification failed" >&2
  exit 1
fi
tar -xzf "$TEMP_DIR/$ASSET" -C "$TEMP_DIR"
SOURCE_BINARY="$TEMP_DIR/vasm-$PLATFORM/vasm"
if [ ! -f "$SOURCE_BINARY" ]; then
  echo "Manager archive layout is invalid" >&2
  exit 1
fi
SOURCE_VERSION="$("$SOURCE_BINARY" --version)"
if ! printf '%s\n' "$SOURCE_VERSION" | grep -Eq '^vasm [0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Downloaded manager failed its version smoke test" >&2
  exit 1
fi
if [ "$VERSION" != latest ] && [ "$SOURCE_VERSION" != "vasm ${VERSION#v}" ]; then
  echo "Downloaded manager version does not match the selected tag" >&2
  exit 1
fi

# Install a stable command before opening the interactive manager.
# 在打开交互管理器之前安装稳定命令入口。
COMMAND_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
mkdir -p "$COMMAND_DIR"
install -m 755 "$SOURCE_BINARY" "$COMMAND_DIR/.vasm-new-$$"
mv -f "$COMMAND_DIR/.vasm-new-$$" "$COMMAND_DIR/vasm"
echo "vasm installed at $COMMAND_DIR/vasm"
if [ ! -r /dev/tty ]; then
  echo "A terminal is required for the interactive installer; run $COMMAND_DIR/vasm later." >&2
  exit 1
fi
VASM_SOURCE="$SOURCE" VASM_MIRROR_BASE="$MIRROR_BASE" "$COMMAND_DIR/vasm" </dev/tty
