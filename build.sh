#!/bin/bash
# 原生 macOS Apple Silicon 打包；用法：./build.sh [version] 或 VERSION=1.0.0 ./build.sh
set -euo pipefail
cd "$(dirname "$0")"

if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
    echo "错误：请在 Apple Silicon Mac 的原生 arm64 终端运行，不支持 Intel。" >&2
    exit 1
fi
for tool in go sips iconutil codesign ditto xattr plutil; do
    command -v "$tool" >/dev/null || { echo "缺少工具：$tool" >&2; exit 1; }
done

VERSION=${1:-${VERSION:-$(git describe --tags --exact-match 2>/dev/null || echo dev)}}
# 限制为标签常用字符，避免破坏 linker 参数或 plist XML。
if [[ ! "$VERSION" =~ ^[a-zA-Z0-9][a-zA-Z0-9.+_-]*$ ]]; then
    echo "错误：版本只能包含字母、数字、点、加号、下划线和连字符。" >&2
    exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/apple-store-helper.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
APP="$WORK/Apple Store Helper.app"
ZIP="AppleStoreHelper-macos-arm64.zip"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$WORK/icon.iconset"

echo "打包 Apple Store Helper $VERSION (macOS arm64)..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build \
    -ldflags "-s -w -X apple-store-helper/common.VERSION=$VERSION" \
    -o "$APP/Contents/MacOS/apple-store-helper" .
for size in 16 32 128 256 512; do
    sips -z "$size" "$size" Icon.png --out "$WORK/icon.iconset/icon_${size}x${size}.png" >/dev/null
    sips -z "$((size * 2))" "$((size * 2))" Icon.png --out "$WORK/icon.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$WORK/icon.iconset" -o "$APP/Contents/Resources/icon.icns"
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
    <key>CFBundleName</key><string>Apple Store Helper</string>
    <key>CFBundleDisplayName</key><string>Apple Store Helper</string>
    <key>CFBundleExecutable</key><string>apple-store-helper</string>
    <key>CFBundleIdentifier</key><string>apple.store.helper</string>
    <key>CFBundleIconFile</key><string>icon.icns</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>$VERSION</string>
    <key>CFBundleVersion</key><string>1</string>
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
plutil -lint "$APP/Contents/Info.plist"
xattr -cr "$APP"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
ditto -c -k --keepParent "$APP" "$WORK/$ZIP"

# 不清空 dist，保留其他已有产物。
mkdir -p dist
ditto "$APP" "dist/Apple Store Helper.app"
cp "$WORK/$ZIP" "dist/$ZIP"
echo "完成：dist/Apple Store Helper.app"
echo "归档：dist/$ZIP"
echo "注意：使用本地 ad-hoc 签名，未经 Apple 公证。"
