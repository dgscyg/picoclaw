# MuninnDB Memory Integration

## 概述

本次集成将 PicoClaw 的记忆系统从简单的文件存储升级为支持 MuninnDB 认知数据库，实现语义检索、联想回忆和认知学习能力。

## 变更文件清单

### 新增文件

| 文件 | 描述 |
|------|------|
| `pkg/muninndb/client.go` | MuninnDB REST API 客户端 |
| `pkg/muninndb/types.go` | Engram、ActivateRequest 等数据类型 |
| `pkg/muninndb/errors.go` | 错误类型定义 |
| `pkg/muninndb/client_test.go` | 客户端单元测试 |
| `pkg/agent/file_memory.go` | 文件记忆存储实现（从 memory.go 提取） |
| `pkg/agent/muninndb_memory.go` | MuninnDB 记忆存储实现 |
| `pkg/agent/memory_provider_test.go` | MemoryProvider 接口测试 |
| `pkg/config/memory.go` | 记忆配置结构 |
| `pkg/tools/memory_recall.go` | memory_recall 工具 |
| `pkg/tools/memory_store.go` | memory_store 工具 |
| `pkg/tools/memory_search.go` | memory_search 工具（预留/扩展） |
| `cmd/picoclaw/internal/memory/command.go` | memory CLI 命令入口 |
| `cmd/picoclaw/internal/memory/recall.go` | recall 子命令 |
| `cmd/picoclaw/internal/memory/store.go` | store 子命令 |
| `cmd/picoclaw/internal/memory/status.go` | status 子命令 |
| `llmdoc/architecture/memory-system.md` | 记忆系统架构文档 |
| `llmdoc/guides/how-to-configure-memory.md` | 记忆配置指南 |

### 修改文件

| 文件 | 变更描述 |
|------|----------|
| `pkg/agent/memory.go` | 重构为 MemoryProvider 接口 |
| `pkg/agent/context.go` | 使用 MemoryProvider 接口 |
| `pkg/agent/instance.go` | 根据配置选择记忆后端 |
| `pkg/config/config.go` | 添加 Memory 配置字段 |
| `pkg/config/defaults.go` | 添加记忆默认配置 |
| `cmd/picoclaw/main.go` | 注册 memory 子命令 |
| `llmdoc/index.md` | 添加记忆系统文档链接 |

## 架构设计

### MemoryProvider 接口

```go
type MemoryProvider interface {
    Recall(ctx context.Context, query string, limit int) (*MemoryQueryResult, error)
    Memorize(ctx context.Context, content string, opts MemoryWriteOptions) error
    GetMemoryContext(ctx context.Context) (string, error)
    Close() error
}
```

### 后端实现

| 后端 | 描述 | 适用场景 |
|------|------|----------|
| FileMemoryStore | 文件存储 | 单机部署、简单场景 |
| MuninnDBMemoryStore | 认知数据库 | 需要语义检索、认知学习 |

## 配置示例

```json
{
  "memory": {
    "provider": "muninndb",
    "muninndb": {
      "endpoint": "http://localhost:8475",
      "vault": "picoclaw",
      "api_key": "${MUNINNDB_API_KEY}",
      "timeout": "30s",
      "fallback_to_file": true
    }
  }
}
```

## 使用方式

### CLI 命令

```bash
# 检索记忆
picoclaw memory recall "项目决策" --limit 5

# 存储记忆
picoclaw memory store "重要决定：使用 Go 1.25" --tags decision,go --long-term

# 检查状态
picoclaw memory status
```

### LLM 工具调用

```json
// 检索记忆
{
  "name": "memory_recall",
  "arguments": { "query": "架构决策", "limit": 5 }
}

// 存储记忆
{
  "name": "memory_store",
  "arguments": { "content": "使用 SQLite 做本地缓存", "long_term": true }
}
```

## 测试覆盖

- `TestMemoryProviderInterface` - 接口契约
- `TestMockMemoryProvider_*` - Mock 实现
- `TestFileMemoryStoreImplementsMemoryProviderContract` - 文件后端
- `TestMuninnDBMemoryStoreFallbackAndContext` - MuninnDB 后端与回退
- `TestMuninnDBMemoryStoreConfigValidation` - 配置验证
- `TestNewMemoryProviderFallsBackToFile` - 初始化回退
- `TestClientActivate` - API 客户端
- `TestClientWriteEngram` - 写入测试
- `TestClientContextCancel` - 上下文取消

## 兼容性

- 完全向后兼容，默认使用 FileMemoryStore
- 不配置 memory 时行为与之前一致
- MuninnDB 不可用时自动回退到文件存储
