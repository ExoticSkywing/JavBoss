# JavBoss 开发者说明

## 开发环境依赖

- Go `1.25.1` 或更高版本
- Node.js `^20.19.0` 或 `>=22.12.0` 和 npm（推荐使用 `.nvmrc` 中的 Node 24）
- C/C++ 编译器（SQLite 驱动依赖 CGO；Linux/macOS 使用 `cc`，Windows 使用 `gcc`）

## 技术栈

- Backend: Go + Gin + GORM + SQLite
- Frontend: React + Vite + Tailwind + Zustand
- 媒体探测: `ffprobe`
- 缩略图截图生成: macOS 使用 `ffmpeg`，其他平台使用 `mpv`
- 播放与手动截图: `mpv`

## 第一次初始化

如果使用 nvm，先切换到仓库固定的 Node 版本：

```bash
nvm install
nvm use
```

然后在仓库根目录执行：

```bash
./scripts/cli.sh setup
```

`setup` 会检查工具链，并行下载 Go/前端依赖，再下载当前平台所需的
`ffprobe`、`mpv`（macOS 还包括 `ffmpeg`）。重复执行是安全的，已经就绪的依赖不会重复下载。

只想安装编译依赖、不下载媒体工具时：

```bash
SKIP_RUNTIME_DOWNLOAD=1 ./scripts/cli.sh setup
```

## 日常开发：改完立即看到结果

同时启动后端和前端：

```bash
./scripts/cli.sh dev both
```

- 前端运行在 <http://localhost:5173>，Vite 提供组件级热更新。
- 后端运行在 <http://localhost:17654>。保存 `.go`、`go.mod` 或 `go.sum` 后，开发器使用
  `.gocache/` 做增量编译；只有新版本编译成功才替换并重启旧进程。编译失败时旧服务继续可用，修复后再次保存即可。
- 临时产物写入 `.dev/`，不会污染 Git。

也可以只启动一侧：

```bash
./scripts/cli.sh dev backend
./scripts/cli.sh dev frontend
```

如需排查自动重载本身，可关闭它并恢复为一次性 `go run`：

```bash
NO_RELOAD=1 ./scripts/cli.sh dev backend
```

## 快速测试与提交前验证

修复后端 bug 时优先只测试受影响的包：

```bash
./scripts/cli.sh test backend ./internal/server
./scripts/cli.sh test backend ./internal/server -run TestName
```

前端单元测试通常很快，可以直接全跑：

```bash
./scripts/cli.sh test frontend
```

需要同时执行全部 Go 和前端单元测试时：

```bash
./scripts/cli.sh test all
```

准备提交前执行完整验证（Go 格式、vet、测试，以及前端测试、lint、格式和生产构建）：

```bash
./scripts/cli.sh check
```

也可只检查一侧：

```bash
./scripts/cli.sh check backend
./scripts/cli.sh check frontend
```

推荐节奏是：保存代码由热重载验证能否编译和启动；修 bug 时运行包级测试；提交前再运行一次
`./scripts/cli.sh check`。不要在每次保存时执行 lint 和生产构建。

## Docker 模式调试

按 Docker 运行时配置启动本地后端（用于调试容器模式行为）：

```bash
DOCKER_MODE=1 ./scripts/cli.sh dev backend
```

该模式会启用 `JAVBOSS_CONTAINER=1`，禁用 API token、目录选择器、桌面集成和 mpv 播放，并使用 ffmpeg 生成截图。需要本机可通过 `FFMPEG_PATH`、`internal/bin/ffmpeg` 或系统 `PATH` 找到 `ffmpeg`。本地调试默认不会把前端输入的目录自动加上 `/host` 前缀，也不会把 `127.0.0.1` 代理改写为 `host.docker.internal`；如需测试 Docker 宿主机路径映射，可使用 `DOCKER_MODE=1 JAVBOSS_HOST_PATH_PREFIX=1 ./scripts/cli.sh dev backend`，如需测试 Docker 代理网关映射，可额外设置 `JAVBOSS_PROXY_HOST_GATEWAY=1`。

## 下载指定平台依赖与打包

手动下载指定平台依赖：

```bash
./scripts/cli.sh download-dependencies linux-x86_64
```

打包发布：

```bash
scripts/cli.sh release linux-x86_64 v0.1.0
```

运行 `./scripts/cli.sh help` 可查看命令摘要。

## 项目结构

```text
cmd/server             Go 服务入口
cmd/javprovider        JAV 元数据 provider 调试入口
internal/common        全局状态与共享配置
internal/db            GORM 模型查询与 SQLite 存储
internal/jav           JAV 元数据与女优资料抓取
internal/manager       封面下载与截图任务
internal/models        数据模型定义
internal/mpv           mpv 播放、快捷键与手动截图配置
internal/server        HTTP API 与静态资源路由
internal/service       目录扫描、JAV 识别、资料补全
internal/util          文件、系统、代理、视频探测等工具
web/                   React + Tailwind 前端
scripts/cli            开发、依赖下载与发布辅助 CLI
data/                  运行期数据库、封面、缩略图与缓存
```
