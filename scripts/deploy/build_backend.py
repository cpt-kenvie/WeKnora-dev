#!/usr/bin/env python3
"""使用本地 Docker 限额测试、编译后端，并封装到兼容的运行镜像。"""

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import uuid

# 构建和测试共享限额，Go 按单包顺序编译。
DEFAULT_CPUS = "2"
DEFAULT_MEMORY = "4g"
# 当前图片问答修复的回归测试；调用方可指定其他测试表达式。
DEFAULT_TESTS = "Test(ImageOnly|QuestionInput|QuestionImage|QuickAnswer|ImageRecognitionError|TitleGeneration|Messages_|SanitizeGeneratedTitle|BuildSessionTitle|ConsumeFallbackStream|HandleAgentEventsForSSE)"
REPOSITORY = Path(__file__).resolve().parents[2]


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime-image", required=True, help="与当前源码兼容的本地 GNU/Linux 后端运行镜像")
    parser.add_argument("--tag", required=True, help="产出的本地镜像标签")
    parser.add_argument("--output", type=Path, required=True, help="新的构建输出目录，不能已存在")
    parser.add_argument("--go-volume", required=True, help="包含 go/bin/go 的本地 Docker SDK 卷")
    parser.add_argument("--module-cache", type=Path, required=True, help="本地 Go 模块缓存目录")
    parser.add_argument("--anydoc-lib", type=Path, required=True, help="本地 Linux amd64 GNU 版本的 libanydoc_go.a")
    parser.add_argument("--cpus", default=DEFAULT_CPUS, help="本地构建容器 CPU 上限")
    parser.add_argument("--memory", default=DEFAULT_MEMORY, help="本地构建容器内存和内存加交换区上限")
    parser.add_argument("--test-pattern", default=DEFAULT_TESTS, help="go test -run 表达式")
    args = parser.parse_args()
    output = args.output.resolve()
    if not args.anydoc_lib.is_file() or not args.module_cache.is_dir():
        parser.error("anydoc 静态库和模块缓存必须已在本地准备好")
    run(["docker", "volume", "inspect", args.go_volume], stdout=subprocess.DEVNULL)
    run(["docker", "image", "inspect", args.runtime_image], stdout=subprocess.DEVNULL)
    output.mkdir(parents=True, exist_ok=False)
    # 快照包含已跟踪文件的工作区版本和新 Go 源码，不读取 .env、数据卷或 node_modules。
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=REPOSITORY).decode("utf-8").split("\0")
    untracked = subprocess.check_output(["git", "ls-files", "--others", "--exclude-standard", "-z"], cwd=REPOSITORY).decode("utf-8").split("\0")
    names = set(tracked) | {name for name in untracked if name.endswith(".go")}
    with tarfile.open(output / "source.tar", "w") as archive:
        for name in sorted(names - {""}):
            path = REPOSITORY / name
            if path.is_file() and not path.is_symlink():
                archive.add(path, arcname=name, recursive=False)
    shutil.copyfile(args.anydoc_lib, output / "libanydoc_go.a")
    (output / "build.sh").write_text("""#!/bin/bash
set -euo pipefail
# 所有依赖安装、测试和编译均在本地限额容器内完成。
apt-get update -qq
apt-get install -y --no-install-recommends libsqlite3-dev
mkdir -p /work/app
cd /work/app
tar -xf /out/source.tar
go test -json -p 1 ./internal/models/api/openaicompletions ./internal/application/service/chat_pipeline ./internal/handler/session ./internal/application/service -run "$TEST_PATTERN" -count=1 -timeout=120s > /out/tests.jsonl 2>&1
mkdir -p third_party/anydoc-go/lib/linux_amd64_gnu
cp /out/libanydoc_go.a third_party/anydoc-go/lib/linux_amd64_gnu/
go build -tags anydoc -ldflags="-w -s -X github.com/Tencent/WeKnora/internal/handler.Version=v0.8.2-custom -X github.com/Tencent/WeKnora/internal/handler.CommitID=$RELEASE_ID -X github.com/Tencent/WeKnora/internal/handler.BuildTime=$BUILD_TIME -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn" -o /out/WeKnora ./cmd/server
sha256sum /out/WeKnora
""", encoding="utf-8", newline="\n")
    name = "weknora-local-build-" + uuid.uuid4().hex[:10]
    try:
        run(["docker", "run", "--name", name, "--cpus", args.cpus, "--memory", args.memory,
             "--memory-swap", args.memory, "--pids-limit", "256", "--entrypoint", "/bin/bash",
             "-v", str(output) + ":/out", "-v", args.go_volume + ":/sdk:ro",
             "-v", str(args.module_cache.resolve()) + ":/gomod", "-v", "weknora-question-cache:/gocache",
             "-e", "GOMODCACHE=/gomod", "-e", "GOCACHE=/gocache", "-e", "GOMAXPROCS=2",
             "-e", "GOMEMLIMIT=3GiB", "-e", "GOFLAGS=-p=1", "-e", "GOTOOLCHAIN=local",
             "-e", "PATH=/sdk/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
             "-e", "TEST_PATTERN=" + args.test_pattern, "-e", "RELEASE_ID=" + args.tag.rsplit(":", 1)[-1],
             "-e", "BUILD_TIME=" + datetime.now(timezone.utc).isoformat(), args.runtime_image, "/out/build.sh"])
    finally:
        subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # 使用镜像内词典路径，不依赖本地编译时的模块目录。
    (output / "Dockerfile").write_text(
        "FROM " + args.runtime_image + "\n"
        'ENV JIEBA_DICT_DIR=/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict\n'
        'COPY --chown=appuser:appuser WeKnora /app/WeKnora\n', encoding="utf-8", newline="\n")
    (output / ".dockerignore").write_text("**\n!Dockerfile\n!WeKnora\n", encoding="utf-8", newline="\n")
    run(["docker", "build", "--network", "none", "--pull=false", "-t", args.tag, str(output)])
    print(json.dumps({"image": args.tag, "output": str(output)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
