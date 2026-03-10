# MuninnDB 记忆功能部署指南

本指南介绍如何部署 MuninnDB 并配置 PicoClaw 使用认知数据库作为记忆后端。

## 前置条件

- Go 1.25+
- MuninnDB 服务器（见下方安装步骤）
- PicoClaw 配置文件

## 第一步：安装 MuninnDB

### 从源码编译

```bash
# 克隆仓库
git clone https://github.com/your-org/muninndb.git
cd muninndb

# 编译
go build -o muninn ./cmd/muninn

# 安装到系统路径
sudo mv muninn /usr/local/bin/
```

### 使用预编译二进制

从 [Releases](https://github.com/your-org/muninndb/releases) 页面下载对应平台的二进制文件。

## 第二步：初始化 MuninnDB

### 创建数据目录

```bash
mkdir -p ~/.muninndb/data
```

### 初始化配置

```bash
muninn init
```

按提示设置管理员密码和基本配置。

### 启动服务器

```bash
# 前台运行
muninn start

# 或作为守护进程
muninn start --daemon
```

默认监听端口：
- REST API: 8475
- MCP: 8750
- Web UI: 8476

## 第三步：创建 Vault 和 API Key

### 创建 Vault

```bash
# 创建 picoclaw 专用 vault
muninn vault create picoclaw --public

# 验证创建成功
muninn vault list
```

### 创建 API Key

```bash
# 创建具有完整权限的 API Key
muninn api-key create --vault picoclaw --mode full

# 输出示例：
# API Key: sk_xxxxxxxxxxxxxxxxxxxx
# 请妥善保存此 Key，后续无法再次查看
```

## 第四步：配置 PicoClaw

### 方法一：配置文件

编辑 `~/.picoclaw/config.json`：

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

### 方法二：环境变量

```bash
# Linux/macOS
export MUNINNDB_ENDPOINT=http://localhost:8475
export MUNINNDB_VAULT=picoclaw
export MUNINNDB_API_KEY=sk_xxxxxxxxxxxxxxxxxxxx
export MUNINNDB_TIMEOUT=30s

# Windows PowerShell
$env:MUNINNDB_ENDPOINT = "http://localhost:8475"
$env:MUNINNDB_VAULT = "picoclaw"
$env:MUNINNDB_API_KEY = "sk_xxxxxxxxxxxxxxxxxxxx"
$env:MUNINNDB_TIMEOUT = "30s"
```

## 第五步：验证连接

### 检查 MuninnDB 状态

```bash
curl http://localhost:8475/api/v1/system/stats
```

### 测试记忆功能

```bash
# 启动 PicoClaw
picoclaw serve

# 在另一个终端测试
picoclaw memory status

# 输出示例：
# Memory Provider: muninndb
# Endpoint: http://localhost:8475
# Vault: picoclaw
# Connection: OK
```

### 存储和检索记忆

```bash
# 存储一条记忆
picoclaw memory store "PicoClaw 项目使用 Go 1.25 开发" --tags project,go --long-term

# 检索记忆
picoclaw memory recall "项目开发语言" --limit 5

# 输出示例：
# [1] PicoClaw 项目使用 Go 1.25 开发
#     Tags: project, go
#     Relevance: 0.95
```

## 高级配置

### Plasticity 预设

MuninnDB 支持认知参数预设，影响记忆检索行为：

```bash
# 设置 vault 为长期知识库模式
muninn vault update picoclaw --plasticity reference
```

| 预设 | Hebbian | Temporal | HopDepth | 适用场景 |
|------|---------|----------|----------|----------|
| default | true | true | 2 | 通用 |
| reference | true | false | 3 | 长期知识库 |
| scratchpad | false | true | 0 | 短期工作记忆 |
| knowledge-graph | true | true | 4 | 知识图谱 |

### 嵌入模型配置

MuninnDB 默认使用本地 ONNX 模型（bge-small-en-v1.5, 384 维）。如需使用云端模型：

```bash
# 设置 OpenAI 嵌入
muninn config set embed.provider openai
muninn config set embed.model text-embedding-3-small
muninn config set embed.api_key ${OPENAI_API_KEY}
```

## 故障排除

### 连接失败

1. 检查 MuninnDB 是否运行：
   ```bash
   muninn status
   ```

2. 检查端口是否开放：
   ```bash
   curl http://localhost:8475/api/v1/system/stats
   ```

3. 检查 API Key 是否有效：
   ```bash
   curl -H "Authorization: Bearer YOUR_API_KEY" \
     http://localhost:8475/api/v1/vault/picoclaw/activate \
     -d '{"query": "test", "limit": 1}'
   ```

### 记忆未出现在检索结果中

1. 确认记忆已成功写入：
   ```bash
   picoclaw memory status
   ```

2. 检查 MuninnDB 日志：
   ```bash
   muninn logs --tail 100
   ```

3. 尝试降低相似度阈值（在 MuninnDB 中配置）

### Fallback 行为

当 `fallback_to_file: true` 时，如果 MuninnDB 不可用：
- 系统自动切换到文件存储
- 日志中会记录警告信息
- Agent 继续正常运行

查看日志确认回退：
```bash
picoclaw serve --log-level debug 2>&1 | grep -i memory
```

## 生产部署建议

### 1. 使用 HTTPS

```json
{
  "memory": {
    "muninndb": {
      "endpoint": "https://muninndb.your-domain.com",
      ...
    }
  }
}
```

### 2. 配置 TLS

在 MuninnDB 服务器端配置 TLS：

```bash
muninn config set tls.enabled true
muninn config set tls.cert /path/to/cert.pem
muninn config set tls.key /path/to/key.pem
```

### 3. 定期备份

```bash
# 导出 vault
muninn vault export picoclaw --output picoclaw-backup.muninn

# 定时备份（crontab）
0 2 * * * muninn vault export picoclaw --output /backup/picoclaw-$(date +\%Y\%m\%d).muninn
```

### 4. 监控

启用 Prometheus 指标：
```bash
muninn config set metrics.enabled true
muninn config set metrics.port 9090
```

## 相关文档

- [MuninnDB 官方文档](https://github.com/your-org/muninndb/docs)
- [记忆系统架构](../llmdoc/architecture/memory-system.md)
- [记忆配置指南](../llmdoc/guides/how-to-configure-memory.md)
