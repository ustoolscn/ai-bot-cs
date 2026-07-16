# QQ AI 机器人

一个面向 QQ 群与私聊的 AI 机器人 MVP。系统使用 QQ Webhook 接收事件，通过统一消息入口完成上下文、知识库和大模型处理，再由统一出口调用 QQ HTTP OpenAPI 回复。管理端提供机器人、模型、群策略、消息链路和知识库配置。

## 技术栈与结构

- 后端：Go、Gin、pgx，单进程运行管理 API、Webhook、AI/知识索引/出口 Worker。
- 管理端：Vue 3、TypeScript、Vite、Pinia、Element Plus。
- 数据库：PostgreSQL 18 + pgvector，统一保存业务数据、任务、知识分块与向量。生产环境直接安装在宿主机。
- 文件：TXT/Markdown 原文件默认保存在本地数据目录。

```text
QQ Webhook -> 统一入口 -> 上下文 / RAG / 大模型 -> 统一出口 -> QQ OpenAPI
                         ^
Vue 管理端 ------------> Go 管理 API ------------> PostgreSQL + pgvector
```

仓库目录：

```text
backend/                 Go 后端
frontend/                Vue 管理端
deploy/                  本地数据库与生产 Docker / Nginx 部署文件
.env.example             本地环境变量模板
dev.ps1                  Windows 开发入口
```

## 本地启动

前置要求：Go 和 pnpm。数据库可以使用 Docker Desktop，也可以使用项目提供的免管理员权限 Windows 原生 PostgreSQL 运行时。

1. 初始化本地配置：

   ```powershell
   ./dev.ps1 init
   ```

2. 编辑 `.env`，至少替换 `POSTGRES_PASSWORD`、`ADMIN_PASSWORD` 和 `APP_MASTER_KEY`。生成 Master Key 的示例：

   ```powershell
   $bytes = New-Object byte[] 32
   [Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
   [Convert]::ToBase64String($bytes)
   ```

3. 启动 PostgreSQL 18 + pgvector，二选一。

   Docker Desktop：

   ```powershell
   ./dev.ps1 infra-up
   ```

   Windows 原生运行时（从国内 GitHub 代理下载 PostgreSQL 18.4 + pgvector 0.8.3，并校验 SHA-256）：

   ```powershell
   ./dev.ps1 native-db-start
   ```

4. 分别打开两个终端运行后端和前端：

   ```powershell
   ./dev.ps1 backend
   ```

   ```powershell
   cd frontend
   pnpm install
   pnpm dev
   ```

5. 打开 `http://localhost:5173`，使用 `.env` 中的初始管理员账号登录。后端默认监听 `http://localhost:8080`。

前端在开发模式下会在后端不可用时展示示例数据，便于独立预览界面。生产构建默认关闭此回退；只有显式设置 `VITE_ENABLE_MOCK_FALLBACK=true` 才会启用，生产环境不要开启。

常用命令：

```powershell
./dev.ps1 doctor
./dev.ps1 infra-logs
./dev.ps1 infra-down
./dev.ps1 native-db-status
./dev.ps1 native-db-stop
```

`dev.ps1` 是本项目面向 Windows/PowerShell 的 Makefile 替代入口。所有命令也可以根据脚本内容手动执行。

## 国内镜像配置

仅在需要下载依赖时配置国内镜像；不要把个人代理、Token 或镜像凭据提交到仓库。

Go 模块：

```powershell
go env -w GOPROXY=https://goproxy.cn,direct
```

pnpm/npm：

```powershell
pnpm config set registry https://registry.npmmirror.com
```

本地开发数据库镜像可以在 `.env` 中覆盖：

```dotenv
PGVECTOR_IMAGE=<国内镜像代理域名>/pgvector/pgvector:pg18
```

生产 Dockerfile 默认通过 DaoCloud 镜像代理拉取 Node、Go 和 Alpine 基础镜像，并使用 `goproxy.cn`、npmmirror 与阿里云 Alpine 镜像下载依赖。所有地址都可以通过 Docker build args 覆盖，以便切换到组织内部镜像。

## 生产部署：单应用容器

