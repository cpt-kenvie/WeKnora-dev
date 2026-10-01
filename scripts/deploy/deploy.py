#!/usr/bin/env python3
"""将本地已构建镜像打包，通过免密 SSH 限额部署到现有 Docker Compose 环境。"""

import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import signal
import subprocess
import tarfile
import time

# 默认沿用现有部署目录；仅重建应用服务，保留数据卷和依赖服务。
DEFAULT_REMOTE_DIR = "/opt/WeKnora"
# 导入镜像与发布脚本的 CPU、内存、磁盘 IO 限额。
WORKER_CPU = "50%"
WORKER_MEMORY = "512M"
IMPORT_MEMORY_HIGH = "384M"
IO_WEIGHT = "20"
# 应用启动健康检查超时；失败后自动切回备份的镜像与源码。
HEALTH_TIMEOUT = 120
# 镜像压缩使用低压缩级别，减少本地 CPU 开销。
COMPRESSION_LEVEL = 1
# 免密登录并校验已知主机指纹，避免脚本等待密码输入。
SSH_OPTIONS = ["-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=yes"]
REPOSITORY = Path(__file__).resolve().parents[2]


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def capture(args, **kwargs):
    return subprocess.check_output(args, text=True, encoding="utf-8", **kwargs).strip()


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def normalized_digest(data):
    return hashlib.sha256(data.decode("utf-8", errors="strict").replace("\r\n", "\n").encode("utf-8")).hexdigest()


def safe_relative(name):
    path = PurePosixPath(name)
    if path.is_absolute() or not path.parts or ".." in path.parts or "\\" in name:
        raise ValueError("源码路径必须是仓库内的相对路径: " + name)
    return path


def atomic_write(path, data):
    temporary = path.with_name(path.name + ".release-tmp")
    temporary.write_bytes(data)
    if path.exists():
        shutil.copystat(path, temporary)
    os.replace(temporary, path)


def replace_images(original, replacements):
    # 只替换目标服务的 image 行，保留现有环境变量、资源限额、注释和换行。
    text = original.decode("utf-8", errors="strict")
    for service, image in replacements.items():
        pattern = re.compile(r"(?m)(^  " + re.escape(service) + r":\r?\n(?:(?!^  \S)[^\n]*\n)*?^    image: )([^\r\n]+)")
        text, count = pattern.subn(lambda match: match[1] + image, text)
        if count != 1:
            raise ValueError("override 中必须有唯一的服务镜像配置: " + service)
    return text.encode("utf-8")


