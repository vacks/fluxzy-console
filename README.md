# FLUXZY · CONSOLE

自托管的 fluxzy 流量审计控制台。console 在单个容器内监管内置 fluxzy CLI，提供抓包索引、实时流量、会话历史、规则管理、启动参数配置、会话轮转、请求/响应体改写和规则热更新。

- 仓库：https://github.com/vacks/fluxzy-console
- 镜像：`ghcr.io/vacks/fluxzy-console`
- 当前版本：`1.0.0`

## 架构

```text
tini (PID 1)
└── /app/console (Go)
    ├── HTTP/API/WebSocket :8080
    ├── capture indexer / retention / rotation
    ├── runtime config + bundled CLI resolver
    └── fluxzy CLI start :443
        -d /data/sessions/<UTC-id>
        -r /config/rules.yaml
```

- fluxzy 由 console 启动、停止、重启和轮转。
- 每次启动或轮转都会创建新的 `/data/sessions/<UTC 时间戳>`，避免 exchange id 覆盖。
- console 负责 `rules.yaml`、采集索引、保留策略和进程生命周期。
- 生产镜像基于最小 Alpine，仅包含由 `fork/fluxzy` 构建的内置 CLI。

## 功能

- 用户名/密码登录；所有用户均为管理员权限。
- 实时流量、会话历史、请求/响应详情、Header/Body 预览（gzip/deflate/br、charset）。
- 热数据完整保留 + 冷数据确定性采样；保留策略可在线配置。
- 自动会话轮转：按时间、exchange 数、会话大小、磁盘剩余空间触发。
- 运行时页面：启动/停止/重启/轮转、PID/uptime/日志、启动参数配置。
- 规则编辑、校验、保存、应用；应用后热更新到运行中的 fluxzy。
- 按域名/路径匹配后改写请求体或响应体（Set / Replace / RegexReplace，带大小上限）。
- 多条件匹配：条件组支持 AND/OR 与嵌套分组，可在规则页可视化添加。
- 审计日志、Prometheus `/metrics`（默认仅允许回环地址访问）。
- 浅色/深色主题，中文界面。

## 快速开始（生产，linux/amd64）

```bash
mkdir -p config
curl -fsSL https://raw.githubusercontent.com/vacks/fluxzy-console/v1.0.0/docker-compose.yml -o docker-compose.yml
docker compose up -d
```

或者克隆仓库后直接启动：

```bash
git clone https://github.com/vacks/fluxzy-console.git
cd fluxzy-console
docker compose up -d
```

- 控制台：`http://<host>:8080`
- fluxzy：`https://<host>:443`，ReverseSecure 模式
- 首次启动自动创建管理员：**`admin / admin`**
- **首次登录后请立即在“设置 → 修改密码”中更换密码。**

`docker-compose.yml` 不包含任何应用配置环境变量。端口、路径、超时等由镜像内置默认值决定；fluxzy 启动参数、保留策略和轮转策略在 Web 界面修改并持久化到 `console.db`。

部署请使用全新的 `fluxzy-data` / `console-data` 卷。console 不执行自动数据库迁移；检测到不兼容的 schema 时会拒绝启动，请初始化新数据卷。

## 从源码构建

构建镜像需要 OrbStack/Docker，且 `fork/fluxzy/out/linux-amd64/` 中已有 patched CLI（`make docker-build` 会自动检查并构建）：

```bash
make docker-build
make up-build
```

镜像会同时打上 `ghcr.io/vacks/fluxzy-console:1.0.0` 和 `fluxzy-console:1.0.0` 标签。

## 开发环境（Apple Silicon / OrbStack）

```bash
HTTP_PROXY= HTTPS_PROXY= make dev-up

# 控制台 http://localhost:18080
# fluxzy https://localhost:443
# 默认管理员 admin / admin
```

开发栈使用 `docker-compose.dev.yml`，构建 `dev-arm64` target，数据卷与生产隔离。开发环境同样不使用应用配置环境变量。

开发数据：

```bash
make dev-reset   # 只删除本项目的 dev 卷，不碰生产卷
make dev-down    # 停止但保留卷
```

## 版本策略

`VERSION` 是唯一的版本来源，`Makefile` 会将其注入 Docker 构建。

- **主版本**：不兼容的 API、数据布局或运行方式变更；
- **次版本**：向后兼容的新功能；
- **修订版本**：向后兼容的问题修复和文案调整。