生产环境只有 AI Bot 应用运行在 Docker 中。Vue 会在镜像构建阶段编译并内嵌进 Go 二进制；PostgreSQL + pgvector 和 Nginx 直接安装在宿主机：

```text
Internet -> 宿主机 Nginx(HTTPS) -> 127.0.0.1:8080 -> AI Bot 容器
                                                        |
                                                        v
                                             宿主机 PostgreSQL + pgvector
```

### 1. 准备宿主机 PostgreSQL

以 PostgreSQL 管理员身份创建用户、数据库和 pgvector 扩展：

```sql
CREATE ROLE ai_bot LOGIN PASSWORD '请替换为强密码';
CREATE DATABASE ai_bot OWNER ai_bot;
\connect ai_bot
CREATE EXTENSION IF NOT EXISTS vector;
```

应用容器固定使用 `172.30.0.0/24` 网段。在 `postgresql.conf` 中允许监听 Docker 网桥，并在 `pg_hba.conf` 中只授权该应用网段：

```conf
# postgresql.conf
listen_addresses = '*'
```

```conf
# pg_hba.conf
host  ai_bot  ai_bot  172.30.0.0/24  scram-sha-256
```

重载或重启 PostgreSQL 后，确认服务器防火墙没有向公网开放 5432。`pg_hba.conf` 和防火墙应同时限制数据库访问来源。

### 2. 从 GitHub Actions 获取镜像

工作流 [`.github/workflows/build-docker.yml`](.github/workflows/build-docker.yml) 会执行后端测试、前端测试与构建，然后生成 `linux/amd64` 镜像压缩包。它不会登录或推送任何 Docker Registry。

在 GitHub 仓库的 Actions 页面手动运行 `Build Docker artifact`，下载 Artifact 后上传到服务器并导入：

```bash
sha256sum -c ai-bot-linux-amd64.tar.gz.sha256
gzip -dc ai-bot-linux-amd64.tar.gz | docker load
docker image inspect ai-bot:latest >/dev/null
```

### 3. 启动单应用容器

复制生产环境模板并填写真实配置：

```bash
cd /opt/ai-bot
cp deploy/ai-bot.env.example ai-bot.env
chmod 600 ai-bot.env
```

其中：

- `DATABASE_URL` 的主机名保持 `host.docker.internal`，密码中若有 `@`、`:`、`/` 等保留字符必须进行 URL 编码。
- `APP_MASTER_KEY` 必须是随机 32 字节的 Base64，并长期备份；不要在已有数据后直接更换。
- `PUBLIC_BASE_URL` 填写真实 HTTPS 域名，`COOKIE_SECURE=true`。
- `ADMIN_PASSWORD` 只在数据库中尚无管理员时用于首次初始化。

启动：

```bash
docker compose --env-file ai-bot.env -f deploy/docker-compose.yml up -d
docker compose --env-file ai-bot.env -f deploy/docker-compose.yml ps
docker compose --env-file ai-bot.env -f deploy/docker-compose.yml logs -f app
```

Compose 只启动一个 `app` 容器，并只把端口发布到宿主机 `127.0.0.1:8080`。知识库原文件保存在 `app_data` Docker Volume 中，容器重建不会丢失。

### 4. 配置宿主机 Nginx

复制 [`deploy/nginx/ai-bot.conf.example`](deploy/nginx/ai-bot.conf.example) 到 Nginx 配置目录，将 `bot.example.com` 和证书路径替换为真实值：

```bash
sudo nginx -t
sudo systemctl reload nginx
```

Nginx 将管理端、`/api` 和 `/callbacks` 全部反向代理给同一个 Go 容器。模型测试最长可运行 10 分钟，因此示例配置的代理超时为 660 秒；上传限制为 12MB，应用内部仍限制 TXT/Markdown 不超过 10MB。

### 5. 升级与回滚

新版本构建完成后下载新的 Artifact，然后执行：

```bash
gzip -dc ai-bot-linux-amd64.tar.gz | docker load
docker compose --env-file ai-bot.env -f deploy/docker-compose.yml up -d --force-recreate app
```

升级前应备份 PostgreSQL 和 `app_data` 卷。回滚时重新 `docker load` 上一版镜像并重建容器；应用启动会自动执行幂等数据库迁移和恢复未完成任务。

