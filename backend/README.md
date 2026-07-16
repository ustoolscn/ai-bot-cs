# AI Bot Backend

Go 单进程后端，包含管理 API、QQ Webhook、知识索引、AI 和消息投递 Worker。

## 本地运行

1. 启动带 `vector` 扩展的 PostgreSQL。
2. 复制 `.env.example` 中的变量到运行环境。
3. 使用 `go run ./cmd/server` 启动。首次启动自动迁移数据库并创建管理员。

默认管理员仅用于本地开发，生产环境必须修改密码和 `APP_MASTER_KEY`。

QQ 回调地址为 `/callbacks/qq/{botId}`。模型 API 的 `baseUrl` 应包含 OpenAI 兼容的 `/v1` 前缀。

`DEFAULT_CONTEXT_LIMIT`、`AI_REQUEST_TIMEOUT` 和 `MESSAGE_RETENTION_DAYS` 只用于首次初始化运行时设置；之后可通过管理端系统设置动态修改，数据库配置不会在重启时被环境变量覆盖。

## 测试

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
go test ./...
```

配置专用测试数据库后，可同时验证迁移、pgvector 距离检索和 `FOR UPDATE SKIP LOCKED`：

```powershell
$env:TEST_DATABASE_URL='postgres://ai_bot:password@localhost:5432/ai_bot_test?sslmode=disable'
go test ./internal/db -run TestPostgresPgvectorAndSkipLocked -v
```
