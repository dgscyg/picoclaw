# 记忆系统架构

## 1. Identity

- **What it is:** PicoClaw 的记忆存储系统，支持可切换的记忆后端。
- **Purpose:** 为 AI Agent 提供持久化记忆能力，支持本地文件存储与 MuninnDB 语义检索。

## 2. High-Level Description

记忆系统采用 Strategy Pattern，通过 `pkg/agent/memory.go:21` 的 `MemoryProvider` 接口统一抽象记忆读写、检索和上下文构建能力。当前有两种实现：

- **FileMemoryStore**：默认后端，使用工作区文件保存长期记忆与每日笔记。
- **MuninnDBMemoryStore**：远程后端，通过 HTTP API 调用 MuninnDB 完成语义召回和 engram 写入。

系统在初始化阶段根据配置选择后端，在需要时可自动回退到文件存储，保证 Agent 记忆能力在远程服务异常时仍可用。

## 3. Core Components

### 3.1 MemoryProvider 接口

`pkg/agent/memory.go:21` 定义统一契约：

- `Recall(ctx, query, limit)`：按查询召回记忆
- `Memorize(ctx, content, opts)`：写入记忆
- `GetMemoryContext(ctx)`：生成提示词使用的记忆上下文
- `Close()`：释放资源

该接口让 `ContextBuilder`、CLI 和工具层都只依赖抽象，不感知具体后端。

### 3.2 FileMemoryStore

`pkg/agent/file_memory.go` 实现本地文件存储：

- 长期记忆：`memory/MEMORY.md`
- 每日笔记：`memory/YYYYMM/YYYYMMDD.md`

主要行为：

- `Memorize(LongTerm=true)` 覆盖写入长期记忆
- 普通 `Memorize` 追加到当天 daily note
- `Recall` 对长期记忆和最近 7 天笔记做简单字符串匹配
- `GetMemoryContext` 拼装长期记忆和最近 3 天笔记为统一 Markdown 片段

底层通过原子写入保证文件可靠落盘。

### 3.3 MuninnDBMemoryStore

`pkg/agent/muninndb_memory.go` 实现远程 MuninnDB 后端：

- `Recall` 调用 `pkg/muninndb/client.go:46` 的 `Activate`
- `Memorize` 调用 `pkg/muninndb/client.go:61` 的 `WriteEngram`
- `GetMemoryContext` 基于召回结果构建长期记忆上下文

写入时会将内容映射为 `Engram`，并根据 `MemoryWriteOptions` 自动补充标签：

- `long-term`
- `daily-note`

返回结果会通过 `formatEngramForMemory` 格式化，补充 `Tags`、`Concept`、`Key Points` 等信息。

### 3.4 MuninnDB 客户端

`pkg/muninndb/client.go` 提供 HTTP 客户端封装：

- 自动设置 `Authorization: Bearer <api_key>`
- 请求与响应统一走 JSON 编解码
- 对 429/5xx 和网络错误进行最多 3 次重试
- 使用 `ErrTemporary` 与 `ErrRequest` 区分临时错误和请求错误

## 4. Configuration

```json
{
  "memory": {
    "provider": "muninndb",
    "file": {
      "workspace": "./workspace/memory"
    },
    "muninndb": {
      "endpoint": "http://127.0.0.1:8080",
      "vault": "default",
      "api_key": "${MUNINNDB_API_KEY}",
      "timeout": "30s",
      "fallback_to_file": true
    }
  }
}
```

配置结构位于 `pkg/config/memory.go`。

关键字段：

- `memory.provider`：`file` 或 `muninndb`
- `memory.muninndb.endpoint`：MuninnDB 服务地址
- `memory.muninndb.vault`：Vault 名称，默认 `default`
- `memory.muninndb.api_key`：Bearer Token
- `memory.muninndb.timeout`：HTTP 超时
- `memory.muninndb.fallback_to_file`：远程失败时启用文件回退

## 5. Execution Flow

### 记忆上下文构建流程

```text
ContextBuilder.BuildMessages()
    ↓
MemoryProvider.GetMemoryContext(ctx)
    ↓
[FileMemoryStore 或 MuninnDBMemoryStore]
    ↓
返回格式化 Markdown
    ↓
注入系统提示词
```

### 记忆写入流程

```text
CLI / memory_store 工具
    ↓
MemoryProvider.Memorize(ctx, content, opts)
    ↓
[FileMemoryStore] 写入本地文件
    或
[MuninnDBMemoryStore] 调用 WriteEngram
    ↓
失败时按配置回退到 FileMemoryStore
```

### 记忆搜索流程

```text
memory_search 工具
    ↓
MemoryProvider.Recall(ctx, query, limit)
    ↓
返回命中的记忆条目
```

## 6. Backend Selection and Fallback

`pkg/agent/instance.go:268` 的 `newMemoryProvider` 负责初始化：

1. 未配置时默认返回 `FileMemoryStore`
2. 当 `memory.provider = "muninndb"` 时尝试创建 `MuninnDBMemoryStore`
3. 若创建失败，记录日志并自动回退到 `FileMemoryStore`

运行期回退由 `MuninnDBMemoryStore` 自身处理：

- `Recall` 失败时可回退到文件后端
- `Memorize` 失败时可回退到文件后端
- `GetMemoryContext` 失败时可回退到文件后端

## 7. Tool and CLI Integration

LLM 可调用的记忆工具位于 `pkg/tools/memory_*.go`：

| 工具 | 描述 |
|------|------|
| `memory_recall` | 检索相关记忆 |
| `memory_store` | 写入新记忆 |
| `memory_search` | 按查询搜索记忆 |

CLI 命令位于 `cmd/picoclaw/internal/memory/`：

- `picoclaw memory recall`
- `picoclaw memory store`
- `picoclaw memory status`

这些入口都会走统一的 `MemoryProvider` 抽象，因此无需为不同后端编写不同上层逻辑。

## 8. Testing

当前测试覆盖重点如下：

- `pkg/muninndb/client_test.go`：成功请求、重试、请求错误、上下文取消
- `pkg/agent/memory_test.go`：接口契约、文件后端行为、MuninnDB 回退行为、初始化失败回退

这些测试确保记忆系统在后端切换、网络异常和提示词构建时行为稳定。

## 9. Related Documents

- `/llmdoc/guides/how-to-configure-memory.md` - 记忆配置指南
- `/llmdoc/architecture/agent-core.md` - Agent 核心与上下文构建
- `/llmdoc/reference/agent-api.md` - Agent 相关 API 结构
