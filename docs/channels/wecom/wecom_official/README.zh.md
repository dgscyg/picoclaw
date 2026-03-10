# 企业微信官方 Smart Bot WebSocket 通道

`wecom_official` 使用企业微信官方 Smart Bot 的 WebSocket 长连接协议接入 PicoClaw，适合两类场景：

1. 作为企业微信官方机器人单聊或群聊入口。
2. 在已有会话中主动推送消息，而不暴露公网 webhook 回调地址。

它与 `wecom_aibot` 的区别是：`wecom_official` 不走 webhook + polling，而是直接走企业微信官方长连接协议，支持基于原始 `req_id` 的 `replyStream`、`stream_with_template_card`、欢迎语回复，以及独立的主动消息推送。

## 与其他 WeCom 通道的区别

| 通道 | 入站方式 | 主动通知 | 群聊 | 流式输出 | 是否需要公网回调 |
|------|----------|----------|------|----------|------------------|
| `wecom` | Webhook | 受限 | 是 | 否 | 通常需要 |
| `wecom_app` | Webhook + 企业微信应用 API | 是 | 否 | 否 | 需要 |
| `wecom_aibot` | Webhook + stream polling | 是 | 是 | 是 | 需要 |
| `wecom_official` | WebSocket 长连接 | 是 | 是 | 是，官方 `replyStream` / `stream_with_template_card` | 否 |

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
| `card.title` | string | 否 | 模板卡片主标题；为空时默认 `PicoClaw` |
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

## 出站行为

### 普通会话回复

- 有活跃官方回调上下文时，PicoClaw 会复用原始 `req_id`。
- 思考占位启用时，先发送一条 `finish=false` 的 `replyStream` 占位消息。
- 正式回复仍走同一个 `stream_id`。
- 卡片关闭时，整个过程都使用普通 `replyStream`。
- 卡片开启时，首个正式回复帧会使用 `stream_with_template_card` 挂载模板卡片，后续继续沿用同一个 `stream_id` 走普通 `replyStream` 收尾。

### 主动通知

- 没有活跃回调上下文时，PicoClaw 会走 `aibot_send_msg`。
- 卡片关闭时，主动消息发送 `markdown`。
- 卡片开启时，主动消息发送 `template_card`。

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

PicoClaw 当前在 `wecom_official` 中已经接通第一层卡片能力：

- 通道内文本转模板卡片渲染。
- 主动消息走 `template_card`。
- 回调内首个正式流式帧走 `stream_with_template_card` 挂卡片，最终仍由普通 `replyStream` 收尾。
- 欢迎语可按卡片模式回复。

当前仍未接通的是第二层结构化卡片能力：

- agent 或 tool 显式产出完整卡片 payload。
- `template_card_event` 回调后的 `aibot_respond_update_msg` 更新链路。
- 面向按钮、下拉、投票等交互控件的结构化出站模型。

也就是说，当前卡片更适合“展示型卡片”，而不是“强交互型卡片”。

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

- 当前 `wecom_official` 只支持展示型卡片。
- `template_card_event` 到 `aibot_respond_update_msg` 的交互更新链路仍未接入。