## QQ 机器人配置

1. 在 QQ 开放平台创建并发布机器人，取得 AppID 和 AppSecret，并开通所需群聊/单聊事件权限。
2. 将后端部署到可被 QQ 平台访问的公网 HTTPS 地址。开发环境可使用可信的 HTTPS 反向代理或隧道，生产环境应由 Nginx/网关终止 TLS。
3. 登录管理端创建 QQ 机器人记录，填写 AppID、AppSecret 并保存；密钥只允许覆盖更新，不会回显明文。
4. 使用该记录在管理端显示的内部 `botId` 配置回调地址：

   ```text
   https://<你的域名>/callbacks/qq/<botId>
   ```

5. 在 QQ 开放平台完成回调地址验证并订阅至少以下事件：

   - `GROUP_AT_MESSAGE_CREATE`：群内 @机器人，触发回答。
   - `C2C_MESSAGE_CREATE`：单聊消息，触发回答。
   - `GROUP_MESSAGE_CREATE`：有权限时保存为上下文，默认不主动回答。

6. 在管理端配置一个 Chat 模型和一个 Embedding 模型。两者都使用 OpenAI 兼容 API，可分别设置 Base URL、API Key 和模型名。
7. 创建知识库、上传 `.txt` 或 `.md` 文件，等待索引完成后绑定到目标群。

QQ 回调必须快速返回：服务会先完成验签、幂等入库和 ACK，再由后台任务调用知识库与大模型。QQ 回复仍通过 HTTP OpenAPI 发送，不需要 WebSocket。

## 模型测试与索引排查

- 模型页面为 Chat 和 Embedding 分别提供“对话测试”和“向量测试”。测试结果会展示实际请求 endpoint、耗时、Token 或向量维度与预览。
- OpenAI 兼容服务的 Base URL 是否包含 `/v1` 取决于服务商；请以测试工作台展示的最终 endpoint 和服务商文档为准。
- Embedding 配置的向量维度会随请求发送，并校验上游实际返回维度，避免文档索引后才发现维度不一致。
- 文档索引失败时，知识库文档表会直接展示失败阶段和原因；完整错误可查看、复制并在修正配置后重试。

## 动态系统设置

管理端“系统设置”中的默认上下文数、AI 请求超时和消息保留天数保存在 PostgreSQL 的 `system_settings` 表中，保存后不需要重启服务：

- AI 请求超时会从下一次模型调用开始生效。
- 默认上下文数只应用于之后新发现的群或私聊，不覆盖已有会话的单独配置。
- 消息保留策略由后台维护任务约每分钟执行一次，同时清理对应的过期 Webhook 事件。

`DEFAULT_CONTEXT_LIMIT`、`AI_REQUEST_TIMEOUT` 和 `MESSAGE_RETENTION_DAYS` 环境变量只用于数据库尚未建立运行时设置时的首次初始化；后续重启不会覆盖管理端保存的值。

## 数据与安全

- `.env`、本地数据、上传文件、日志和构建产物均被 `.gitignore` 排除。
- `APP_MASTER_KEY` 用于加密 AppSecret/API Key；更换它之前必须先完成密钥轮换，否则已有密文将无法读取。
- 管理端会话使用 HttpOnly Cookie。公网 HTTPS 部署时必须设置 `COOKIE_SECURE=true`。
- 生产环境不要直接暴露 PostgreSQL 端口；Compose 的端口映射仅用于本地开发。
- 消息、模型调用和投递错误会记录到数据库，日志中不得输出完整 AccessToken、AppSecret 或 API Key。

## 运行检查

本地开发数据库健康状态：

```powershell
docker compose --env-file .env -f deploy/docker-compose.dev-db.yml ps
```

确认 pgvector 已启用：

```powershell
docker compose --env-file .env -f deploy/docker-compose.dev-db.yml exec postgres psql -U ai_bot -d ai_bot -c "SELECT extversion FROM pg_extension WHERE extname = 'vector';"
```

首次初始化脚本只会在新建数据卷时运行。若数据库卷已经存在，后端迁移仍应使用 `CREATE EXTENSION IF NOT EXISTS vector` 保证扩展可用。
