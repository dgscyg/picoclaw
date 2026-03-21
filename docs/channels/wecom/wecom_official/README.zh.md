# 企业微信官方 Smart Bot WebSocket 通道

`wecom_official` 使用企业微信官方 Smart Bot 的 WebSocket 长连接协议接入 PicoClaw，适合两类场景：

1. 作为企业微信官方机器人单聊或群聊入口。
2. 在已有会话中主动推送消息，而不暴露公网 webhook 回调地址。

它与 `wecom_aibot` 的区别是：`wecom_official` 不走 webhook + polling，而是直接走企业微信官方长连接协议，支持基于原始 `req_id` 的 `replyStream`、显式 `template_card` 回复、`aibot_respond_update_msg` 卡片更新、欢迎语回复、reply-scoped 媒体回复，以及独立的主动消息推送。

## 与其他 WeCom 通道的区别

| 通道 | 入站方式 | 主动通知 | 群聊 | 流式输出 | 是否需要公网回调 |
|------|----------|----------|------|----------|------------------|
| `wecom` | Webhook | 受限 | 是 | 否 | 通常需要 |
| `wecom_app` | Webhook + 企业微信应用 API | 是 | 否 | 否 | 需要 |
| `wecom_aibot` | Webhook + stream polling | 是 | 是 | 是 | 需要 |
| `wecom_official` | WebSocket 长连接 | 是 | 是 | 是，官方 `replyStream` / `template_card` / `aibot_respond_update_msg` | 否 |

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
      "placeholder": {
        "enabled": true,
        "text": "Thinking... 💭"
      },
      "card": {
        "enabled": false,
        "title": "PicoClaw"
      },
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
| `group_trigger` | object | 否 | 群聊触发条件，沿用 PicoClaw 现有群触发配置 |
| `placeholder.enabled` | bool | 否 | 是否先发送官方 `replyStream` 思考中占位流，默认开启 |
| `placeholder.text` | string | 否 | 占位文案；为空时默认 `Thinking... 💭` |
| `sendThinkingMessage` | bool | 否 | 兼容旧插件配置名，等价于 `placeholder.enabled` |
| `card.enabled` | bool | 否 | 是否启用通道内文本转官方模板卡片渲染 |
| `card.title` | string | 否 | 默认卡片品牌标题；为空时默认 `PicoClaw`。该值会用于通道内自动卡片，也会作为 `wecom_card` 的默认品牌标题 |
| `welcome_message` | string | 否 | 用户触发 `enter_chat` 时发送的欢迎语；留空则不发送 |
| `reasoning_channel_id` | string | 否 | 将 reasoning 输出路由到指定 channel 目标 ID |

## 接入步骤

### 1. 在企业微信后台创建官方 Smart Bot

1. 登录企业微信后台。
2. 创建 Smart Bot。
3. 记录 `bot_id` 和 `secret`。
4. 确认机器人具备目标会话权限。

### 2. 配置 PicoClaw 并启动

把 `bot_id`、`secret` 写入 `channels.wecom_official`，然后启动：

```bash
picoclaw gateway
```

启动后 PicoClaw 会：

1. 建立到 `websocket_url` 的长连接。
2. 发送 `aibot_subscribe` 完成鉴权。
3. 周期性发送 `ping` 保活。
4. 接收文本、图片、文件、混排和事件回调并接入 PicoClaw bus。

## 会话隔离

`wecom_official` 不再只用 `chatid` 作为会话历史 key，而是入站时显式写入 `InboundMessage.SessionKey`：

- 普通文本/媒体消息优先使用 `wecom_official:<chatid>:msg:<msgid>`。
- 模板卡片事件优先使用 `wecom_official:<chatid>:task:<task_id>`。
- 如果上游没有更稳定的锚点，则退回 `wecom_official:<chatid>:req:<req_id>`。

这样同一个群里多人同时和机器人互动、或同一个用户在同一会话里并发触发多轮回调时，不会再共享一条历史。

## 出站行为

### 普通会话回复

- 有活跃官方回调上下文时，PicoClaw 会复用原始 `req_id`。
- 思考占位启用时，先发送一条 `finish=false` 的 `replyStream` 占位消息。
- 普通文本回复继续复用同一个 `stream_id`，最终由通道内部直接发送 `finish=true` 的收尾帧结束这次 reply stream；这一步不再依赖共享消息结构上的专用字段。
- 如果 agent 或 tool 显式发送 `template_card` payload，例如通过 `wecom_card` tool，通道会改走 `aibot_respond_msg + template_card` 回复当前会话。
- 显式卡片回复不会替代后续普通文本流；若同一轮后续仍有最终文本回复，它仍会继续复用原 `stream_id` 去覆盖占位流。

