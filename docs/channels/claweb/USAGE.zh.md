# PicoClaw `claweb` 通道使用说明

本文是面向“我要把现有 CLAWeb browser/frontdoor 跑起来并接到 PicoClaw”的操作说明。

如果你只关心字段定义和职责边界，看 `README.zh.md`；如果你要看 frontdoor/browser 的对接拓扑，看 `INTEGRATION.zh.md`。

## 先理解两个鉴权层

`claweb` 相关报错最容易混淆的是，这里其实有两层认证：

1. browser -> frontdoor
   - 走 `POST /login`
   - 用的是 `claweb-login.json` 里的 `passphrases`
2. frontdoor -> PicoClaw `channels.claweb`
   - 走 WebSocket `hello.token`
   - 用的是 `channels.claweb.auth_token(_file)` 和 `CLAWEB_UPSTREAM_TOKEN(_FILE)`

`ws_auth_failed` 通常是第 1 层失败，不是第 2 层失败。

## 启动前准备

### 1. PicoClaw 配置文件位置

默认配置文件路径是：

```text
C:\Users\<你的用户名>\.picoclaw\config.json
```

如果你要改用别的配置文件，先在启动 `picoclaw` 的终端设置：

```powershell
$env:PICOCLAW_CONFIG = "D:\path\to\config.json"
```

### 2. CLAWeb frontdoor 登录配置

首次使用可以直接从模板复制：

```powershell
cd D:\tmp\claw\claweb\access\frontdoor
Copy-Item .\config\claweb-login.example.json .\config\claweb-login.json
```

然后把 `passphrases` 改成你自己的登录口令，例如：

```json
{
  "guest-a": {
    "displayName": "Guest A",
    "passphrases": ["123456"],
    "userId": "user-guest-a",
    "roomId": "",
    "clientId": "guest-a"
  }
}
```

## 方式一：直接在两边写 `auth_token`

这是最直观的方式，也最适合本机联调。

### 第 1 步：配置 PicoClaw

在 `config.json` 里启用 `channels.claweb`：

```json
{
  "channels": {
    "claweb": {
      "enabled": true,
      "listen_host": "127.0.0.1",
      "listen_port": 18999,
      "auth_token": "claweb-local-20260317",
      "auth_token_file": "",
      "allow_from": [],
      "reasoning_channel_id": ""
    }
  }
}
```

注意：

- `listen_host/listen_port` 是 `claweb` 通道自己的上游监听地址
- 它不是 PicoClaw `gateway.host/gateway.port`
- `auth_token` 和 `auth_token_file` 二选一即可；如果两个都填，当前实现优先读 `auth_token`

### 第 2 步：启动 PicoClaw

```powershell
cd D:\tmp\claw\picoclaw
go run ./cmd/picoclaw gateway
```

成功时，日志里不应再出现：

```text
Failed to initialize channel {channel=Claweb, error=claweb auth_token or auth_token_file is required}
```

### 第 3 步：启动 frontdoor

必须在执行 `node server.js` 的同一个终端里先设置环境变量：

```powershell
cd D:\tmp\claw\claweb\access\frontdoor

$env:CLAWEB_UPSTREAM_WS = "ws://127.0.0.1:18999"
$env:CLAWEB_UPSTREAM_TOKEN = "claweb-local-20260317"
$env:CLAWEB_LOGIN_CONFIG = "D:\tmp\claw\claweb\access\frontdoor\config\claweb-login.json"

node server.js
```

如果你还需要本地上传和媒体回放，再补：

```powershell
$env:CLAWEB_MEDIA_DIR = "D:\tmp\claw\claweb\access\frontdoor\data\media"
$env:CLAWEB_MEDIA_BASE_URL = "http://127.0.0.1:18081"
```

### 第 4 步：打开浏览器并登录

浏览器打开：

```text
http://127.0.0.1:18081
```

先用 `claweb-login.json` 里的 `passphrase` 登录，再建立 `/ws` 会话并发消息。

