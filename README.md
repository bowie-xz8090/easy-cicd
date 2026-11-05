# 自动部署平台

一个轻量级的 Web 部署平台，通过浏览器页面一键触发项目的构建与部署。支持从 Gitea 拉取代码，自动构建，并通过 SSH 密码认证将产物部署到不同环境的目标服务器。

## 功能特性

- **免登录使用** — 打开页面即可操作，无需账号密码
- **Gitea 集成** — 自动获取 Gitea 上的项目列表和分支列表
- **多环境、多目标部署** — 开发/测试可自动选唯一服务器，生产环境可由用户选择具体部署目标
- **真实组件选择** — 单体仓库直接选择项目，前端 Monorepo 再选择 `apps` 下的独立组件
- **SSH 密码认证** — 通过密码登录目标服务器，无需配置免密登录
- **自动化流程** — 代码拉取 → 构建 → 上传产物 → 执行部署脚本，全程自动
- **实时状态跟踪** — 页面每 5 秒轮询，实时展示部署进度
- **部署历史** — 记录所有部署操作，支持按项目和环境筛选，可查看执行日志

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go + Gin |
| 前端 | Vue 3 + Vite + TypeScript |
| 数据库 | SQLite（零配置，自动创建） |
| SSH 连接 | golang.org/x/crypto/ssh（密码认证） |
| 代码管理 | Gitea REST API |

## 环境要求

部署平台所在的机器需要安装以下软件：

| 软件 | 版本要求 | 用途 |
|------|----------|------|
| Go | 1.21+ | 编译后端 |
| Node.js | 18+ | 构建前端 |
| Git | 任意版本 | 部署时拉取项目代码 |
| GCC | 任意版本 | 编译 SQLite 驱动（CGO 依赖） |

> **注意**：目标服务器不需要安装任何额外软件，只需要能通过 SSH 登录即可。

## 快速开始

### 1. 获取代码

```bash
git clone <仓库地址>
cd auto-deploy-platform
```

### 2. 构建前端

```bash
cd web
npm install
npm run build
cd ..
```

构建产物会输出到 `web/dist/` 目录。

### 3. 编译后端

linux、macOS
```bash
CGO_ENABLED=1 go build -o auto-deploy-platform ./cmd/
```

windows
```bash
go run ./cmd/main.go
```

### 4. 创建配置文件

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，填入实际的 Gitea 地址、服务器信息等（详见下方配置说明）。

### 5. 启动服务

```bash
./auto-deploy-platform
```

打开浏览器访问 `http://localhost:8080` 即可使用。

## 配置说明

所有配置集中在一个 `config.yaml` 文件中，默认放在可执行文件同级目录。也可以通过环境变量指定路径。

OIDC 登记信息保存在服务端 `oidc` 段，包括 `issuer`、`discovery_url`、`client_id`、`client_secret` 和 `redirect_uri`。`discovery_url` 必须是 Issuer 的 `/.well-known/openid-configuration` 地址。整个段落不通过配置 API 返回或修改；Client Secret 也禁止单独 JSON 序列化。

`access.ip_whitelist` 为精确 IP 列表，实际地址仅在部署环境的 `config.yaml` 中维护，不在源码中固定，也不会自动扩大为整个网段。白名单优先免 SSO，右上角和部署记录显示完整 IP；其他来源必须通过 OIDC 登录，右上角及部署记录只显示脱敏手机号。OIDC 应用需允许 `openid phone` 范围，并在 ID Token 或 UserInfo 中披露 `phone_number`；带 `+86` 的号码会规范为国内 11 位手机号，无有效手机号不建立会话。

默认以连接来源确定 IP，不信任请求自带的代理头。确有反向代理时，仅在 `access.trusted_proxies` 中登记可信代理的精确 IP 或 CIDR；代理必须覆盖或可靠追加转发头。白名单及代理配置随配置重载生效，`access` 整段只在服务端 YAML 中维护，不能由配置 API 覆盖。

