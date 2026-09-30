#!/bin/bash
# Air 构建入口，Windows 通过 Git Bash 执行以复用现有版本脚本。
set -e

# 第一个参数指定构建产物，Windows 配置需传入带 .exe 后缀的路径。
OUTPUT_PATH="${1:-./tmp/main}"
# 后端入口包，相对于项目根目录。
MAIN_PACKAGE="./cmd/server"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

LDFLAGS="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"
exec go build -tags "${GO_BUILD_TAGS:-}" -ldflags="$LDFLAGS" -o "$OUTPUT_PATH" "$MAIN_PACKAGE"