def prepare(args):
    release = args.output.resolve()
    release.mkdir(parents=True, exist_ok=False)
    images = {"app": args.app_image}
    if args.frontend_image:
        images["frontend"] = args.frontend_image
    for image in images.values():
        run(["docker", "image", "inspect", image], stdout=subprocess.DEVNULL)
    package = release / "images.tar.gz"
    print("正在本地打包镜像……", flush=True)
    process = subprocess.Popen(["docker", "save", *images.values()], stdout=subprocess.PIPE)
    try:
        with gzip.open(package, "wb", compresslevel=COMPRESSION_LEVEL) as target:
            shutil.copyfileobj(process.stdout, target, length=1024 * 1024)
        if process.wait() != 0:
            raise RuntimeError("docker save 失败")
    finally:
        process.stdout.close()
        if process.poll() is None:
            process.terminate()
            process.wait()

    names = set(args.source_file)
    previous_files = {}
    if args.source_manifest:
        previous = json.loads(args.source_manifest.read_text(encoding="utf-8"))
        if previous["root"] != args.remote_dir:
            raise ValueError("上次发布清单的部署目录不匹配")
        previous_files = {entry["path"]: entry["after"] for entry in previous["files"]}
        names.update(previous_files)
    if args.source_base:
        names.update(capture(["git", "diff", "--name-only", "--diff-filter=ACM", args.source_base], cwd=REPOSITORY).splitlines())
    manifest_files = []
    with tarfile.open(release / "source.tar.gz", "w:gz", compresslevel=COMPRESSION_LEVEL) as archive:
        for name in sorted(names):
            safe_relative(name)
            path = REPOSITORY / name
            if not path.is_file() or path.is_symlink() or not path.resolve().is_relative_to(REPOSITORY):
                raise ValueError("源码文件不可用: " + name)
            data = path.read_bytes()
            data.decode("utf-8", errors="strict")
            before = None
            if args.source_base:
                exists = subprocess.run(["git", "cat-file", "-e", args.source_base + ":" + name], cwd=REPOSITORY, stderr=subprocess.DEVNULL)
                if exists.returncode == 0:
                    before = normalized_digest(subprocess.check_output(["git", "show", args.source_base + ":" + name], cwd=REPOSITORY))
            # 连续发布尚未提交的修复时，以已成功发布的文件哈希为基线。
            if name in previous_files:
                before = previous_files[name]
            archive.add(path, arcname=name, recursive=False)
            manifest_files.append({"path": name, "before": before, "after": hashlib.sha256(data).hexdigest()})
    shutil.copyfile(__file__, release / "deploy.py")
    manifest = {"root": args.remote_dir, "images": images, "files": manifest_files,
                "image_ids": {service: capture(["docker", "image", "inspect", image, "--format", "{{.Id}}"])
                              for service, image in images.items()},
                "sha256": {name: digest(release / name) for name in ("images.tar.gz", "source.tar.gz", "deploy.py")}}
    (release / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return release


def deploy(args):
    if not re.fullmatch(r"[A-Za-z0-9_.@:-]+", args.host) or args.host.startswith("-"):
        raise ValueError("SSH 主机格式不合法")
    if not re.fullmatch(r"/[A-Za-z0-9_./-]+", args.remote_dir) or ".." in PurePosixPath(args.remote_dir).parts:
        raise ValueError("远程目录必须是无空格的绝对路径")
    release = prepare(args)
    release_id = time.strftime("%Y%m%d-%H%M%S") + "-" + digest(release / "manifest.json")[:8]
    remote = str(PurePosixPath(args.remote_dir).parent / ("weknora-release-" + release_id))
    ssh = ["ssh", *SSH_OPTIONS, args.host]
    run([*ssh, "mkdir -m 700 -- " + shlex.quote(remote)])
    for name in ("deploy.py", "manifest.json", "source.tar.gz", "images.tar.gz"):
        run(["scp", *SSH_OPTIONS, str(release / name), args.host + ":" + remote + "/"])
    command = ["systemd-run", "--unit=weknora-release-" + release_id,
               "--property=CPUQuota=" + WORKER_CPU, "--property=MemoryMax=" + WORKER_MEMORY,
               "--property=IOWeight=" + IO_WEIGHT, "--property=Nice=15",
               "--property=StandardOutput=append:" + remote + "/deploy.log",
               "--property=StandardError=append:" + remote + "/deploy.log",
               "/usr/bin/python3", "-u", remote + "/deploy.py", "worker", "--release-dir", remote]
    run([*ssh, shlex.join(command)])
    print("发布已在服务器后台启动，断开 SSH 不影响执行。日志: " + remote + "/deploy.log", flush=True)
    print("备份目录: " + remote + "/backup", flush=True)
    last_status = None
    while True:
        unit = "weknora-release-" + release_id + ".service"
        status = capture([*ssh, "if test -f " + shlex.quote(remote + "/result.json") + "; then cat " + shlex.quote(remote + "/result.json") + "; elif systemctl is-active --quiet " + shlex.quote(unit) + "; then tail -n 3 " + shlex.quote(remote + "/deploy.log") + "; else printf '%s\\n' '{\"success\":false,\"error\":\"worker stopped without result; inspect systemd and daemon limits\"}'; fi"])
        if status != last_status:
            print(status, flush=True)
            last_status = status
        try:
            result = json.loads(status)
        except json.JSONDecodeError:
            result = None
        if isinstance(result, dict) and "success" in result:
            if not result["success"]:
                raise RuntimeError("发布失败，查看远端日志与回滚状态")
            break
        time.sleep(5)


def import_images(release):
    services = ("docker.service", "containerd.service")
    for service in services:
        for key in ("CPUQuotaPerSecUSec", "MemoryHigh"):
            if capture(["systemctl", "show", service, "-p", key, "--value"]) != "infinity":
                raise RuntimeError("守护进程已有资源配置，请先核对: " + service + " " + key)
        if capture(["systemctl", "show", service, "-p", "IOWeight", "--value"]) not in ("[not set]", "100"):
            raise RuntimeError("守护进程已有 IO 配置: " + service)
    try:
        for service in services:
            run(["systemctl", "set-property", "--runtime", service, "CPUQuota=" + WORKER_CPU,
                 "MemoryHigh=" + IMPORT_MEMORY_HIGH, "IOWeight=" + IO_WEIGHT])
        # gzip 与 docker 客户端受发布单元限制；镜像解包同时受守护进程限制。
        with gzip.open(release / "images.tar.gz", "rb") as source:
            process = subprocess.Popen(["docker", "load"], stdin=subprocess.PIPE)
            try:
                shutil.copyfileobj(source, process.stdin, length=1024 * 1024)
                process.stdin.close()
                if process.wait() != 0:
                    raise RuntimeError("镜像导入失败")
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait()
    finally:
        failures = []
        for service in services:
            result = subprocess.run(["systemctl", "set-property", "--runtime", service, "CPUQuota=", "MemoryHigh=infinity", "IOWeight=100"])
            if result.returncode:
                failures.append(service)
        if failures:
            raise RuntimeError("恢复守护进程资源配置失败: " + ", ".join(failures))


def compose(root, *args):
    return run(["docker", "compose", *args], cwd=root)


def wait_healthy(root):
    deadline = time.monotonic() + HEALTH_TIMEOUT
    while time.monotonic() < deadline:
        container = capture(["docker", "compose", "ps", "-q", "app"], cwd=root)
        if container:
            state = json.loads(capture(["docker", "inspect", container]))[0]["State"]
            if state.get("Health", {}).get("Status") == "healthy":
                return
            if state.get("OOMKilled") or state["Status"] in ("restarting", "exited", "dead"):
                raise RuntimeError("应用启动失败: " + state["Status"])
        time.sleep(2)
    raise RuntimeError("应用健康检查超时")


def activate(release, manifest):
    root = Path(manifest["root"])
    if root.resolve() != root or not (root / "docker-compose.yml").is_file():
        raise ValueError("部署目录无效")
    config = root / "docker-compose.override.yml"
    original = config.read_bytes()
    updated = replace_images(original, manifest["images"])
    sources = {}
    expected = {entry["path"]: entry for entry in manifest["files"]}
    with tarfile.open(release / "source.tar.gz") as archive:
        members = archive.getmembers()
        if len(members) != len(expected) or {m.name for m in members} != set(expected):
            raise ValueError("源码清单不一致")
        for member in members:
            safe_relative(member.name)
            target = root / member.name
            if not member.isfile() or target.is_symlink() or not target.resolve().is_relative_to(root):
                raise ValueError("源码目标不合法")
            data = archive.extractfile(member).read()
            entry = expected[member.name]
            if hashlib.sha256(data).hexdigest() != entry["after"]:
                raise ValueError("源码校验失败: " + member.name)
            if entry["before"] is None:
                if target.exists():
                    raise ValueError("新增源码已存在: " + member.name)
            elif not target.exists() or entry["before"] not in (digest(target), normalized_digest(target.read_bytes())):
                raise ValueError("服务器源码与基线不同: " + member.name)
            sources[member.name] = data
    backup = release / "backup"
    backup.mkdir()
    shutil.copy2(config, backup / config.name)
    for name in sources:
        target = root / name
        if target.exists():
            saved = backup / name
            saved.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(target, saved)
    try:
        for name, data in sources.items():
            target = root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            atomic_write(target, data)
        atomic_write(config, updated)
        compose(root, "config", "-q")
        compose(root, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "app")
        wait_healthy(root)
        # 后端重建后刷新前端代理，确保解析到当前后端容器地址。
        compose(root, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--force-recreate", "frontend")
        frontend = capture(["docker", "compose", "ps", "-q", "frontend"], cwd=root)
        run(["docker", "exec", frontend, "wget", "-q", "-O", "/dev/null", "http://127.0.0.1/"])
    except BaseException:
        atomic_write(config, original)
        for name, data in sources.items():
            saved, target = backup / name, root / name
            if saved.exists():
                shutil.copy2(saved, target)
            elif target.exists() and target.read_bytes() == data:
                target.unlink()
        compose(root, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "app")
        wait_healthy(root)
        compose(root, "up", "-d", "--no-build", "--pull", "never", "--no-deps", "--force-recreate", "frontend")
        print("已回滚到发布前版本", flush=True)
        raise


def worker(args):
    import fcntl

    release = args.release_dir.resolve()
    result = {"success": False}
    def interrupted(signum, _frame):
        raise RuntimeError("发布被信号中断: " + str(signum))
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        with open("/run/weknora-release.lock", "w", encoding="utf-8") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            manifest = json.loads((release / "manifest.json").read_text(encoding="utf-8"))
            for name, expected in manifest["sha256"].items():
                safe_relative(name)
                if digest(release / name) != expected:
                    raise ValueError("发布文件校验失败: " + name)
            if shutil.disk_usage(release).free < max(3 * 1024**3, (release / "images.tar.gz").stat().st_size * 3):
                raise RuntimeError("服务器磁盘空间不足")
            import_images(release)
            for service, image in manifest["images"].items():
                if capture(["docker", "image", "inspect", image, "--format", "{{.Id}}"]) != manifest["image_ids"][service]:
                    raise ValueError("导入镜像 ID 不匹配")
            activate(release, manifest)
            result = {"success": True, "images": manifest["images"], "backup": str(release / "backup")}
            (release / "images.tar.gz").unlink()
    except BaseException as error:
        result["error"] = str(error)
        raise
    finally:
        atomic_write(release / "result.json", (json.dumps(result, ensure_ascii=False) + "\n").encode("utf-8"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    local = commands.add_parser("deploy", help="本地打包、上传并限额发布")
    local.add_argument("--host", required=True, help="免密 SSH 目标，例如 root@47.243.181.81")
    local.add_argument("--remote-dir", default=DEFAULT_REMOTE_DIR, help="现有 Compose 项目目录")
    local.add_argument("--app-image", required=True, help="已在本地构建并验证的后端镜像标签")
    local.add_argument("--frontend-image", help="可选：已在本地构建的前端镜像标签")
    local.add_argument("--output", required=True, type=Path, help="新的本地发布目录；不能已存在")
    local.add_argument("--source-base", help="服务器源码对应的本地 Git 提交；同步其后的已跟踪修改")
    local.add_argument("--source-manifest", type=Path, help="上次成功发布的本地 manifest.json；其文件哈希优先于 Git 基线")
    local.add_argument("--source-file", action="append", default=[], help="额外同步的新增源码相对路径，可重复")
    remote = commands.add_parser("worker", help="服务器后台执行，通常由 deploy 自动调用")
    remote.add_argument("--release-dir", type=Path, required=True, help="已上传的发布目录")
    args = parser.parse_args()
    if args.command == "deploy":
        deploy(args)
    else:
        worker(args)


if __name__ == "__main__":
    main()