SSO 成功后建立 CICD 自己的 30 天固定会话，访问不续期，重启后仍有效，到期需重新发起 SSO。本期没有退出按钮和集中退出联动，不把身份平台 Token 放在浏览器中。会话 Cookie 使用 HttpOnly、SameSite=Lax，Secure 按已配置回调地址的协议决定，Cookie Path 为应用路径。OIDC 不可用时不会放行非白名单匿名请求；空白名单不会关闭认证。

所有业务 API（包括配置、历史和日志）同样要求白名单或有效会话，内部部署密码和配置管理 `admin_token` 继续独立校验。写请求还需携带 `X-CICD-Request: 1`；浏览器同源校验不通过会拒绝请求。运维脚本在非白名单来源调用配置 API 时，必须同时携带有效会话 Cookie、自定义请求头和 `admin_token`。

部署时的操作人由后端确定，SQLite 保存原始手机号或 IP 及来源 IP，业务接口只输出脱敏操作人。旧部署记录保留，操作人显示“—”。“最后 commit”仅在页面按 100 个 Unicode 字符折叠，通过“更多 / 收起”切换，存储内容不截断。

配置采用版本 2 模型，完整可运行示例见 `config.example.yaml`。核心关系如下：

```text
项目/真实组件 + 环境
  ├─ build：共享的构建命令与产物
  └─ deployments
       ├─ 目标 A → 服务器 A + 部署脚本 A
       └─ 目标 B → 服务器 B + 部署脚本 B
```

精简示例：

```yaml
config_version: 2

servers:
  prod-main:
    label: "生产主站"
    host: "192.168.1.30"
    port: 22
    user: root
    password: "change-me"
    deploy_path: "/root/temp"
  prod-region:
    label: "生产区域节点"
    host: "192.168.1.31"
    port: 22
    user: root
    password: "change-me"
    deploy_path: "/data/temp"

environments:
  prod:
    label: "生产环境"
    disabled: false
    servers: [prod-main, prod-region]
    links: { user_url: "", admin_url: "", extra: [] }

projects:
  my-backend:
    label: "后端服务"
    mode: single
    build_type: backend
    environments:
      prod:
        build:
          command: "mvn clean package -DskipTests -Pprod"
          artifacts:
            - { key: app, output: "./target/app.jar", upload_as: "app.jar" }
        deployments:
          prod-main:
            label: "生产主站"
            server: prod-main
            script: "systemctl restart my-backend"
          prod-region:
            label: "生产区域节点"
            server: prod-region
            script: "systemctl restart my-backend-region"

  my-frontend:
    label: "前端 Monorepo"
    mode: monorepo
    components:
      user-ui:
        label: "用户端"
        path: "apps/user-ui"
        build_type: frontend
        environments:
          prod:
            build:
              command: "pnpm build:prod --filter user-ui"
              artifacts:
                - { key: web, output: "./apps/user-ui/dist", upload_as: "user-ui.tar.gz" }
            deployments:
              prod-main:
                label: "生产主站"
                server: prod-main
                script: "tar -xzf /root/temp/user-ui.tar.gz -C /srv/user-ui"
```

### 配置项详解

#### server（服务配置）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `port` | int | `8080` | 平台 Web 服务监听端口 |
| `workspace` | string | `./workspace` | 代码克隆和构建的工作目录，需要有足够磁盘空间和写权限 |

#### gitea（Gitea 配置）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | 是 | Gitea 服务地址，如 `http://192.168.1.100:3000` |
| `token` | string | 是 | Gitea API Access Token，在 Gitea 的 `设置 → 应用 → 管理 Access Token` 中生成 |

**生成 Gitea Token 的步骤：**
1. 登录 Gitea → 点击右上角头像 → 设置
2. 左侧菜单选择「应用」
3. 在「管理 Access Token」中输入名称，点击「生成令牌」
4. 复制生成的 Token 填入配置文件

#### servers（服务器资源）

服务器只保存连接信息和上传目录，不再承担项目或组件的语义。`password` 不会通过配置管理 API 返回。

#### environments（环境配置）

`servers` 是该环境允许使用的服务器 key 列表。开发/测试环境通常配置一台，生产环境可配置多台；`disabled` 控制整个环境是否开放部署。

