# 有序演示素材接口

## 上传与编辑

每个回答可保存 0～9 个素材，混排 PNG、JPEG 和 MP4。图片上限为 5 MiB 和 1600 万像素。每个 MP4 上限为 25 MiB，整组上限为 64 MiB。MP4 需包含视频轨与媒体数据，后台保存原片。视频的浏览器播放能力取决于客户端支持的编码。

带素材使用 multipart/form-data，文件重复使用字段 `files`。字符串字段 `materials` 是最终顺序的 JSON 数组。无文件的问答编辑和审核也可发 JSON，此时直接传字符串数组。浏览器使用 FormData 时由浏览器设置 Content-Type 和 boundary。

| 操作 | materials |
|---|---|
| 编辑或审核时保留全部 | 省略字段且不传文件 |
| 清空 | `[]` |
| 保留第二个旧素材，在它之后添加新文件 | `["keep:<旧素材ID>","new:0"]` |
| 上传两个文件，将第二个排在前面 | `["new:1","new:0"]` |
| 全部替换为新文件，按选择顺序排列 | 只传 files |

`new:N` 使用 files 的零基下标。每个新文件必须出现一次，每个旧素材最多保留一次。keep 仅允许引用当前正在编辑的问答或正在审核的候选。新建、纠错、人工回复和缺口补答均没有可保留的旧素材。后端拒绝缺失、重复和越界的引用，编辑失败时保留原记录。新前端统一使用 materials 和 files。

## 请求与响应

以下路径以 `/api` 为前缀，沿用后台登录会话和 `{"data": ...}` 包装。纯文字请求继续支持 JSON。在 multipart 中布尔值用 `true` 或 `false` 字符串，别名逐个追加到 `aliases` 字段。

| 方法与路径 | 业务字段 | 成功响应中的 data |
|---|---|---|
| POST /messages/:id/feedback | rating 必填。可传 answerId、comment、correctedAnswer、question、knowledgeBaseId、publish。附素材时填写 correctedAnswer，直接发布时填写 knowledgeBaseId。 | 200，saved；生成候选时有 candidateId，直接发布时另有 entryId |
| POST /messages/:id/reply | text 和素材至少一项，可传 learn。沿用现有工单权限。 | 200，id 为第一条出站消息 ID，messageIds 为整组入队消息 ID，按发送顺序排列 |
| POST /knowledge/entries | knowledgeBaseId、question、answer 必填，可传 aliases | 201，id |
| PUT /knowledge/entries/:id | question、answer 必填，可传 aliases、status | 200，updated |
| POST /knowledge/candidates/:id/approve | knowledgeBaseId、question、answer 必填，可传 aliases、note | 200，entryId |
| POST /knowledge/gaps/:id/answer | knowledgeBaseId、answer 必填；question 默认沿用缺口内容，可传 aliases | 200，entryId |
| GET /knowledge/entries、GET /knowledge/candidates | 沿用现有筛选 | 200，data 为记录列表，每条含有序 materials，空组返回空数组 |

素材示例

```json
{
  "id": "3fb708b5-b9cb-45da-8826-e795c09e17d6",
  "kind": "video",
  "mime": "video/mp4",
  "filename": "第二步.mp4",
  "sizeBytes": 2048000,
  "url": "/api/knowledge/entries/<问答ID>/materials/3fb708b5-b9cb-45da-8826-e795c09e17d6"
}
```

预览使用同源认证 GET。候选路径为 `/api/knowledge/candidates/:id/materials/:materialId`，问答路径为 `/api/knowledge/entries/:id/materials/:materialId`。视频 url 可直接用作 video 元素的 src，支持 HTTP Range、206 和拖动播放。

## QQ 投递与学习

首条富媒体消息携带完整回答和第一个素材，其后每条发送一个素材。按实际已用 seq 分配余量，群聊和单聊被动窗口均为 60 分钟。单聊最多 4 次，普通群 5 次，满足开发者为群主或管理员条件且已启用扩展额度的群按 50 次处理。频道的 5 分钟规则与这里的群聊不同。超额度或出队时已过期的素材消息改走主动发送。主动发送仍取决于 QQ 对当前机器人的权限、配额和用户接收状态。接口 200 表示入队。投递连续失败后，该步及后续待发步骤标记失败，管理员可以用现有重试操作重新按序发送。

学习按同一群中连续的入站发言合并。同一客服在前一条后 30 秒内追加的图片或视频进入同一组，其他成员插话或客服开始新的纯文字发言会分组。模型读取客服文字、可用上下文和图片；视频保留原片，并将文件名提供给学习模型。候选保存所有来源消息 ID。勾选 learn 的人工回复按提交批次学习。

QQ 富媒体接口列出的图片格式为 PNG、JPG，视频为 MP4。此次支持 file_type 1 和 2。官方协议出处见[消息概述](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html)和[群富媒体上传](https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_files.post.html)。

## 迁移、备份和本地验证

迁移 048 将已有问答和候选的单图回填成一个有 ID 的素材。含有序素材的 ZIP 使用版本 2，新后端仍可导入版本 1 的单图包；导入会为素材创建新 ID。整库删除会删除所属记录及其素材。示例 Nginx 配置的请求体上限已改为 65 MiB，以容纳 64 MiB 素材和 multipart 表单。既有站点需在发布时核对实际反向代理限制。

在 backend 目录下设置 TEST_DATABASE_URL 指向带 pgvector、pgcrypto 的本地测试 PostgreSQL，然后执行：

```sh
go test -count=1 ./...
```

## 本次文件

项目根目录为 `/Users/luji/Documents/coding/ai-bot-cs`。

- 数据迁移在 `backend/internal/db/migrations/048_ordered_materials.sql`。
- 素材接口和入站路由在 `backend/internal/app/` 下的 `materials.go`、`reply_image.go`、`learning.go`、`app.go`、`ops.go`、`images.go`。
- 客服学习、知识检索、归档和投递在同目录下的 `staff_learning.go`、`group_guard.go`、`jobs.go`、`agent_pipeline.go`、`knowledge_archive.go`、`workers.go`。
- QQ 协议和文件存储在 `backend/internal/qq/client.go`、`backend/internal/qq/webhook.go`、`backend/internal/storage/local.go`。反向代理示例在 `deploy/nginx/ai-bot.conf.example`。
- 测试在 `backend/internal/app/` 下的 `materials_test.go`、`reply_image_test.go`、`agent_integration_test.go`、`google_knowledge_integration_test.go`、`human_reply_integration_test.go`、`knowledge_archive_test.go`。测试用 H.264 视频为 `backend/internal/app/testdata/knowledge/demo.mp4`。
