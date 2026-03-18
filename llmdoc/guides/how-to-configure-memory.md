# 如何配置记忆系统

本指南介绍如何在 PicoClaw 中配置记忆后端，以及如何在文件存储和 MuninnDB 之间切换。

## 1. 默认行为

默认情况下，PicoClaw 使用文件记忆后端：

- 长期记忆写入 `<workspace>/memory/MEMORY.md`
- 每日笔记写入 `<workspace>/memory/YYYYMM/YYYYMMDD.md`

默认配置由 `pkg/config/defaults.go:44-52` 提供：

- `memory.provider = "file"`
- `memory.muninndb.vault = "default"`
- `memory.muninndb.timeout = "30s"`

## 2. 使用文件后端

```json
{
  "memory": {
    "provider": "file",
    "file": {
      "workspace": "./workspace/memory"
    }
  }
}
```

说明：

- `provider` 设为 `file` 时，Agent 会使用 `FileMemoryStore`
- 实际文件仍按工作区的 `memory/` 目录组织
- 适合单机部署、调试和无外部依赖场景

## 3. 使用 MuninnDB 后端

```json
{
  "memory": {
    "provider": "muninndb",
    "muninndb": {
      "endpoint": "http://127.0.0.1:8080",
      "vault": "default",
      "api_key": "your-api-key",
      "timeout": "30s",
      "fallback_to_file": true
    }
  }
}
```

字段说明：

| 字段 | 含义 |
|------|------|
| `endpoint` | MuninnDB HTTP 服务地址 |
| `vault` | 记忆仓名称 |
| `api_key` | Bearer Token 形式认证密钥 |
| `timeout` | HTTP 请求超时时间，如 `30s` |
| `fallback_to_file` | 远程失败时是否回退到文件后端 |

校验规则见 `pkg/config/memory.go:67-87`：

- 当 `provider = "muninndb"` 时，`endpoint` 必填
- `vault` 不能为空，默认值为 `default`

## 4. 使用环境变量

配置文件支持在字符串字段中引用环境变量。例如：

```json
{
  "memory": {
    "provider": "muninndb",
    "muninndb": {
      "endpoint": "${MUNINNDB_ENDPOINT}",
      "vault": "${MUNINNDB_VAULT}",
      "api_key": "${MUNINNDB_API_KEY}",
      "timeout": "${MUNINNDB_TIMEOUT}"
    }
  }
}
```

启动前设置环境变量：

```bash
export MUNINNDB_ENDPOINT=http://127.0.0.1:8080
export MUNINNDB_VAULT=default
export MUNINNDB_API_KEY=secret
export MUNINNDB_TIMEOUT=30s
```

## 5. 切换后端

切换方式很简单，只需要修改 `memory.provider`：

- 切到本地文件：`file`
- 切到远程 MuninnDB：`muninndb`

Agent 初始化时会调用 `pkg/agent/instance.go:268` 的 `newMemoryProvider`：

- 配置合法且连接参数完整时，创建 `MuninnDBMemoryStore`
- 如果初始化失败，会自动回退到 `FileMemoryStore`

这意味着即使远程后端配置出错，系统也不会因为记忆模块初始化失败而完全不可用。

## 6. 常用命令

记忆系统相关 CLI 命令位于 `picoclaw memory`：

```bash
picoclaw memory recall --query "最近的项目决策"
picoclaw memory store --content "用户偏好使用简体中文" --long-term
picoclaw memory status
```

如果你已经切换到 MuninnDB，这些命令会通过当前配置的 `MemoryProvider` 访问远程记忆后端。

## 7. 故障排除

### 7.1 提示缺少 endpoint

现象：启动时报错 `memory.muninndb.endpoint is required`

处理方法：

- 检查 `memory.provider` 是否设为 `muninndb`
- 确认 `memory.muninndb.endpoint` 已配置
- 如果使用环境变量，确认变量已正确导出

### 7.2 提示 timeout 解析失败

现象：启动时报错 `parse muninndb timeout`

处理方法：

- 使用 Go duration 格式，例如 `5s`、`30s`、`1m`
- 不要写成 `30` 或 `30sec`

### 7.3 MuninnDB 短暂不可用

现象：召回或写入报远程错误

处理方法：

- 建议开启 `fallback_to_file: true`
- 检查 MuninnDB 服务地址、端口和 API Key
- 检查网络连通性以及 Vault 是否存在

### 7.4 记忆没有出现在提示词中

处理方法：

- 确认 `GetMemoryContext` 返回非空
- 文件后端检查 `memory/MEMORY.md` 与最近 daily notes 是否有内容
- MuninnDB 后端检查 `activate` 接口是否能返回 engram

## 8. 验证配置是否生效

你可以通过以下方式验证：

1. 使用 `picoclaw memory store` 写入一条测试记忆
2. 使用 `picoclaw memory recall` 查询关键词
3. 启动 Agent 并观察是否能利用刚写入的记忆回答问题

如果启用了 MuninnDB 且开启文件回退，也可以同时检查本地 `memory/` 目录，以确认回退链路是否按预期工作。
