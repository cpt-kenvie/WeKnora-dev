#!/bin/bash
# Air 构建入口，Windows 通过 Git Bash 执行以复用现有版本脚本。
set -e

# 第一个参数指定构建产物，Windows 配置需传入带 .exe 后缀的路径。
OUTPUT_PATH="${1:-./tmp/main}"
# 后端入口包，相对于项目根目录。
MAIN_PACKAGE="./cmd/server"
# Windows 临时链接文件被占用时，等待一秒后重试一次；其他构建错误直接返回。
WINDOWS_LINK_RETRY_DELAY=1

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

LDFLAGS="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
BUILD_ARGS=(-tags "${GO_BUILD_TAGS:-}" -ldflags="$LDFLAGS" -o "$OUTPUT_PATH" "$MAIN_PACKAGE")
BUILD_LOG="$(mktemp)"
trap 'rm -f -- "$BUILD_LOG"' EXIT

if go build "${BUILD_ARGS[@]}" 2>"$BUILD_LOG"; then
    cat "$BUILD_LOG" >&2
    exit 0
else
    BUILD_STATUS=$?
fi
cat "$BUILD_LOG" >&2

case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
        if grep -Eq '^go: unlinkat .*a\.out\.exe: Access is denied\.$' "$BUILD_LOG"; then
            printf 'Windows 临时链接文件被占用，等待后重试构建一次。\n' >&2
            sleep "$WINDOWS_LINK_RETRY_DELAY"
            # 保持相同参数以复用已完成的链接缓存，仍以 Go 的成功退出作为验收条件。
            go build "${BUILD_ARGS[@]}"
            exit 0
        fi
        ;;
esac
exit "$BUILD_STATUS"
