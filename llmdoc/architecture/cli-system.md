# CLI System Architecture

## 1. Identity

- **What it is:** PicoClaw 命令行接口系统，基于 Cobra 框架构建的多命令 CLI 应用。
- **Purpose:** 提供统一的命令入口，支持代理交互、网关启动、认证管理、定时任务、技能管理等操作。

## 2. Core Components

- `cmd/picoclaw/main.go` (`NewPicoclawCommand`, `main`): CLI 应用主入口点，创建根命令并添加所有子命令。
- `cmd/picoclaw/internal/helpers.go` (`GetPicoclawHome`, `GetConfigPath`, `LoadConfig`, `FormatVersion`): 配置路径解析和版本格式化辅助函数。
- `pkg/config/config.go` (`Config`, `LoadConfig`, `LoadConfigWithEnv`, `SaveConfig`): 配置结构定义和加载/保存逻辑。
- `pkg/config/defaults.go` (`DefaultConfig`): 默认配置值定义。

### 子命令模块

- `cmd/picoclaw/internal/onboard/command.go` (`NewOnboardCommand`): 初始化配置和工作区模板。
- `cmd/picoclaw/internal/agent/command.go` (`NewAgentCommand`): 直接与代理交互（交互式/非交互式）。
- `cmd/picoclaw/internal/gateway/command.go` (`NewGatewayCommand`): 启动网关服务器。
- `cmd/picoclaw/internal/auth/command.go` (`NewAuthCommand`): 认证管理命令组。
- `cmd/picoclaw/internal/cron/command.go` (`NewCronCommand`): 定时任务管理命令组。
- `cmd/picoclaw/internal/skills/command.go` (`NewSkillsCommand`): 技能管理命令组。
- `cmd/picoclaw/internal/migrate/command.go` (`NewMigrateCommand`): 从其他 claw 项目迁移配置。
- `cmd/picoclaw/internal/status/command.go` (`NewStatusCommand`): 显示系统状态。
- `cmd/picoclaw/internal/version/command.go` (`NewVersionCommand`): 显示版本信息。

## 3. Execution Flow (LLM Retrieval Map)

### 启动流程

1. **入口点:** `main()` 在 `cmd/picoclaw/main.go:64-70` 打印 ASCII 横幅并执行根命令。
2. **命令注册:** `NewPicoclawCommand()` 在 `cmd/picoclaw/main.go:27-49` 创建根命令并添加 9 个子命令。

### 配置加载流程

1. **路径解析:** `GetConfigPath()` 在 `cmd/picoclaw/internal/helpers.go:31-36` 优先使用 `$PICOCLAW_CONFIG`，否则使用 `~/.picoclaw/config.json`。
2. **主目录解析:** `GetPicoclawHome()` 在 `cmd/picoclaw/internal/helpers.go:23-29` 优先使用 `$PICOCLAW_HOME`，否则使用 `~/.picoclaw`。
3. **配置加载:** `LoadConfig()` 在 `cmd/picoclaw/internal/helpers.go:38-40` 调用 `config.LoadConfig()` 加载 JSON 配置。
4. **环境变量覆盖:** `LoadConfigWithEnv()` 在 `pkg/config/config.go` 使用 `env` 标签支持环境变量覆盖配置值。

### 子命令初始化模式

1. **延迟加载模式:** `cron` 和 `skills` 命令在 `cmd/picoclaw/internal/cron/command.go:25-32` 和 `cmd/picoclaw/internal/skills/command.go:25-41` 使用 `PersistentPreRunE` 钩子延迟加载配置。
2. **即时执行模式:** `agent`、`gateway` 等命令在 `RunE` 中直接加载配置并执行。

### 命令层级结构

```
picoclaw (root)
├── onboard (o)        # 初始化
├── agent              # 代理交互
├── gateway (g)        # 启动网关
├── auth               # 认证管理
│   ├── login          # OAuth 登录
│   ├── logout         # 登出
│   ├── status         # 认证状态
│   └── models         # 可用模型
├── cron (c)           # 定时任务
│   ├── list           # 列出任务
│   ├── add            # 添加任务
│   ├── remove         # 删除任务
│   ├── enable         # 启用任务
│   └── disable        # 禁用任务
├── skills             # 技能管理
│   ├── list           # 列出已安装
│   ├── install        # 安装技能
│   ├── remove         # 移除技能
│   ├── search         # 搜索技能
│   ├── show           # 显示详情
│   ├── list-builtin   # 列出内置
│   └── install-builtin # 安装内置
├── migrate            # 配置迁移
├── status (s)         # 系统状态
└── version (v)        # 版本信息
```

## 4. Design Rationale

### Cobra 框架选择

- 使用 `github.com/spf13/cobra` 提供标准的 CLI 模式：命令嵌套、标志解析、帮助生成。
- 每个子命令独立于 `cmd/picoclaw/internal/<name>/` 目录，便于维护和测试。

### 配置系统设计

- **双重配置源:** JSON 文件 + 环境变量，环境变量优先级更高。
- **环境变量映射:** 使用 `env:"PICOCLAW_<PATH>"` 标签自动映射（如 `PICOCLAW_AGENTS_DEFAULTS_WORKSPACE`）。
- **迁移支持:** `pkg/config/migration.go` 提供旧版配置格式迁移。

### 命令隔离原则

- 每个子命令包独立实现，通过 `New*Command()` 函数返回 `*cobra.Command`。
- 共享辅助函数集中在 `cmd/picoclaw/internal/helpers.go`。
- 配置加载通过 `internal.LoadConfig()` 统一入口。

## 5. Related Documents

- `/llmdoc/guides/cli-reference.md` - 完整的命令和标志参考
- `/llmdoc/reference/agent-api.md` - 配置结构定义
- `/llmdoc/architecture/agent-core.md` - Agent 系统架构