#### projects（项目与组件）

- `mode: single`：项目本身就是部署单元，直接配置 `environments`。
- `mode: monorepo`：在 `components` 中声明 `apps` 下真实、独立的组件；不得为了区分服务器脚本创建伪组件。
- `build`：归属“项目/组件 + 环境”，描述一次共享构建。
- `deployments`：归属具体环境，每个目标绑定一台服务器和一份部署脚本。
- 所有构建产物上传完成后，平台只执行一次所选目标的 `script`。

`projects` 下的 key 必须和 Gitea 仓库名完全一致。`components` 的 key 建议和 `apps` 目录名一致。

旧版 `sub_projects/env_overrides` 配置可以使用迁移命令转换：

```bash
go run ./cmd/config-migrate -config config.yaml -write
```

迁移会根据构建命令或产物路径中的 `apps/<name>` 合并别名，例如把仅用于区分服务器的 `mult-onlyone-ui` 合并回真实组件 `cde-onlyone-ui`。

> **安全提示**：配置文件中包含 Gitea Token、部署密码和服务器凭据，请限制 `config.yaml` 的文件权限。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `CONFIG_PATH` | `config.yaml`（当前目录） | 配置文件路径 |
| `DB_PATH` | `deploy.db`（当前目录） | SQLite 数据库文件路径 |
| `GIN_MODE` | `debug` | 设为 `release` 可关闭 Gin 的调试日志 |

## 部署到服务器

### 目录结构

建议在服务器上按以下结构部署：

```
/opt/auto-deploy-platform/
├── auto-deploy-platform          # 可执行文件
├── config.yaml                   # 配置文件（chmod 600）
├── deploy.db                     # SQLite 数据库（自动创建）
├── workspace/                    # 代码工作目录（自动创建）
└── web/
    └── dist/                     # 前端构建产物
        ├── index.html
        └── assets/
```

### 部署步骤

```bash
# 1. 在开发机上编译（也可以在服务器上编译）
CGO_ENABLED=1 go build -o auto-deploy-platform ./cmd/
cd web && npm install && npm run build && cd ..

# 2. 将文件传输到服务器
scp auto-deploy-platform root@your-server:/opt/auto-deploy-platform/
scp config.example.yaml root@your-server:/opt/auto-deploy-platform/config.yaml
scp -r web/dist root@your-server:/opt/auto-deploy-platform/web/dist

# 3. 在服务器上编辑配置
ssh root@your-server
cd /opt/auto-deploy-platform
vim config.yaml    # 填入实际的 Gitea 地址、服务器密码等

# 4. 设置配置文件权限（包含密码，仅所有者可读）
chmod 600 config.yaml

# 5. 启动
./auto-deploy-platform
```

### 使用 systemd 管理（推荐）

创建服务文件 `/etc/systemd/system/auto-deploy.service`：

```ini
[Unit]
Description=Auto Deploy Platform
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/auto-deploy-platform
ExecStart=/opt/auto-deploy-platform/auto-deploy-platform
Environment=GIN_MODE=release
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

管理命令：

```bash
# 加载配置
sudo systemctl daemon-reload

# 设置开机自启
sudo systemctl enable auto-deploy

# 启动服务
sudo systemctl start auto-deploy

# 查看状态
sudo systemctl status auto-deploy

# 查看日志
sudo journalctl -u auto-deploy -f

# 重启服务（修改配置后）
sudo systemctl restart auto-deploy
```

## Docker 部署（推荐）

使用 Docker 可以一键构建和部署，**目标服务器不需要安装 Go、Node.js 等编译环境，只需要有 Docker**。

### 部署方式对比

| 方式 | 需要传什么到服务器 | 服务器要求 | 适用场景 |
|------|-------------------|-----------|----------|
| **本地构建 + 传镜像** | 镜像包 + config.yaml + docker-compose.yml | 仅 Docker | 服务器无法联网或不想暴露源码 |
| **服务器上构建** | 整个项目源码 | Docker | 服务器能联网，图省事 |
| **镜像仓库** | config.yaml + docker-compose.yml | Docker + 能访问仓库 | 团队协作、CI/CD 流水线 |

---

### 方式一：本地构建镜像 + 离线部署（推荐）

**不需要把源码传到服务器**，只需要传 3 个文件。

#### 步骤 1：在开发机上构建并导出镜像

```bash
# 构建镜像（自动编译前端和后端，约 2-5 分钟）
docker build -t auto-deploy-platform .