每次发布运行 `./scripts/bump-version.sh X.Y.Z`，它会同步 `VERSION`、Go 内置版本、前端版本、Dockerfile 和 compose 镜像标签。新增 `CHANGELOG.md` 条目后提交，创建 `vX.Y.Z` Git 标签并推送。GitHub Actions 会构建并推送对应镜像，同时创建 GitHub Release。

## 运行时控制

打开“运行时”页面：

- **启动 / 重启 / 轮转**：会中断在途请求并创建新会话；**停止**只中断在途请求，不创建新会话。
- **会话轮转**：重启 fluxzy 并创建新的会话目录；旧会话归档，之后按热/冷保留策略处理。轮转本身不会立即释放磁盘。
- **运行日志**：查看 fluxzy stdout/stderr 最近 300 行。
- **启动参数**：白名单结构化字段，保存后需要点击“应用并重启”才生效。

可配置的启动参数：

| 字段 | 说明 |
|---|---|
| `listenAddr` | 监听地址，默认 `0.0.0.0:443`；修改端口需要同步 Docker 端口映射 |
| `mode` | `ReverseSecure` / `ReversePlain` / `Regular` |
| `reversePort` | 反向代理上游端口 |
| `maxUpstreamConnections` | 每个上游 host 最大连接数 |
| `requestBuffer` | 请求缓冲字节数 |
| `maxCaptureCount` | 单会话最大 exchange 数；0 表示不限制 |
| `serveH2` / `parseUA` / `noCertCache` / `skipSSLDecryption` | 高级选项，按需开启 |

## 规则与热更新

规则保存与应用是两个独立动作：

1. **保存**：校验并写入 `/config/rules.yaml.pending`，不影响运行中的 fluxzy。
2. **保存并应用**：写入 `/config/rules.yaml` 并触发热更新；不会重启 fluxzy，也不会创建新会话。

规则支持多条件匹配：

- 条件组 `AND` / `OR`，可嵌套；
- 按域名、路径、方法、状态码、Header、Body 等过滤；
- 动作支持修改请求/响应 Header 和 Body（Set / Replace / RegexReplace）。

应用失败时 console 会恢复上一版规则并保留错误信息。热更新只影响新开始的请求；在途请求继续使用旧规则。

## 保留与轮转

- 热数据在热保留期内完整保留；到期后按确定性采样写入冷索引，再删除原始会话文件。
- 冷数据超过冷保留期后删除索引；聚合计数保留 90 天。
- 被 pin 的会话不会被自动清理；当前活动会话在轮转前不会进入清理流程。
- 自动轮转的默认值：时间不限制、exchange 数不限制、会话大小不限制；磁盘剩余低于 2 GB 时触发清理/轮转，低于 0.5 GB 时停止 fluxzy 保护磁盘。
- 磁盘阈值在 Web 界面以 GB 配置（1 GB = 1,000,000,000 字节）。

## 配置

应用不读取 `CONSOLE_*` / `FLUXZY_*` 环境变量，也不使用 `.env` 文件。

固定内置值：

| 项目 | 值 |
|---|---|
| 控制台监听 | `:8080` |
| fluxzy 监听 | `0.0.0.0:443`，`ReverseSecure` |
| console 数据目录 | `/var/lib/fluxzy-console` |
| fluxzy 数据目录 | `/data` |
| 配置文件目录 | `/config` |
| 内置 CLI | `/opt/fluxzy/fork/fluxzy` |
| 默认管理员 | `admin / admin` |
| 热保留 | 24 小时 |
| 冷保留 | 30 天 |
| 冷采样 | 5% |
| 实时推送上限 | 20 条/秒 |
| 单次响应体预览上限 | 8 MiB |

可在 Web 界面修改并持久化：fluxzy 启动参数、保留策略、轮转策略。其余部署级参数（监听端口、路径、反向代理头信任等）由镜像固定。

`/metrics` 默认只允许回环地址访问；如需从其他主机采集，请通过反向代理或后续版本提供的内置配置项开放。

## 数据目录

