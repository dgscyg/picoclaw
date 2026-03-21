# PicoClaw `claweb` 与 CLAWeb Frontdoor 对接说明

本文说明如何把 `D:\tmp\claw\claweb` 的 frontdoor/browser 对接到 PicoClaw 新增的 `claweb` channel。

## 对接拓扑

```text
Browser Client
    |
    |  HTTP /ws /history /upload
    v
CLAWeb Frontdoor
    |
    |  WebSocket hello/ready/message
    v
PicoClaw channels.claweb
    |
    v
MessageBus -> AgentLoop -> Tools / Providers
```

关键边界只有一条：

- `frontdoor` 负责浏览器登录、历史、上传、assistant frame 合并。
- PicoClaw `claweb` channel 只负责上游 WebSocket 协议和 agent 消息收发。

## 第 1 步：准备共享 token

推荐把上游 token 放到同一个文件里，让 PicoClaw 与 frontdoor 共用。

示例：

```text
C:\path\to\claweb.token
```

文件内容就是一行随机 token。

## 第 2 步：配置 PicoClaw

在 `picoclaw` 的配置中启用 `channels.claweb`：

```json
{
  "channels": {
    "claweb": {
      "enabled": true,
      "listen_host": "127.0.0.1",
      "listen_port": 18999,
      "auth_token_file": "C:/path/to/claweb.token",
      "allow_from": []
    }
  }
}
```

说明：

- `listen_host/listen_port` 是 frontdoor 要连接的上游地址。
- 这里的端口不是 PicoClaw gateway 健康检查端口，而是 `claweb` 通道自己的监听端口。

## 第 3 步：配置 CLAWeb Frontdoor

在 `D:\tmp\claw\claweb\access\frontdoor` 启动前，至少配置这些环境变量：

```bash
CLAWEB_UPSTREAM_WS=ws://127.0.0.1:18999
CLAWEB_UPSTREAM_TOKEN_FILE=C:/path/to/claweb.token
CLAWEB_LOGIN_CONFIG=./config/claweb-login.json
CLAWEB_STATIC_ROOT=../../clients/browser
```

如果你还要支持文件下载，继续配置：

```bash
CLAWEB_MEDIA_DIR=./data/media
CLAWEB_MEDIA_BASE_URL=http://127.0.0.1:18081
```

## 第 4 步：启动顺序

推荐顺序：

1. 先启动 `picoclaw gateway`
2. 再启动 `claweb frontdoor`
3. 最后打开 browser client

这样 frontdoor 在首次 `ensureUpstream` 时能直接连上 PicoClaw。

## 对接后的职责分工

### PicoClaw `claweb` channel 负责

- `hello` 鉴权
- `ready` 返回
- 接收 `message` 帧
- 把入站文本/附件变成 PicoClaw `InboundMessage`
- 把 agent 结果回写成 `message` 帧
- 复用 `reply_to` 作为 assistant turn id

### CLAWeb Frontdoor 继续负责

- `POST /login`
- `GET /history`
- `POST /upload`
- `POST /upload-file`
- `/ws` 浏览器入口
- assistant frame 900ms 合并
- JSONL 历史与 recent snapshot

## 推荐配置组合

### 本机开发

```text
picoclaw channels.claweb.listen_host = 127.0.0.1
picoclaw channels.claweb.listen_port = 18999
frontdoor CLAWEB_UPSTREAM_WS         = ws://127.0.0.1:18999
frontdoor BIND/PORT                  = 127.0.0.1:18081
```

### 反向代理部署

```text
Browser -> Reverse Proxy -> Frontdoor
Frontdoor -> ws://127.0.0.1:18999 -> PicoClaw claweb channel
```

建议仍然让 `claweb` 上游监听在内网或 loopback，不直接暴露给公网。

## 常见问题

### 1. frontdoor 报 `auth failed`

检查两边是否使用同一份 token：

- PicoClaw `channels.claweb.auth_token(_file)`
- frontdoor `CLAWEB_UPSTREAM_TOKEN(_FILE)`

### 2. frontdoor 连接的是 gateway 健康端口

这是错误配置。
`CLAWEB_UPSTREAM_WS` 必须指向 `channels.claweb.listen_host:list_port`，不是 `gateway.host:gateway.port`。

### 3. 浏览器能登录，但消息没有回复

优先检查：

1. frontdoor 是否已经连上 PicoClaw 上游
2. PicoClaw 日志里是否有 `claweb` 客户端鉴权成功
3. `allow_from` 是否误拦截

### 4. 浏览器文件上传正常，但 agent 看不到附件

先区分两段：

1. browser -> frontdoor 上传是否成功
2. frontdoor -> PicoClaw 上游 `message` 是否带了 `mediaUrl` / `mediaDataUrl`

`claweb` channel 只处理第二段；如果第一段没成功，问题仍在 frontdoor。
