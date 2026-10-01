# 本地构建与 SSH 部署

## 新 Windows 电脑：只需 Docker Desktop

`windows.ps1` 从当前源码构建应用、文档解析器和前端，然后启动本地服务。默认构建限额为 2 核 / 6 GiB，服务和 Dockerfile 阶段依次构建；结束或失败后移除专用构建容器，保留缓存。首次自动创建 `.env`、随机生成密码和密钥；以后原样保留该文件及数据卷。`compose.windows.yml` 提供独立本地镜像标签和常驻服务限额，`buildkitd.toml` 限制构建阶段并行。

环境要求：

- Windows PowerShell 5.1（Windows 自带）或 PowerShell 7。
- 较新的 Docker Desktop，已启动 Linux 容器引擎；包含 Compose（支持 `build --builder`）和 Buildx 0.14+。
- 建议电脑内存至少 16 GiB、Docker / WSL 分配至少 8 GiB，磁盘留出约 30 GiB；首次构建需要联网下载基础镜像和依赖。
- 无需在 Windows 安装 Go、Node.js、Python 或 Rust。Git 可选：可以下载完整仓库 ZIP 解压；使用 Git 时运行 `git clone -c core.autocrlf=false https://github.com/cpt-kenvie/WeKnora-dev.git`。

在代码根目录打开 PowerShell：

```powershell
# 默认：首次配置、限额构建、启动并检查健康状态
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1
```

`Bypass` 仅对本次 PowerShell 进程生效，不修改系统执行策略。脚本按自身位置寻找源码，也可以使用绝对路径从其他目录运行。ZIP 必须包含新增脚本和完整源码。

成功后访问 `http://localhost:8081`，注册账号并配置回答、视觉和向量模型。新部署使用本机数据库和文件存储，不导入现有服务器的数据；调用在线模型仍会使用你在界面中配置的模型服务。

| 参数 | 含义 |
| --- | --- |
| `-Action` | `Deploy`（默认，构建并启动）、`Build`（只构建）、`Start`（使用已有本地镜像启动）、`Stop`、`Status`、`Logs`（最近 100 行）、`Check`（初始化缺失的 `.env` 并检查环境和配置，不启动服务） |
| `-BuildCpus` | 构建总 CPU 限额及 Go/Rust 并行数，默认 2，范围 1–16 |
| `-BuildMemoryGB` | 构建内存及内存加交换区上限，默认 6，范围 4–64 GiB |
| `-FrontendPort` | 首次生成 `.env` 时的网页端口，默认 8081；已有配置需编辑其中的 `FRONTEND_PORT` |
| `-StartupTimeout` | 等待服务健康的秒数，默认 180，范围 60–1800 |

```powershell
# 先检查环境，不进行构建；仅首次创建缺失的 .env
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Check

# 只构建，保持 2 核 / 6 GiB 限额
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Build

# 启动、查看状态、查看日志、停止（保留数据）
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Start
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Status
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Logs
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\deploy\windows.ps1 -Action Stop
```

后续更新代码后重复运行默认命令即可。请始终通过此脚本操作这个部署，它固定使用 `weknora-windows` Compose 项目和 `compose.windows.yml`；直接使用仓库默认 Compose 命令会选择不同镜像或数据卷。已有其他 WeKnora 部署占用同名容器时应先核对，脚本不会删除其他部署。现有 `.env` 若指向远程数据库或存储，脚本也会原样保留；全新本地部署应从干净源码目录开始。

运行阶段的内存上限为 app 1 GiB、docreader 1 GiB、postgres 512 MiB、frontend/redis 各 128 MiB；大文档解析确需更多资源时，可调整 `compose.windows.yml`。构建使用单独的缓存卷，失败会返回非零退出码，修正网络或内存问题后重新执行即可。

## 已有环境：本地后端增量构建与 SSH 发布

`build_backend.py` 在本地 Docker 容器内安装依赖、运行回归测试、编译带 anydoc 的后端，并生成应用镜像。默认限制 2 CPU、4 GiB 内存，Go 单包编译；自动清理构建容器，保留日志及产物。

`deploy.py` 打包本地镜像，通过免密 SSH/SCP 上传并校验 SHA256；服务器只导入和运行，不安装编译依赖。导入期间限制发布进程和 Docker/containerd 的 CPU、内存高水位及 IO 权重，并恢复原设置。只更新应用和前端，保留现有 Compose 资源限制、配置、数据库和数据卷；健康检查失败自动回滚涉及的镜像配置与源码。

需要本地 Python 3.11+、Docker Linux 容器、Git、OpenSSH；服务器需要 Python 3.10+、systemd、Docker Compose、已配置健康检查的 app 服务，以及在 `docker-compose.override.yml` 中独立配置的 app/frontend 镜像。首次连接前应核对服务器指纹并加入 known_hosts，SSH 密钥需已配置。脚本不接受密码，也不保存凭证。