# Linux/macOS：导出为压缩包（约 50-80MB）
docker save auto-deploy-platform | gzip > auto-deploy-platform.tar.gz
```

Windows PowerShell / CMD 默认没有 `gzip`，请导出为未压缩的 tar 包：

```powershell
docker build -t auto-deploy-platform .
docker save -o auto-deploy-platform.tar auto-deploy-platform:latest
```

#### 步骤 2：准备配置文件

```bash
cp config.example.yaml config.yaml
vim config.yaml    # 填入实际的 Gitea 地址、服务器密码等
```

#### 步骤 3：传输到目标服务器

只需要传这 3 个文件：

```
scp auto-deploy-platform.tar.gz root@your-server:/opt/deploy/   # Linux/macOS gzip 方式
# 或
scp auto-deploy-platform.tar root@your-server:/opt/deploy/      # Windows docker save -o 方式
scp config.yaml root@your-server:/opt/deploy/
scp docker-compose.yml root@your-server:/opt/deploy/
```

目标服务器上的文件结构：

```
/opt/deploy/
├── auto-deploy-platform.tar.gz   # 镜像包（Linux/macOS gzip 方式）
├── auto-deploy-platform.tar      # 镜像包（Windows docker save -o 方式，二选一）
├── config.yaml                    # 配置文件（含服务器密码，chmod 600）
└── docker-compose.yml             # Docker 启动配置
```

#### 步骤 4：在服务器上启动

```bash
cd /opt/deploy

# 导入镜像
docker load -i auto-deploy-platform.tar.gz
# 或
docker load -i auto-deploy-platform.tar

# 启动
docker compose up -d

# 查看日志确认启动成功
docker compose logs -f
```

打开浏览器访问 `http://服务器IP:8080` 即可使用。

#### 更新部署（镜像升级）

在开发机上重新构建并导出镜像后，传到服务器执行：

```bash
cd /opt/deploy

# 1. 导入新镜像
docker load -i auto-deploy-platform.tar.gz
# 或
docker load -i auto-deploy-platform.tar

# 2. 停止旧容器并用新镜像重启
docker compose down
docker compose up -d

# 3. 确认启动成功
docker compose logs -f
```

---

### 方式二：在服务器上构建（需要上传源码）

如果服务器能联网，可以直接在服务器上构建，但需要把整个项目源码传上去。

```bash
# 1. 将项目源码传到服务器
scp -r ./ root@your-server:/opt/deploy/

# 2. 在服务器上操作
ssh root@your-server
cd /opt/deploy

# 3. 准备配置文件
cp config.example.yaml config.yaml
vim config.yaml

# 4. 一键构建并启动
docker compose up -d --build

# 5. 构建完成后可以删除源码（镜像已包含所有内容）
# rm -rf cmd/ internal/ web/src/ go.mod go.sum tools.go
```

---

### 方式三：docker run 手动启动

不用 docker-compose，直接用 docker 命令：

```bash
# 运行容器
docker run -d \
  --name auto-deploy-platform \
  -p 8080:8080 \
  -v /opt/deploy/config.yaml:/app/config.yaml:ro \
  -v deploy-data:/app/data \
  -v deploy-workspace:/app/workspace \
  -e GIN_MODE=release \
  --restart unless-stopped \
  auto-deploy-platform
```

---

### Docker 数据持久化

| 挂载路径 | 说明 | 是否必须 |
|----------|------|----------|
| `/app/config.yaml` | 配置文件（Gitea 地址、服务器密码等） | **必须** |
| `/app/data/` | SQLite 数据库（部署历史记录） | 建议（否则容器重启后记录丢失） |
| `/app/workspace/` | 代码克隆和构建的工作目录 | 建议（避免每次重新克隆） |