## 方式二：用共享 token 文件

如果你不想把 token 明文写进配置或环境变量，可以让 PicoClaw 和 frontdoor 共用一个文件。

### 第 1 步：写入 token 文件

```powershell
Set-Content -Path D:\tmp\claw\tmp\claweb.token -NoNewline -Value "claweb-local-20260317"
```

注意：

- 文件内容必须非空
- PicoClaw 只在启动时读取这个文件；改完后要重启 `picoclaw gateway`

### 第 2 步：配置 PicoClaw

```json
{
  "channels": {
    "claweb": {
      "enabled": true,
      "listen_host": "127.0.0.1",
      "listen_port": 18999,
      "auth_token": "",
      "auth_token_file": "D:/tmp/claw/tmp/claweb.token",
      "allow_from": []
    }
  }
}
```

### 第 3 步：配置 frontdoor

```powershell
cd D:\tmp\claw\claweb\access\frontdoor

$env:CLAWEB_UPSTREAM_WS = "ws://127.0.0.1:18999"
$env:CLAWEB_UPSTREAM_TOKEN_FILE = "D:\tmp\claw\tmp\claweb.token"
$env:CLAWEB_LOGIN_CONFIG = "D:\tmp\claw\claweb\access\frontdoor\config\claweb-login.json"

node server.js
```

## 推荐启动顺序

1. 先启动 `picoclaw gateway`
2. 再启动 `claweb frontdoor`
3. 最后打开浏览器
4. 先登录，再发消息

## 最小自检

### 自检 1：确认 token 是否非空

```powershell
Get-Content D:\tmp\claw\tmp\claweb.token
```

### 自检 2：确认 PicoClaw 是否在监听 `claweb` 上游端口

```powershell
Test-NetConnection 127.0.0.1 -Port 18999
```

### 自检 3：确认 frontdoor 当前终端已经拿到上游 token

```powershell
$env:CLAWEB_UPSTREAM_TOKEN
$env:CLAWEB_UPSTREAM_TOKEN_FILE
```

## 常见报错与处理

### 1. frontdoor 提示 `missing CLAWEB_UPSTREAM_TOKEN`

说明 frontdoor 启动时没有读到：

- `CLAWEB_UPSTREAM_TOKEN`
- 或 `CLAWEB_UPSTREAM_TOKEN_FILE`

先在同一个 PowerShell 终端设置环境变量，再执行 `node server.js`。

### 2. PicoClaw 提示 `claweb auth_token or auth_token_file is required`

说明 `channels.claweb` 在初始化时没有拿到有效 token。

常见原因：

- `auth_token` 是空字符串
- `auth_token_file` 指向了空文件
- 改完配置或 token 文件后，没有重启 `picoclaw gateway`

### 3. frontdoor 日志出现 `ws_auth_failed`

这通常是 browser -> frontdoor 的登录态失效，不是上游 WebSocket token 配错。

先检查：

1. 你是否已经先登录
2. `claweb-login.json` 里的 `passphrases` 是否正确
3. 浏览器是否残留了旧 token

最省事的处理方式是开一个无痕窗口重新登录。

### 4. frontdoor 指到了 PicoClaw gateway 端口

错误示例：

```text
CLAWEB_UPSTREAM_WS=ws://127.0.0.1:18790
```

这里的 `18790` 如果是 PicoClaw `gateway` 端口，就是错的。

正确目标应该是：

```text
ws://127.0.0.1:18999
```

也就是 `channels.claweb.listen_host/listen_port`。

## 当前实现边界

当前 `claweb` channel 负责：

- WebSocket `hello -> ready -> message` 协议
- 入站文本和 `mediaUrl` / `mediaDataUrl`
- 出站文本和附件 frame
- 使用 PicoClaw `ReplyTo` 复用原 turn id

当前仍由 frontdoor 负责：

- `/login`
- `/history`
- `/upload`
- `/upload-file`
- 浏览器静态页面
- 历史与 recent snapshot
- assistant frame 合并
