# CLI Reference Guide

PicoClaw CLI 完整命令参考。

## 环境变量

| 变量 | 描述 |
|------|------|
| `PICOCLAW_HOME` | 覆盖默认主目录 (`~/.picoclaw`) |
| `PICOCLAW_CONFIG` | 覆盖默认配置文件路径 (`~/.picoclaw/config.json`) |

## 根命令

```bash
picoclaw [command]
```

## 命令列表

### `picoclaw onboard` (别名: `o`)

初始化 PicoClaw 配置和工作区模板。

```bash
picoclaw onboard
```

### `picoclaw agent`

直接与代理交互，支持交互式和非交互式模式。

```bash
picoclaw agent [flags]
```

| 标志 | 简写 | 默认值 | 描述 |
|------|------|--------|------|
| `--debug` | `-d` | `false` | 启用调试日志 |
| `--message` | `-m` | `""` | 发送单条消息（非交互模式） |
| `--session` | `-s` | `cli:default` | 会话密钥 |
| `--model` | | `""` | 指定使用的模型 |

**示例:**
```bash
# 交互模式
picoclaw agent

# 非交互模式
picoclaw agent -m "你好"
```

### `picoclaw gateway` (别名: `g`)

启动 PicoClaw 网关服务器。

```bash
picoclaw gateway [flags]
```

| 标志 | 简写 | 默认值 | 描述 |
|------|------|--------|------|
| `--debug` | `-d` | `false` | 启用调试日志 |

### `picoclaw auth`

管理认证。

```bash
picoclaw auth <subcommand>
```

#### 子命令

**`auth login`** - OAuth 登录或粘贴令牌

```bash
picoclaw auth login [flags]
```

| 标志 | 简写 | 默认值 | 描述 |
|------|------|--------|------|
| `--provider` | `-p` | (必填) | 提供商: `openai`, `anthropic` |
| `--device-code` | | `false` | 使用设备代码流（无头环境） |
| `--setup-token` | | `false` | 使用 setup-token 流（Anthropic） |

**`auth logout`** - 登出

**`auth status`** - 显示认证状态

**`auth models`** - 显示可用模型（Google Antigravity）

### `picoclaw cron` (别名: `c`)

管理定时任务。

```bash
picoclaw cron <subcommand>
```

#### 子命令

**`cron list`** - 列出所有任务

**`cron add`** - 添加新任务

```bash
picoclaw cron add [flags]
```

| 标志 | 简写 | 默认值 | 描述 |
|------|------|--------|------|
| `--name` | `-n` | (必填) | 任务名称 |
| `--message` | `-m` | (必填) | 发送给代理的消息 |
| `--every` | `-e` | `0` | 每隔 N 秒执行 |
| `--cron` | `-c` | `""` | Cron 表达式（如 `0 9 * * *`） |
| `--deliver` | `-d` | `false` | 将响应投递到通道 |
| `--to` | | `""` | 投递接收者 |
| `--channel` | | `""` | 投递通道 |

**注意:** `--every` 和 `--cron` 互斥，必须指定其一。

**`cron remove`** - 删除任务

**`cron enable`** - 启用任务

**`cron disable`** - 禁用任务

### `picoclaw skills`

管理技能。

```bash
picoclaw skills <subcommand>
```

#### 子命令

**`skills list`** - 列出已安装技能

**`skills install`** - 安装技能

```bash
picoclaw skills install <source> [--registry]
```

**`skills remove`** - 移除技能

**`skills search`** - 搜索技能

**`skills show`** - 显示技能详情

**`skills list-builtin`** - 列出内置技能

**`skills install-builtin`** - 安装内置技能

### `picoclaw migrate`

从其他 claw 项目迁移配置。

```bash
picoclaw migrate [flags]
```

| 标志 | 默认值 | 描述 |
|------|--------|------|
| `--dry-run` | `false` | 预览迁移，不实际执行 |
| `--from` | `openclaw` | 迁移源（如 `openclaw`） |
| `--refresh` | `false` | 重新同步工作区文件 |
| `--config-only` | `false` | 仅迁移配置 |
| `--workspace-only` | `false` | 仅迁移工作区文件 |
| `--force` | `false` | 跳过确认提示 |
| `--source-home` | `""` | 覆盖源主目录 |
| `--target-home` | `""` | 覆盖目标主目录 |

**示例:**
```bash
picoclaw migrate --dry-run
picoclaw migrate --from openclaw --force
```

### `picoclaw status` (别名: `s`)

显示 PicoClaw 状态信息（版本、配置路径、工作区路径、模型、API 提供商状态）。

```bash
picoclaw status
```

### `picoclaw version` (别名: `v`)

显示版本信息。

```bash
picoclaw version
```

## 配置文件

配置文件位于 `~/.picoclaw/config.json`（可通过 `PICOCLAW_CONFIG` 覆盖）。

配置结构见 `pkg/config/config.go:50-60`：
- `agents` - 代理配置
- `bindings` - 代理绑定
- `session` - 会话配置
- `channels` - 通道配置
- `providers` - 提供商配置（旧版）
- `model_list` - 模型列表配置（新版）
- `gateway` - 网关配置
- `tools` - 工具配置
- `heartbeat` - 心跳配置
- `devices` - 设备配置