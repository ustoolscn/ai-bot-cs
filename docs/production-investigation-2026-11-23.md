# 生产问题排查与本地修复交接

任务：19709FAF-0035-439A-996B-72DC28B7B482。日志时间均为北京时间 2026-11-23。源码基线：92595c55f1a4323aece86fed99810f1f5106bead。

本次服务器操作全部只读；未部署、未调整线上配置、未调用收费模型测试、未重试生产任务、未 push。本地修改尚未提交。

## 1. 仅 @ 回复

已修复。真实载荷在 d.mentions 数组中包含 is_you:true，而非 d 顶层字段。

- 解析为 InboundMessage.MentionsBot，仅 GROUP_MESSAGE_CREATE 且标记为本机器人时新增回复资格。
- @ 其他人、其他机器人、仅文本包含 @，均不因此触发 mention_only。
- 原 GROUP_AT_MESSAGE_CREATE、私聊、always 行为保留。
- 合并消息时，带本机器人标记的普通事件也可更新先到的普通消息内容、附件、原始事件和回复期限；AT 事件保持最高优先级，后到普通事件不覆盖 AT 内容。
- 沿用平台消息唯一约束、inbox_tasks.message_id 唯一约束；只有首次创建 inbox 任务时创建处理确认。无需数据库迁移。
- 测试覆盖本机器人/其他机器人/混合标记/无标记；普通事件先到、mention 先到、AT 先到、只有 mention，以及任务已 completed 后收到另一个事件的去重。

## 2. 向量检索

根因是当前上游路由无可用渠道，并非项目漏发向量请求。线上 embedding 配置为 cooper，base_url=https://cooper-api.com/v1，模型 text-embedding-3-small，维度 1024。上游返回 503 model_not_found，明确指出 auto 分组无可用渠道。

数据库实测：101 个 index_entry 任务为 failed，全部 attempts=3；303 次失败正好对应 101×3。不存在单个任务无限自动重试。失败任务不会被后台自动恢复；编辑条目或新增同步内容可产生新任务，符合现有机制。当前 ready 知识条目数为 0。未为了减少日志而改动重试机制。

建议优先保留现有 embedding 模型，修复渠道：

1. 用户在 new-api 管理后台查找提供 text-embedding-3-small 的渠道，核对渠道启用状态、余额/配额、上游地址与凭据，以及模型名或模型映射。
2. 核对项目所用令牌的分组和 auto 分组路由规则，确保该令牌实际能路由到支持此模型的渠道；具体后台名称可能随 new-api 版本不同。
3. 在项目管理端「模型配置」对 cooper 执行 embedding 测试，确认实际返回 1024 维向量。测试会调用上游并可能产生费用，需用户认可。
4. 若上游不再提供该模型，由用户决定更换供应商/模型与费用；项目模型配置及所有关联知识库应使用一致的新向量模型与维度，已有索引应重建，不能混用向量空间。
5. 渠道测试成功后，再受控恢复失败 index_entry 任务并重建失败文档索引。此项会写生产库、调用模型，尚未执行，需授权。系统 retry-failed 接口当前不包含 agent_jobs，不能以它已恢复全部 FAQ 索引为前提；可由 Codex 定向重置最新失败索引任务，排除已有活动 dedupe_key，或逐条保存条目触发重建。

## 3. 构建、数据库连接与版本

- Dockerfile 原 syntax 声明需要从 docker.io 获取 Dockerfile frontend，构建环境网络不通即可在开始阶段失败。已移除该声明，并将 COPY --chmod 改为 COPY 后 RUN chmod，避免额外 frontend 拉取；基础镜像仍需镜像源可用，未承诺完全离线构建。
- entrypoint-smoke.log 确认失败进程连接 localhost:5432/aibot。这说明该次启动没有收到正确生产 DATABASE_URL；不是生产 ai_bot_cs 库已经损坏的证据。现新增容器启动前必须显式提供 DATABASE_URL 的校验，缺失时直接给出明确错误。
- 升级脚本不再在目标库备份失败时静默退回固定库名 ai_bot，避免备错库仍继续升级。
- Dockerfile 新增 org.opencontainers.image.revision；CI 传 github.sha；本地升级脚本传 HEAD，存在未提交文件时追加 -dirty。新增 GIN_MODE=release。
- 生产当前镜像 ID：sha256:1f0911535ed38312721ae3a582a8acf098ae992e24c99a30bdd5c1f52aac837f，标签为仓库 v2；启动于北京时间 21:57:22。
- 运行中 /app/ai-bot SHA256：76903ae61f4fd927960790001d47a8beeb023c1c5e850681dfd91d872bf0dd68。
- v2 没有 revision 标签；提取二进制的 go version -m 显示 go1.26.11，但也没有 vcs.revision。其二进制与服务器标记 92595c5 镜像内最终二进制不同，不能把那个镜像的 revision 继承给 v2。不同哈希也可能源于重新构建，不能据此断定源码不同。
- 可确认线上已有 agent/live-data/settings 等功能与相关接口，但无法可靠证明 v2 精确对应 640e604 或 92595c5 的完整源码。结论保留为“提交来源不可验证”。下一次经授权从明确源码构建并记录 revision、源码补丁及镜像 digest，才能恢复可追溯性。
- 仓库通用 compose 使用服务 app、端口 8080；生产使用 ai-bot-cs、端口 8124、硬编码镜像。不要直接把通用 server-upgrade.sh 对生产目录执行。正式部署将按生产 compose 做版本固定与健康检查。

