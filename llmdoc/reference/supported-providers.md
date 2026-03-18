# Supported LLM Providers

本文档列出 PicoClaw 支持的所有 LLM 提供商及其协议前缀。

## 1. Core Summary

PicoClaw 支持 15+ HTTP API 提供商、CLI 集成提供商和特殊认证提供商。使用协议前缀格式 `provider/model` 统一标识模型，如 `openai/gpt-4o`。OpenAI 兼容提供商通过通用 HTTP 客户端实现，Anthropic 等特殊提供商使用原生 SDK。

## 2. Source of Truth

- **Primary Code:** `pkg/providers/factory_provider.go:getDefaultAPIBase` - 默认 API 地址定义
- **Primary Code:** `pkg/providers/model_ref.go:NormalizeProvider` - 提供商别名映射
- **Configuration:** `pkg/config/config.go:ModelConfig` - 模型配置结构
- **Related Architecture:** `/llmdoc/architecture/provider-system.md` - 提供商系统架构

## 3. HTTP API Providers

| 协议前缀 | 提供商 | 默认 API Base |
|---------|--------|---------------|
| `openai` | OpenAI | `https://api.openai.com/v1` |
| `anthropic` | Anthropic | 原生 SDK |
| `groq` | Groq | `https://api.groq.com/openai/v1` |
| `zhipu` / `glm` | 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4` |
| `gemini` / `google` | Google Gemini | `https://generativelanguage.googleapis.com/v1beta` |
| `deepseek` | DeepSeek | `https://api.deepseek.com` |
| `mistral` | Mistral AI | `https://api.mistral.ai/v1` |
| `moonshot` | Moonshot | `https://api.moonshot.cn/v1` |
| `openrouter` | OpenRouter | `https://openrouter.ai/api/v1` |
| `litellm` | LiteLLM | `http://localhost:4000/v1` |
| `vllm` | VLLM | `http://localhost:8000/v1` |
| `ollama` | Ollama | `http://localhost:11434/v1` |
| `nvidia` | Nvidia NIM | `https://integrate.api.nvidia.com/v1` |
| `cerebras` | Cerebras | `https://api.cerebras.ai/v1` |
| `vivgrid` | Vivgrid | 自定义配置 |
| `avian` | Avian | 自定义配置 |
| `volcengine` | 火山引擎 | `https://ark.cn-beijing.volces.com/api/v3` |
| `qwen` | 通义千问 | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| `ssyun` / `shengsuanyun` | 算说云 | 自定义配置 |

## 4. CLI Integration Providers

| 协议前缀 | 提供商 | 实现文件 |
|---------|--------|----------|
| `claude-cli` | Claude CLI | `pkg/providers/claude_cli_provider.go` |
| `codex-cli` | Codex CLI | `pkg/providers/codex_cli_provider.go` |

## 5. Special Auth Providers

| 提供商 | 认证方式 | 实现文件 |
|--------|----------|----------|
| GitHub Copilot | OAuth + gRPC/stdio | `pkg/providers/github_copilot_provider.go` |
| Antigravity (Google Cloud Code Assist) | OAuth | `pkg/providers/antigravity_provider.go` |
| Codex OAuth | OAuth + Token Refresh | `pkg/providers/codex_provider.go` |

## 6. Provider Aliases

`pkg/providers/model_ref.go:NormalizeProvider` 定义的别名映射：

| 别名 | 规范名称 |
|-----|---------|
| `gpt` | `openai` |
| `claude` | `anthropic` |
| `glm` | `zhipu` |
| `google` | `gemini` |
| `ssyun` | `shengsuanyun` |

## 7. Configuration Example

```json
{
  "model_list": [
    {
      "model_name": "smart",
      "model": "openai/gpt-4o",
      "api_key": "sk-xxx",
      "fallbacks": ["anthropic/claude-3-opus", "deepseek/deepseek-chat"]
    },
    {
      "model_name": "fast",
      "model": "groq/llama-3.1-70b-versatile",
      "api_key": "gsk_xxx"
    }
  ]
}
```