### 常用 Docker 管理命令

```bash
# 查看运行状态
docker compose ps

# 查看实时日志
docker compose logs -f

# 重启服务
docker compose restart

# 停止并删除容器（数据卷保留）
docker compose down

# 停止并删除容器和数据卷（清除所有数据）
docker compose down -v

# 更新镜像（重新构建后）
docker compose down
docker compose up -d

# 重建并重启服务
docker compose up -d --build 
```

## 使用方式


启动后打开浏览器访问 `http://服务器IP:8080`。

### 部署操作（首页）

1. **选择项目/组件** — 单体项目在级联选择器第一层直接选中；Monorepo 在第二层选择真实组件
2. **选择分支或 Tag** — 选择部署单元后自动加载代码引用
3. **选择环境** — 选择 dev / sit / prod
4. **选择部署服务器** — 只有一个目标时自动选中，多个目标时必须手动选择
5. **点击「部署」** — 项目、代码引用、环境和目标完整后才可触发

点击部署后，页面会实时显示任务状态：

| 状态 | 含义 |
|------|------|
| 等待中 | 任务已创建，等待执行 |
| 拉取代码中 | 正在从 Gitea 克隆/拉取代码 |
| 构建中 | 正在执行构建命令 |
| 部署中 | 正在上传产物并执行部署脚本 |
| 成功 | 部署完成 |
| 失败 | 某个阶段出错，可查看日志排查 |

### 部署历史（/history）

- 查看所有历史部署记录
- 支持按项目名称和环境筛选
- 点击任意记录可展开查看详细执行日志

## 部署流程原理

当用户点击「部署」后，后台自动执行以下流程：

```
1. 拉取代码
   从 Gitea 克隆或拉取指定项目的指定分支到 workspace 目录

2. 执行构建
   执行所选“项目/组件 + 环境”的 build.command
   环境变量 BUILD_ENV 会被设置为 dev/sit/prod

3. 上传产物
   准备 build.artifacts，并将全部产物上传到所选服务器的 deploy_path

4. 执行部署脚本
   全部产物上传成功后，仅执行所选 deployment.script
```

任何一个阶段失败，任务会标记为「失败」并记录错误信息。

## 常见问题

### 启动时报 "config file not found"

确保 `config.yaml` 文件存在于当前工作目录，或通过 `CONFIG_PATH` 环境变量指定路径：

```bash
CONFIG_PATH=/etc/deploy/config.yaml ./auto-deploy-platform
```

### 启动时报 "gitea.url must not be empty"

`config.yaml` 中的 `gitea.url` 和 `gitea.token` 是必填项，不能为空。

### 页面上获取项目列表失败

检查 Gitea 配置是否正确：
- `gitea.url` 是否能从部署平台机器访问
- `gitea.token` 是否有效（在 Gitea 上测试：`curl -H "Authorization: token YOUR_TOKEN" http://your-gitea/api/v1/repos/search`）

### 部署失败：SSH connection failed

检查目标服务器配置：
- `host` 和 `port` 是否正确
- `user` 和 `password` 是否能正常 SSH 登录
- 目标服务器的 SSH 服务是否开启了密码认证（`/etc/ssh/sshd_config` 中 `PasswordAuthentication yes`）

### 部署失败：产物上传失败

检查目标服务器的 `deploy_path` 目录是否存在且有写权限：

```bash
ssh user@target-server "ls -la /opt/apps"
```

### 编译时报 CGO 相关错误

SQLite 驱动需要 CGO，确保安装了 GCC：

```bash
# macOS
xcode-select --install

# Ubuntu/Debian
sudo apt install gcc

# CentOS/RHEL
sudo yum install gcc
```

## 项目结构