## 构建

已准备的 SDK 卷包含 `go/bin/go`；anydoc 静态库须与 Linux amd64 GNU 运行镜像匹配。以下参数复用当前机器的构建缓存：

```powershell
python -X utf8 scripts/deploy/build_backend.py --runtime-image weknora-custom/app:c28c3cc9 --tag weknora-custom/app:image-chat-fix --output C:/Users/admin/AppData/Local/Temp/weknora-image-chat-build --go-volume weknora-question-go --module-cache C:/Users/admin/go/pkg/mod --anydoc-lib C:/Users/admin/AppData/Local/Temp/weknora-question-cleanup-20261001/anydoc/libanydoc_go.a
```

构建参数：

| 参数 | 含义 |
| --- | --- |
| `--runtime-image` | 必填，兼容当前源码并含 GNU 编译依赖的本地运行镜像 |
| `--tag` | 必填，产出镜像标签 |
| `--output` | 必填，新的输出目录，已有目录会拒绝覆盖 |
| `--go-volume` | 必填，包含 Go SDK 的 Docker 卷 |
| `--module-cache` | 必填，本地 Go 模块缓存目录 |
| `--anydoc-lib` | 必填，已编译的 GNU anydoc 静态库 |
| `--cpus` | 本地容器 CPU 限额，默认 2 |
| `--memory` | 本地容器内存上限，默认 4g，禁止额外交换区 |
| `--test-pattern` | 本次构建需运行的 Go 回归测试表达式；默认覆盖图片、标题、流式结束等流程 |

源码快照包含工作区已跟踪文件及新增 `.go` 文件。更换大版本或运行依赖时，应先按项目 Dockerfile 在本地构建匹配的基础镜像；本脚本用于同一运行环境的后端更新。前端有修改时须先在本地构建前端镜像，再传给部署脚本。

## 部署

先在本地验证镜像启动与功能，再执行：

```powershell
python -X utf8 scripts/deploy/deploy.py deploy --host root@47.243.181.81 --remote-dir /opt/WeKnora --app-image weknora-custom/app:image-chat-fix --output C:/Users/admin/AppData/Local/Temp/weknora-image-chat-release
```

部署参数：

| 参数 | 含义 |
| --- | --- |
| `--host` | 必填，已配置免密登录的 SSH 目标 |
| `--remote-dir` | 现有 Compose 项目目录，默认 `/opt/WeKnora` |
| `--app-image` | 必填，已构建并验证的本地后端镜像 |
| `--frontend-image` | 可选，同时上传更新前端镜像；省略则沿用现有镜像 |
| `--output` | 必填，新的本地打包目录，保留发布包用于审查 |
| `--source-base` | 可选，服务器源码对应的 Git 提交；同步该提交之后的已跟踪修改 |
| `--source-manifest` | 可选，上次成功发布的本地 `manifest.json`；同步其中的文件，并优先使用其已发布哈希作为基线 |
| `--source-file` | 可重复，额外同步新增源码相对路径；校验目标尚不存在 |

例如在上次服务器源码对应 `d5a41990` 时，增加 `--source-base d5a41990 --source-file internal/handler/session/qa_title.go` 可同步已跟踪修改和指定新增文件。源码基线不匹配会拒绝覆盖。脚本不执行 Git commit/push，不自动删除旧镜像。

连续发布尚未提交的修复时，可同时传入 `--source-base d5a41990 --source-manifest C:/path/to/previous-release/manifest.json`；已发布文件使用上次清单，新修改的已跟踪文件使用 Git 基线。只使用已确认 `success: true` 的发布清单；回滚过的失败发布不能作为基线。

服务器任务运行在 `weknora-release-<时间>-<校验前缀>` systemd 单元。SSH 中断后不要重新部署同一个包，先查看输出的远程发布目录：`deploy.log`、`result.json`、`backup/`。`result.json` 中 `success: true` 才表示发布通过；失败时查看日志确认回滚是否完成。底层已有独立资源设置时脚本会拒绝修改，需要先核对配置。

`worker --release-dir <远程发布目录>` 是由部署入口自动启动的内部命令。验收后大型远端镜像包会删除，备份和日志保留。

## 脚本验证

`test_deploy.py` 使用临时目录和模拟命令验证镜像配置替换、路径约束与失败回滚，不连接服务器。

```powershell
python -X utf8 -m unittest discover -s scripts/deploy -p test_deploy.py
```

本次新增上述脚本：保留本地构建方式，并加入纯图片问答回归测试、词典路径配置、上传完整性验证、连续发布源码基线及受限部署流程。