```text
/data/sessions/<session-id>/
  meta.json
  exchanges/*.mpack
  contents/*.data
  errors/errors.mpack

/config/
  rules.yaml          # 当前已被 CLI 加载的规则
  rules.yaml.pending  # 已保存但尚未应用的候选规则
  rules.yaml.applied  # 上次成功应用的规则快照

/var/lib/fluxzy-console/
  console.db
  index/hot-<session>.db
  index/cold-<session>.db
  exports/
  logs/
  fluxzy-home/        # 与 fluxzy 子进程共享的 cache HOME
  ca/rootca.pfx       # 自定义 CA（存在时覆盖内置 CA）
  tmp/

/opt/fluxzy/fork/
  fluxzy              # 内置 fluxzy CLI
  fork.json           # 上游版本、commit、patch 与构建 RID
  fluxzy.sha256       # 构建产物校验和
```

## fluxzy CLI 版本

当前内置 CLI 基于上游正式版 `1.39.52.38106`（commit `94dada2f431ddabb5ff4ac9be4eef607705ceec2`），包含两个 console 补丁：

- 声明式请求/响应体转换 Action；
- 规则文件热更新与原子状态文件。

“运行时”页面显示内置 CLI 的版本和来源。升级流程：

```bash
./fork/fluxzy/check-upstream.sh <version> --full
# 补丁通过、构建成功且规则测试通过后
make fork-build-amd64
make docker-build
```

`fork/fluxzy/upstream.env` 是 CLI 上游版本的唯一固定点，`build.sh`、Makefile 和发布工作流都从该文件读取；升级时同时更新 `deploy/Dockerfile` 中的 `FLUXZY_CLI_RELEASE` 默认值。

## API 摘要

除登录和初始化接口外，所有写操作需要登录 + CSRF；权限在服务端强制。所有用户均为管理员。

- `GET /api/v1/system/fluxzy`：进程/采集/配置/二进制状态。
- `POST /api/v1/system/fluxzy/start|stop|restart|rotate`：启动、停止、重启、轮转。
- `GET /api/v1/system/fluxzy/logs`：运行日志。
- `GET /api/v1/system/fluxzy/startup`：读取启动参数；`PUT` 保存，`POST /startup/apply` 应用并重启。
- `GET /api/v1/system/fluxzy/rotation`：读取轮转策略；`PUT` 保存。
- `POST /api/v1/rules/validate|save|apply|rollback`：校验、保存、热更新、回滚规则。
- `GET /api/v1/capture/...`：会话、交换、body、导出。
- `GET /api/v1/live`：WebSocket 实时事件。
- `GET/POST/PATCH/DELETE /api/v1/users`：用户管理。
- `GET /api/v1/audit`：审计日志。

## 故障排查

**fluxzy 未启动 / 443 不可达**

打开“运行时”查看进程状态、`lastError` 和运行日志。

**规则没有生效**

规则保存后需要在规则页点击“保存并应用”。如果应用失败，页面会显示错误；确认 fluxzy 正在运行后重试。

**升级后无法登录 / 数据库拒绝启动**

不兼容的旧数据卷会被拒绝。请备份后初始化新的 `fluxzy-data` / `console-data` 卷。

**磁盘持续增长**

- 检查“运行时 → 会话轮转”的磁盘阈值；
- 缩短热保留时间或降低冷采样比例；
- 活动会话只有轮转后才会进入清理流程；
- 轮转不会立即释放磁盘，旧会话仍需等待热保留到期或被手动删除。

**构建阶段代理连接失败**

如果宿主机 `HTTP_PROXY` 指向 `127.0.0.1:6152`，Docker 构建容器无法访问宿主 loopback：

```bash
HTTP_PROXY= HTTPS_PROXY= make docker-build
```

## 已知限制

- 单容器架构：console 重启/升级会同时中断 fluxzy；关键场景请使用外部双实例负载均衡方案（当前未实现）。
- 规则热更新只影响新开始的请求；在途请求继续使用旧规则。启动参数变更、CA 变更、CLI 升级仍会重启 fluxzy。
- 未实现双实例滚动升级、非 root 容器加固。
- 生产 amd64 使用 musl 自包含 CLI；Apple Silicon 上不建议用 amd64 仿真做性能验证。
- Docker healthcheck 使用 `/api/v1/healthz`（console liveness）；`/api/v1/readyz` 额外检查数据库、CLI 和 fluxzy 运行状态，适合外部监控。

## 许可

fluxzy CLI/Core 为 GPLv3。详见 `NOTICE.md` 与上游仓库：
https://github.com/haga-rak/fluxzy.core