## 4. 安全结论与待批准方案

实测 PostgreSQL 同时监听 0.0.0.0:5432 和 [::]:5432；8124 同时公开 IPv4/IPv6。UFW 未启用。现有 pg_hba 只有 loopback 和 ai_bot_cs/postgres 的 Docker 网段规则；TCP 公网可达不等于已经验证可从公网认证进入数据库。

推荐批准后一并执行：

1. 生产 compose 将端口改成 127.0.0.1:8124:8080，通过 Nginx 继续提供公开服务。重建 app 容器时会有短暂中断，直接访问公网 8124 的客户端将停止工作。
2. PostgreSQL 监听收紧为 127.0.0.1、::1、172.17.0.1；当前容器 /etc/hosts 的 host.docker.internal 实际为 172.17.0.1，不能仅保留 localhost。核对 Docker 启动顺序，确保重启后该网关存在。
3. pg_hba 的 ai_bot_cs/postgres 来源由 172.16.0.0/12 缩小为当前项目 172.22.0.0/16。当前活动 TCP 数据库连接均来自 172.22.0.2；执行前还需确认没有停机中的外部定时任务依赖原网段。
4. 新镜像使用 GIN_MODE=release。用户同时可在阿里云安全组取消公网 5432 入站，作为额外防线；无需为此全局启用 UFW，避免影响 SSH 与同机其他项目。

风险：PostgreSQL 修改监听地址需要重启，会断开数据库连接；app 重建会短暂影响机器人和管理端。先备份目标 ai_bot_cs 与生产 compose、env、PG 配置，保留当前镜像 digest；验证数据库恢复、Nginx/healthz、签名回调与单条新 @ 消息只回复一次。异常时恢复原配置与原镜像。不重放今晚过期的 QQ 测试事件，其被动回复期限已经过去。

这是交接任务明确要求先由用户确认的生产变更，当前均未执行。

## 5. 22:06 刷新 400

22:06:27 POST refresh 返回 400；22:06:34 保存 agent/settings 返回 200；22:06:40 再次 refresh 返回 200。当前处理函数只有未启用 LiveData 或未填写 BaseURL 会返回 400，上游抓取失败返回 502。因此与“先刷新、后保存启用/网站地址设置”一致。没有留存响应体，无法进一步区分当时是哪一个设置为空。无需修改后端逻辑；若要引导保存，可由 Claude 判断前端提示。

## 本地验证

- Go 1.26.0：go test -count=1 ./...，已连接本机隔离 PostgreSQL 16.6 + pgvector 0.8.0 + pgcrypto，所有包通过，包括数据库集成测试。
- go test -race ./internal/qq，通过。
- go vet ./...，通过。
- sh -n deploy/server-upgrade.sh deploy/docker-entrypoint.sh，通过。
- 缺 DATABASE_URL 的 entrypoint 本地检查，按预期启动前失败并给出明确原因。
- git diff --check，通过。
- 首轮并行集成测试遇到两个测试包同时 CREATE EXTENSION 的数据库初始化竞争；在隔离测试库预先创建扩展后重新完整运行并通过。未因此修改生产逻辑。
- 本机没有 Docker，未完成整镜像构建或容器启动验证；正式上线前仍需用生产等价 env/network 验证候选镜像。

## 改动文件（绝对路径）

- /Users/luji/Documents/coding/ai-bot-cs/.github/workflows/build-docker.yml
- /Users/luji/Documents/coding/ai-bot-cs/Dockerfile
- /Users/luji/Documents/coding/ai-bot-cs/backend/internal/app/app.go
- /Users/luji/Documents/coding/ai-bot-cs/backend/internal/app/app_integration_test.go
- /Users/luji/Documents/coding/ai-bot-cs/backend/internal/domain/domain.go
- /Users/luji/Documents/coding/ai-bot-cs/backend/internal/qq/webhook.go
- /Users/luji/Documents/coding/ai-bot-cs/backend/internal/qq/webhook_test.go
- /Users/luji/Documents/coding/ai-bot-cs/deploy/docker-entrypoint.sh
- /Users/luji/Documents/coding/ai-bot-cs/deploy/server-upgrade.sh
- /Users/luji/Documents/coding/ai-bot-cs/docs/production-investigation-2026-11-23.md

需要用户确认：批准生产镜像部署与 8124 收口；批准 PostgreSQL 监听/HBA 收紧及数据库重启窗口；决定修复现有 embedding 渠道或换模型及费用；渠道可用后批准索引恢复。
