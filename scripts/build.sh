#!/usr/bin/env bash
# ImaHub 打包脚本：Windows x86_64 exe + Linux x86_64 + Linux arm64（可选 armv7）。
# 用法：bash scripts/build.sh [--all]   （默认只出三个主目标）
# 产物：dist/imahub_<版本>_<系统>_<架构>[.exe]
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(grep -oP 'const Version = "\K[^"]+' internal/api/api.go)"
OUT=dist
rm -rf "$OUT"   # 清掉旧版本产物，防新旧混杂
mkdir -p "$OUT"

build_one() {
  local goos="$1" goarch="$2" arm="${3:-}" name="$4"
  local ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  local out="$OUT/imahub_${VERSION}_${name}${ext}"
  echo "==> $out"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" ${arm:+GOARM=$arm} \
    go build -trimpath -ldflags "-s -w" -o "$out" .
}

build_one windows amd64 ""   windows-x86_64
build_one linux   amd64 ""   linux-x86_64
build_one linux   arm64 ""   linux-arm64
if [ "${1:-}" = "--all" ]; then
  build_one linux arm 7 linux-armv7
fi

echo
echo "打包完成："
ls -la "$OUT"
