# Coding Conventions

## 1. Core Summary

PicoClaw 遵循标准 Go 命名规范，同时采用若干项目特定的架构模式，包括接口抽象、工厂注册、原子文件写入和 Context 注入。

## 2. Source of Truth

- **项目结构:** `cmd/picoclaw/` (CLI), `pkg/` (核心库)
- **Channel 接口:** `pkg/channels/interfaces.go`, `pkg/channels/base.go`
- **Channel 注册:** `pkg/channels/registry.go`
- **Provider 接口:** `pkg/providers/types.go`
- **原子写入:** `pkg/fileutil/file.go`
- **错误处理:** `pkg/channels/errors.go`, `pkg/channels/errutil.go`

## 3. Go 基础规范

### 命名

- **CamelCase:** 使用驼峰命名，首字母大写表示导出
- **接口命名:** 单方法接口以 `-or` 或 `-able` 结尾（如 `TypingCapable`, `MessageEditor`）
- **哨兵错误:** 以 `Err` 前缀命名（如 `ErrNotRunning`, `ErrRateLimit`）

### 错误处理

```go
// 哨兵错误定义
var ErrRateLimit = errors.New("rate limited")

// 错误包装（保留原始错误链）
return fmt.Errorf("%w: %v", ErrRateLimit, rawErr)
```

参考: `pkg/channels/errors.go:1-21`, `pkg/channels/errutil.go:1-31`

## 4. 项目特定模式

### 接口抽象

核心接口定义小型、专注的契约：

- **Channel:** `pkg/channels/base.go:43-52` - 核心通道接口
- **LLMProvider:** `pkg/providers/types.go:24-33` - LLM 提供者接口

**可选能力接口**（通过类型断言使用）：

- `TypingCapable` - 支持打字指示器
- `MessageEditor` - 支持消息编辑
- `ReactionCapable` - 支持消息反应
- `PlaceholderCapable` - 支持占位消息

参考: `pkg/channels/interfaces.go:1-53`

### 工厂注册模式

Channel 通过 `init()` 自注册：

```go
// pkg/channels/telegram/init.go
func init() {
    channels.RegisterFactory("telegram", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
        return NewTelegramChannel(cfg, b)
    })
}
```

注册表使用 `sync.RWMutex` 保护并发访问。参考: `pkg/channels/registry.go:1-33`

### 原子文件写入

关键文件写入使用 temp + sync + rename 模式：

1. 创建临时文件（同目录）
2. 写入数据
3. `Sync()` 刷新到磁盘
4. 设置权限
5. 原子 `Rename()`
6. 同步目录元数据

参考: `pkg/fileutil/file.go:17-119`

### Context 注入

所有主要方法第一个参数为 `context.Context`：

```go
func (c *BaseChannel) HandleMessage(ctx context.Context, ...)
func (p LLMProvider) Chat(ctx context.Context, ...)
```

### 线程安全

使用 `sync.RWMutex` 或 `atomic.Bool` 保护共享状态：

```go
// pkg/agent/registry.go:14-18
type AgentRegistry struct {
    agents   map[string]*AgentInstance
    resolver *routing.RouteResolver
    mu       sync.RWMutex
}
```

## 5. 文件组织

```
cmd/picoclaw/                    # CLI 主程序
  internal/<command>/            # 子命令实现
    command.go                   # Cobra 命令定义
    helpers.go                   # 辅助函数

pkg/
  channels/                      # 通道核心
    <name>/                      # 各通道独立目录
      init.go                    # 工厂注册
      <name>.go                  # 通道实现
  providers/                     # LLM 提供者
  agent/                         # Agent 核心逻辑
  bus/                           # 消息总线
  config/                        # 配置管理
  tools/                         # 工具定义
```

## 6. 测试规范

- **表驱动测试:** 使用 struct slice 定义测试用例
- **断言:** 使用 `testing` 标准库（不使用 testify）
- **测试文件:** 与源文件同目录，后缀 `_test.go`

参考: `pkg/channels/base_test.go:10-57`
