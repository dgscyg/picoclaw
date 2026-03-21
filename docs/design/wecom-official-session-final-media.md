# WeCom Official 会话隔离、最终收尾与媒体上传改造说明

## 背景

这次合并同时处理了三类问题：

1. `wecom_official` 在群聊和并发回调场景下，不能继续只靠 `chatid` 作为会话历史 key。
2. 企业微信官方 `replyStream` 需要明确的最终收尾语义，但这个语义不应该污染所有 channel 共享的 `bus.OutboundMessage`。
3. `wecom_official` 之前缺少完整的官方媒体上传能力，无法覆盖主动发送和 callback-scoped 回复两种路径。

同时，这次改动保持了既定 merge 边界：

- 保留 `wecom_official` 的 main 分支结构和官方 WebSocket 通道形态。
- 保留 MuninnDB 的 MCP-only / vault / workspace memory boundary 约束。
- 不把 WeCom 专用语义扩散到通用 bus DTO。

## 目标

- 让同一 `chatid` 下并发发生的多轮 WeCom 对话不再串历史。
- 让 `wecom_official` 能正确区分“普通发送”和“本轮最终收尾”。
- 让 `send_file` / 媒体总线在 `wecom_official` 上具备官方可用的上传与发送能力。

## 主要变更

### 1. 去掉全局 `OutboundMessage.Final`

不再在 `pkg/bus/types.go` 的通用 `OutboundMessage` 上追加 WeCom 专用 `Final` 字段。

改为：

- 在 `pkg/channels/interfaces.go` 新增 `FinalMessageCapable`
- 在 `pkg/agent/loop.go` 通过 `sendFinalMessage(...)` 优先调用 channel 的 `SendFinal(...)`
- `wecom_official` 在 `pkg/channels/wecom/official.go` 内实现 `SendFinal(...)`

这样只有真正需要“最终收尾”语义的 channel 才实现该能力，其余 channel 继续走普通 `Send(...)` / bus 出站流程。

### 2. `wecom_official` 显式生成 `SessionKey`

在 `pkg/channels/base.go` 新增 `HandleMessageWithSessionKey(...)`，允许 channel 在入站时直接写入会话 key。

`wecom_official` 当前规则：

- 普通消息优先：`wecom_official:<chatid>:msg:<msgid>`
- 模板卡片事件优先：`wecom_official:<chatid>:task:<task_id>`
- 回退：`wecom_official:<chatid>:req:<req_id>`

这样做的原因是：

- 企业微信机器人可能在同一个群中同时被多个人触发
- 同一个用户也可能在同一个 chat 中并发触发多次 callback
- 仅使用 `chatid` 会把并发上下文压到同一个 history bucket 中

### 3. `wecom_official` 增加官方媒体上传能力

`pkg/channels/wecom/official.go` 已新增完整媒体上传流程：

- `aibot_upload_media_init`
- `aibot_upload_media_chunk`
- `aibot_upload_media_finish`

随后按上下文选择发送路径：

- 有活跃 reply token 时，第一个附件优先走 callback-scoped `aibot_respond_msg`
- 该媒体回复不会提前销毁原 stream reply task，因此同一轮最终文本仍可继续覆盖 `Thinking...` 占位流
- 无活跃 reply token，或不是第一个附件时，走主动 `aibot_send_msg`

支持的官方约束：

- 图片最大 10MB
- 视频最大 10MB
- 语音必须是 `AMR` 且最大 2MB
- 文件硬上限 20MB

降级策略：

- 图片/视频/语音若超出各自类型限制，但整体仍在 20MB 以内，则自动降级为 `file`
- 超过 20MB 直接拒绝发送

## 兼容性与边界

### 对其他 channel 的影响

- Telegram、Discord、Feishu、CLI 等 channel 不需要理解 WeCom 的 final reply 语义
- 通用 bus 结构仍只承载通用路由与回复字段，例如 `Channel`、`ChatID`、`ReplyTo`
- WeCom 专用逻辑被限制在 capability 和 `wecom_official` 通道内部

### 对配置的影响

- 本次没有新增 `wecom_official` 配置字段
- `config/config.example.json` 无需为这次改动新增配置项

### 对现有 WeCom 行为的影响

- 普通文本 reply stream 的最终收尾现在更明确，由 channel 内部直接结束 reply task
- `template_card_event` 仍保留 5 秒内自动“处理中”更新
- 超过卡片更新窗口后，后续可见文本继续走 `response_url` markdown follow-up 或主动消息

## 验证

本次改动已按两层验证：

### 聚焦行为验证

- `pkg/agent`：最终收尾能力、会话 key 选择、Muninn 相关行为
- `pkg/channels/wecom`：最终 reply stream 收尾、主动媒体发送、reply-scoped 媒体回复、模板卡片事件会话 key

### 全仓编译验证

使用：

```bash
go test -run "^$" ./...
```

目标是做 compile-only 验证，确认当前工作树在本次合并后的代码至少可以完整通过编译。

## 参考范围

- 参考 PR：`#1787`、`#1789`
- 核心实现文件：
  - `pkg/channels/wecom/official.go`
  - `pkg/channels/base.go`
  - `pkg/channels/interfaces.go`
  - `pkg/agent/loop.go`
  - `pkg/bus/types.go`
