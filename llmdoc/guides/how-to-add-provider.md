# How to Add a New LLM Provider

本指南说明如何为 PicoClaw 添加新的 LLM 提供商支持。

## 1. 确定提供商类型

首先确定新提供商的 API 类型：

- **OpenAI 兼容 API:** 大多数提供商（推荐，只需配置）
- **原生 SDK:** 需要特殊处理（如 Anthropic）
- **CLI 集成:** 通过命令行工具调用（如 Claude CLI）
- **OAuth 认证:** 需要特殊认证流程（如 GitHub Copilot）

## 2. OpenAI 兼容提供商（配置方式）

对于 OpenAI 兼容 API，只需在配置中添加：

```json
{
  "model_list": [{
    "model_name": "my-provider-model",
    "model": "myprovider/model-id",
    "api_key": "your-api-key",
    "api_base": "https://api.myprovider.com/v1"
  }]
}
```

然后在 `pkg/providers/factory_provider.go:getDefaultAPIBase` 添加默认 API 地址。

## 3. 添加新协议前缀

编辑 `pkg/providers/model_ref.go:NormalizeProvider` 添加提供商别名：

```
"myalias" -> "myprovider"
```

## 4. 自定义提供商实现

若需要自定义实现，创建新文件 `pkg/providers/myprovider/provider.go`：

1. **实现 `LLMProvider` 接口** (`pkg/providers/types.go`):
   - `Chat(ctx context.Context, messages []Message, opts ...Option) (*LLMResponse, error)`
   - `GetDefaultModel() string`

2. **注册到工厂** (`pkg/providers/factory_provider.go`):
   - 在 `CreateProviderFromConfig` 添加 case 分支
   - 在 `providerType` 枚举添加新类型

## 5. 验证实现

运行测试验证新提供商：

```bash
go test ./pkg/providers/... -v
```

检查配置验证：

```bash
go test ./pkg/config/... -v
```

## 6. 更新文档

在 `/llmdoc/reference/supported-providers.md` 添加新提供商信息。