```
auto-deploy-platform/
├── cmd/
│   └── main.go                 # 程序入口，组件初始化和注入
├── internal/
│   ├── api/                    # HTTP API 路由和处理函数
│   ├── builder/                # 代码拉取和构建执行器
│   ├── config/                 # YAML 配置加载和解析
│   ├── db/                     # SQLite 数据库操作
│   ├── deployer/               # SSH 密码认证部署器
│   ├── gitea/                  # Gitea REST API 客户端
│   └── task/                   # 部署任务管理和异步执行
├── web/                        # Vue 3 前端项目
│   ├── src/
│   │   ├── api/                # API 请求封装
│   │   ├── components/         # 通用组件（DeployStatus、LogViewer）
│   │   ├── views/              # 页面（DeployPage、HistoryPage）
│   │   └── router/             # 路由配置
│   └── dist/                   # 前端构建产物
├── config.example.yaml         # 配置文件示例
├── go.mod                      # Go 依赖管理
└── README.md                   # 本文档
```

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/health` | 健康检查 |
| GET | `/api/auth/session` | 查询当前访问模式、脱敏身份和本地会话到期时间 |
| GET | `/api/auth/oidc/login` | 发起 OIDC 登录，return_to 仅接受本站页面路径 |
| GET | `/api/auth/oidc/callback` | 已登记的 OIDC 授权回调 |
| GET | `/api/projects` | 获取 Gitea 项目列表 |
| GET | `/api/projects/:owner/:repo/branches` | 获取项目分支列表 |
| GET | `/api/projects/:owner/:repo/tags` | 获取项目 Tag 列表 |
| GET | `/api/environments?project=&component=` | 获取环境及部署目标 |
| POST | `/api/deploy` | 触发部署任务 |
| GET | `/api/deploy/:id/status` | 获取任务状态 |
| GET | `/api/deploy/:id/logs` | 获取任务日志 |
| GET | `/api/deploy/records` | 获取部署历史（支持筛选和分页） |
| GET | `/api/config/editor` | 获取安全可编辑的配置视图（需要 admin_token） |
| PUT | `/api/config/projects` | 更新项目、组件、构建与部署目标（需要 admin_token） |
| PUT | `/api/config/environments/:key` | 更新环境访问与服务器范围（需要 admin_token） |
| PUT | `/api/config` | 修改配置（需要 admin_token 鉴权） |
| POST | `/api/config/reload` | 从磁盘重新加载配置（需要 admin_token 鉴权） |
| POST | `/api/webhook/gitea` | Gitea Webhook 回调（push 事件自动触发部署） |

### 配置动态修改

平台支持在运行时动态修改配置，**无需重启容器或服务**，修改后立即生效。

所有配置管理接口先检查白名单或有效登录会话，再额外校验 `admin_token`，在 `config.yaml` 中设置：

```yaml
admin_token: "your-secure-random-string"
```

Web 配置管理页使用分区接口更新环境范围和项目规则，连接凭据不会返回浏览器。保存前会校验项目模式、环境、构建产物、部署目标和服务器关联；成功后同时更新内存与 `config.yaml`，新任务立即使用最新配置。

`PUT /api/config` 仍保留给运维脚本做完整配置替换。调用方必须提交完整的非敏感配置；Gitea Token、`admin_token`、已有服务器密码和环境部署密码保留服务端当前值，不接受 JSON 覆盖。这些凭据只在服务端 `config.yaml` 中维护。`server.port` 的修改需要重启进程才能生效。

#### 从磁盘重载配置（POST /api/config/reload）

如果直接编辑了 `config.yaml` 文件（例如通过 `docker exec` 或挂载卷修改），调用此接口让服务读取最新配置：

重载成功后只返回与 `/api/config/editor` 相同的可编辑配置视图，不返回 Gitea 配置、Token 或密码。Gitea Token 字段也禁止 JSON 序列化，避免配置对象被意外返回时泄露凭据。

```bash
curl -X POST http://localhost:8080/api/config/reload \
  -H "X-CICD-Request: 1" \
  -H "Authorization: Bearer your-secure-random-string"
```

也可以通过 query 参数传递 token：

```bash
curl -X POST "http://localhost:8080/api/config/reload?token=your-secure-random-string" \
  -H "X-CICD-Request: 1"
```

> **安全说明**：未设置 `admin_token` 时接口返回 403 不可用；token 错误返回 401。`admin_token` 不会出现在任何 API 响应中。
