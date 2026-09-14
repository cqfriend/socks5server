#!/bin/bash
# 交叉编译 socks5 服务端
# 用法:
#   ./build.sh            默认编译全部平台 + 本机版本
#   GARBLE=0 ./build.sh   关闭 garble 混淆编译
#   PLATFORMS="linux/amd64" ./build.sh   只编译指定平台
set -e

APP_NAME="socks5"
MAIN_PKG="."

# 版本信息
VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE} -X main.builtBy=build.sh"

# 可选：使用 garble 混淆编译（GARBLE=0 关闭）
BUILD_CMD="go build"
if [ "${GARBLE:-1}" != "0" ]; then
    GARBLE_BIN=""
    if command -v garble >/dev/null 2>&1; then
        GARBLE_BIN="garble"
    elif [ -x "$HOME/go/bin/garble" ]; then
        GARBLE_BIN="$HOME/go/bin/garble"
    fi
    if [ -n "$GARBLE_BIN" ]; then
        BUILD_CMD="${GARBLE_BIN} -literals -tiny build"
        echo "🔒 检测到 garble，启用混淆编译（GARBLE=0 可关闭）"
    else
        echo "💡 未检测到 garble，使用标准 go build"
    fi
fi

PLATFORMS=${PLATFORMS:-"linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"}

echo ""
echo "🚀 构建 ${APP_NAME} (${VERSION}, commit ${COMMIT})"

# 清理上次的产物
rm -rf dist
mkdir -p dist

for PLATFORM in ${PLATFORMS}; do
    GOOS=${PLATFORM%/*}
    GOARCH=${PLATFORM#*/}
    SUFFIX=""
    if [ "${GOOS}" = "windows" ]; then
        SUFFIX=".exe"
    fi

    OUT_DIR="dist/${APP_NAME}_${GOOS}_${GOARCH}"
    OUT="${OUT_DIR}/${APP_NAME}${SUFFIX}"
    mkdir -p "${OUT_DIR}"

    echo "  • ${GOOS}/${GOARCH}"
    CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} ${BUILD_CMD} -trimpath -ldflags "${LDFLAGS}" -o "${OUT}" "${MAIN_PKG}"
done

# 本机版本，方便直接运行
echo "  • 本机 $(go env GOOS)/$(go env GOARCH)"
${BUILD_CMD} -trimpath -ldflags "${LDFLAGS}" -o "${APP_NAME}" "${MAIN_PKG}"

echo ""
echo "✅ 构建完成"
echo ""
find dist -type f -exec ls -lh {} \;
ls -lh "${APP_NAME}"
echo ""
echo "本机可直接运行: ./${APP_NAME} -h"