### 主动通知

- 没有活跃回调上下文时，PicoClaw 会走 `aibot_send_msg`。
- 普通主动消息统一发送 `markdown`。
- 只有显式结构化卡片 payload，例如 `wecom_card` 或原始 `template_card` JSON，才会主动发送 `template_card`。

### 媒体发送

- `wecom_official` 现在支持通过 `send_file` / bus 媒体管线发送图片、语音、视频和文件。
- 实际上传流程走企业微信官方 websocket 分片接口：`aibot_upload_media_init` -> `aibot_upload_media_chunk` -> `aibot_upload_media_finish`。
- 若当前会话仍有可复用的回调 reply token，并且附件是本轮第一个媒体，通道会优先走 `aibot_respond_msg` 做 reply-scoped 媒体回复；这样媒体可以先发出，而本轮最终文本仍可继续复用原 `replyStream` 去替换思考占位符。后续附件改走主动发送。
- 没有活跃 reply token，或当前批次不是第一个附件时，通道统一走 `aibot_send_msg` 主动发送媒体。
- 限制与降级规则：
  - 图片、视频最大 10MB。
  - 语音只接受 `AMR`，且最大 2MB。
  - 任意文件硬上限 20MB。
  - 图片/视频/语音若类型不被官方接受或超过该类型限制，但整体仍在 20MB 以内，会自动降级为 `file` 发送。

### 欢迎语

- 收到 `enter_chat` 事件且 `welcome_message` 非空时发送欢迎语。
- 卡片关闭时使用 `aibot_respond_welcome_msg + text`。
- 卡片开启时使用 `aibot_respond_welcome_msg + template_card`。

## 卡片消息说明

企业微信官方 SDK `@wecom/aibot-node-sdk` 已提供：

- `replyTemplateCard`
- `replyStreamWithCard`
- `sendMessage(template_card)`
- `updateTemplateCard`

PicoClaw 当前在 `wecom_official` 中已经接通结构化卡片能力：

- 通道内文本转模板卡片渲染。
- `wecom_card` tool 直接生成并发送企业微信官方 `template_card`。
- 普通主动消息默认走 `markdown`，显式卡片 payload 才走 `template_card`。
- `template_card_event` 到达后，会优先在 5 秒窗口内自动执行一次 `aibot_respond_update_msg`，把卡片更新为“处理中”状态，避免长耗时 LLM 推理错过窗口。
- 超过 5 秒卡片更新窗口后，后续用户可见输出会改走当前 callback 的 `response_url` markdown follow-up，或退回普通主动消息，而不是继续伪装成新的卡片更新。
- 欢迎语可按卡片模式回复。

如果 agent 需要发送企业微信卡片消息，应优先使用 `wecom_card` tool，而不是通过 `message` tool 手写原始 JSON。

## 适用边界

- 这是官方 Smart Bot 长连接通道，不需要公网 webhook。
- 当前 provider 到 channel 仍不是 token 级逐字透传；现有实现是通道级 `replyStream`，并可在首个正式帧附带 `stream_with_template_card`。
- 如果你需要 webhook + polling 语义，继续使用 `wecom_aibot`。
- 如果你需要企业微信自建应用能力，使用 `wecom_app`。

## 常见问题

### 能收到消息，但不回卡片

- 检查 `card.enabled` 是否为 `true`。
- 检查日志里是否有 `aibot_send_msg` 或 `aibot_respond_msg` 回执错误。

### 流式回复正常，但最终没有卡片

- 确认本次回复来自活跃官方回调，而不是后台独立通知。
- 检查是否存在中途断线导致最终收尾帧未成功发送。

### 卡片按钮点击后没有后续动作

- `template_card_event` 的更新窗口只有 5 秒；自动“处理中”更新只保证第一时间占位。超过窗口后，后续文本跟进会改走 `response_url` markdown follow-up 或普通主动消息，不会再继续做卡片更新。
- 按钮卡片的可用字段必须遵守企业微信模板卡片类型限制，例如 `button_interaction` 不支持 `vertical_content_list`。

### 媒体消息没有发送出来

- 检查文件是否超过官方限制：图片/视频 10MB、语音 2MB、总文件上限 20MB。
- 检查语音是否真的是 `AMR`；非 `AMR` 语音会自动按文件发送，而不是按 voice 发送。
- 检查当前 reply token 是否仍处于可复用窗口；同一轮只有第一个符合条件的媒体会走 reply-scoped 回复，其余媒体会转为主动发送，而最终文本仍会尝试复用原 `replyStream` 收尾。
