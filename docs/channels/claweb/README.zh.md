# CLAWeb WebSocket 通道

`claweb` 是 PicoClaw 内置的 WebSocket 上游通道，目的是让现有 `claweb` browser/frontdoor 继续复用原协议，把浏览器消息送进 PicoClaw 的 channel -> bus -> agent 链路。

它不是浏览器直连用的完整 Web 应用，也不负责登录、历史、上传这些浏览器侧能力；这些能力仍应由 `claweb` frontdoor 保留。

## 文档导航

- 第一次接入或排查启动问题：看 `USAGE.zh.md`
- 已经知道基本启动方式，只想看配置和职责边界：看本文
- 需要把 `D:\tmp\claw\claweb` 的 frontdoor/browser 接到 PicoClaw：看 `INTEGRATION.zh.md`

## 适用场景

1. 你已经在用 `D:\tmp\claw\claweb` 的 browser client + frontdoor。
2. 你希望上游改接 PicoClaw，而不是 OpenClaw。
3. 你需要保留现有 `hello / ready / message / error` WebSocket 协议和前端行为。

## 与 `pico` 通道的区别

| 通道 | 面向对象 | 是否自带浏览器协议 | 是否负责浏览器登录/历史/上传 |
|------|----------|--------------------|-------------------------------|
| `pico` | PicoClaw 自有客户端 | 是，但协议不同 | 否 |
| `claweb` | 现有 CLAWeb frontdoor/browser | 是，兼容现有 CLAWeb 上游协议 | 否 |

## 配置示例

```json
{
  "channels": {
    "claweb": {
      "enabled": true,
      "listen_host": "127.0.0.1",
      "listen_port": 18999,
      "auth_token_file": "C:/path/to/claweb.token",
      "allow_from": [],
      "reasoning_channel_id": ""
    }
  }
}
```

## 配置字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `enabled` | bool | 否 | 是否启用通道 |
| `listen_host` | string | 否 | `claweb` 上游 WebSocket 监听地址，默认 `127.0.0.1` |
| `listen_port` | int | 否 | `claweb` 上游 WebSocket 监听端口，默认 `18999` |
| `auth_token` | string | 二选一 | 直接写入上游握手 token |
| `auth_token_file` | string | 二选一 | 从文件读取上游握手 token，推荐 |
| `allow_from` | array | 否 | 允许访问的发送者白名单；空数组表示全部放行 |
| `group_trigger` | object | 否 | 当浏览器侧传入 `roomId` 时，按 PicoClaw 群聊触发规则处理 |
| `reasoning_channel_id` | string | 否 | 将 reasoning 输出路由到指定 channel 目标 ID |

## 启动行为

启动 `picoclaw gateway` 后，`claweb` 通道会：

1. 在 `listen_host:listen_port` 启动独立 WebSocket server。
2. 校验首帧 `hello.token` 是否与 `auth_token` / `auth_token_file` 一致。
3. 接收浏览器上游的 `message` 帧。
4. 把文本和附件整理为 PicoClaw `InboundMessage`。
5. 将 agent 的出站结果回写为 `message` 帧。

注意：

- 这个监听端口是 `claweb` 通道自己的，不复用 `gateway.host/gateway.port`。
- `claweb` frontdoor 的 `CLAWEB_UPSTREAM_WS` 应该指向这里，而不是指向 PicoClaw 健康检查端口。

## 协议边界

当前实现兼容的上游协议重点是：

- `hello`：带 token 鉴权
- `ready`：服务端确认
- `message`：传文本、`mediaUrl`、`mediaDataUrl`
- `error`：返回协议或派发错误

当前实现已经支持：

- 入站文本
- 入站 `mediaUrl` / `mediaDataUrl`
- 出站文本
- 出站附件 frame
- 用 PicoClaw 的 `ReplyTo` 复用原用户 turn id，方便 frontdoor 继续做 assistant turn 聚合

当前实现不负责：

- `/login`
- `/history`
- `/upload`
- `/upload-file`
- 浏览器静态资源托管

这些仍属于 `claweb` frontdoor 的职责。

## 运行建议

1. 优先使用 `auth_token_file`，避免把 token 直接写进配置文件。
2. 如果只允许本机 frontdoor 访问，保持 `listen_host=127.0.0.1`。
3. 如果需要按浏览器房间隔离群聊行为，再配置 `group_trigger`。

## 最小验证

```bash
picoclaw gateway
```

成功时应在日志中看到：

1. `claweb` channel 被初始化并启动。
2. frontdoor 连上后出现 WebSocket 鉴权成功日志。
3. 浏览器发送消息后，agent 正常产出回复。
