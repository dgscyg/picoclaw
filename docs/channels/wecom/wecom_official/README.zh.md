# 企业微信官方 Smart Bot（WebSocket）

企业微信官方 Smart Bot 通道使用企业微信官方长连接 WebSocket 协议接入 PicoClaw。它适合两类场景：

1. 需要企业微信官方机器人私聊/群聊入口。
2. 需要在已有会话中主动发送消息通知，而不想暴露公网 webhook 回调地址。

与 `wecom_aibot` 的区别是：`wecom_official` 不走企业微信 AI Bot 的 webhook + stream polling 协议；它走官方 WebSocket 长连接，既支持基于原始回调 `req_id` 的 `aibot_respond_msg`/`replyStream` 回复，也支持通过 `aibot_send_msg` 做主动 markdown 通知。

## 与其他 WeCom 通道的对比

| 通道 | 入站方式 | 主动通知 | 群聊 | 流式输出 | 是否需要公网回调 |
|------|----------|----------|------|----------|------------------|
| `wecom` | Webhook | 受限 | ✅ | ❌ | 通常需要 |
| `wecom_app` | Webhook + 企业微信应用 API | ✅ | ❌ | ❌ | 需要 |
| `wecom_aibot` | Webhook + stream polling | ✅ | ✅ | ✅ | 需要 |
| `wecom_official` | WebSocket 长连接 | ✅ | ✅ | ✅（官方 replyStream） | ❌ |

## 配置

```json
{
  "channels": {
    "wecom_official": {
      "enabled": true,
      "bot_id": "YOUR_BOT_ID",
      "secret": "YOUR_BOT_SECRET",
      "websocket_url": "wss://openws.work.weixin.qq.com",
      "allow_from": [],
      "welcome_message": "",
      "reasoning_channel_id": ""
    }
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `enabled` | bool | 否 | 是否启用该通道 |
| `bot_id` | string | 是 | 企业微信官方 Smart Bot 的机器人 ID |
| `secret` | string | 是 | 企业微信官方 Smart Bot 的密钥 |
| `websocket_url` | string | 否 | 官方 WebSocket 地址，默认 `wss://openws.work.weixin.qq.com` |
| `allow_from` | array | 否 | 允许访问的用户 ID 白名单；空数组表示允许所有用户 |
| `welcome_message` | string | 否 | 用户触发 `enter_chat` 事件时发送的欢迎语；留空则不发送 |
| `reasoning_channel_id` | string | 否 | 将模型思维链路由到指定 channel 的目标 ID |

## 使用说明

### 1. 在企业微信后台创建官方 Smart Bot

1. 登录企业微信后台。
2. 进入机器人或 Smart Bot 管理页面。
3. 创建机器人并记录 `bot_id` 与 `secret`。
4. 确认机器人具备你需要的会话权限。

`wecom_official` 不依赖 PicoClaw 暴露回调 URL；PicoClaw 会主动向企业微信官方 WebSocket 服务建立长连接。

### 2. 填写 PicoClaw 配置并启动 Gateway

将 `bot_id`、`secret` 写入 `channels.wecom_official`，然后启动：

```bash
picoclaw gateway
```

启动成功后，PicoClaw 会：

1. 建立到 `websocket_url` 的长连接。
2. 发送 `aibot_subscribe` 做鉴权。
3. 定时发送 `ping` 心跳。
4. 在收到文本、图片、文件、混合消息后转入 PicoClaw 的标准 bus 流程。
5. 对普通消息回复优先走官方 `aibot_respond_msg` 流式回复链路；没有活跃回调上下文时才回退到 `aibot_send_msg` 主动通知。

### 3. 入站消息处理

当前实现支持：

- 文本消息
- 语音转写文本
- 图片消息下载与落盘
- 文件消息下载与落盘
- 图文混排消息
- `enter_chat` 事件欢迎语

群聊消息会继续走 PicoClaw 现有的 `group_trigger` 判断逻辑；如果你希望群里只在特定前缀下触发，可以额外配置 `group_trigger`。

### 4. 回复与主动通知

`wecom_official` 现在有两条出站路径：

- 对“刚收到的官方回调消息”，PicoClaw 会优先复用该消息的 `req_id`，通过 `aibot_respond_msg` 发送 `stream` 回复。
- 对“没有活跃回调上下文的独立通知”，PicoClaw 继续通过官方 `aibot_send_msg` 发送 markdown 消息。

这意味着它既能做官方会话内回复，也能做独立主动通知。

适合：

- 设备事件通知
- heartbeat/cron 结果通知
- 子任务完成后的补发消息

## 适用边界

- 这是“官方 Smart Bot WebSocket 通道”，支持官方 `replyStream` 回复，但它和 `wecom_aibot` 的 webhook + stream polling 仍然是两套不同协议。
- 如果你需要企业微信 AI Bot 那种“客户端轮询 `finish=false/true`”模型，请继续使用 `wecom_aibot`。
- PicoClaw 当前在 `wecom_official` 上实现的是“官方 stream reply 协议 + 消息级分块/收尾”，还不是 provider token 级逐字增量转发。
- 如果你需要企业微信应用级能力、媒体上传接口或更传统的企业应用回调模型，请使用 `wecom_app`。

## 常见问题

### 连接建立后立即断开

- 检查 `bot_id` 和 `secret` 是否正确。
- 检查出站网络是否允许访问 `wss://openws.work.weixin.qq.com`。
- 查看 PicoClaw 日志里是否出现认证失败或心跳连续失败。

### 能收到消息，但无法主动通知

- 确认 Gateway 进程仍在运行，并且 WebSocket 长连接未断开。
- 检查日志中是否出现 `aibot_send_msg` 回执错误。
- 确认发送目标使用的是企业微信实际会话 ID（单聊通常是 `userid`，群聊是 `chatid`）。

### 能收到消息，但回复没有走官方 stream

- 检查日志里是否出现 `aibot_respond_msg` 回执错误。
- 确认这条消息来自当前 WebSocket 回调会话，而不是后台任务补发或独立通知。
- 如果上游 Agent 只产出一条最终消息，你看到的会是“单条 stream 回复 + 自动 finish 收尾”，而不是 token 级逐字输出。

### 群里消息太容易触发

- 给 `wecom_official` 配置 `group_trigger.prefixes` 或 `group_trigger.mention_only`。
- 如无额外配置，PicoClaw 默认对群消息采取较宽松策略。
