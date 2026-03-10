# Provider System Architecture

## 1. Identity

- **What it is:** LLM 提供商抽象层，支持多提供商、自动故障转移和冷却机制。
- **Purpose:** 统一不同 LLM 提供商的 API 差异，提供自动降级和错误恢复能力。

## 2. Core Components

- `pkg/providers/types.go` (`LLMProvider`, `FailoverError`, `ModelConfig`): 核心接口定义和错误类型。
- `pkg/providers/factory.go` (`resolveProviderSelection`, `providerType`): 传统配置的提供商工厂，支持 15+ 协议类型。
- `pkg/providers/factory_provider.go` (`CreateProviderFromConfig`, `ExtractProtocol`): 基于 ModelConfig 的提供商创建，解析协议前缀。
- `pkg/providers/openai_compat/provider.go` (`Provider`, `Chat`, `serializeMessages`): OpenAI 兼容 API 的通用 HTTP 实现。
- `pkg/providers/fallback.go` (`FallbackChain`, `Execute`, `ResolveCandidates`): 故障转移链执行和候选解析。
- `pkg/providers/cooldown.go` (`CooldownTracker`, `MarkFailure`, `IsAvailable`): 提供商冷却状态管理。
- `pkg/providers/error_classifier.go` (`ClassifyError`, `classifyByStatus`): 错误分类和可重试性判断。
- `pkg/providers/model_ref.go` (`ModelRef`, `ParseModelRef`, `NormalizeProvider`): 模型引用解析和提供商名称规范化。
- `pkg/config/config.go` (`ModelConfig`, `GetModelConfig`, `ValidateModelList`): 模型列表配置和验证。
- `pkg/providers/anthropic/provider.go` (`Provider`, `SupportsThinking`, `applyThinkingConfig`): Anthropic 原生 SDK 实现，支持扩展思考。

## 3. Execution Flow (LLM Retrieval Map)

### 3.1 Provider Creation Flow

- **1. Config Resolution:** `pkg/config/config.go:GetModelConfig` 根据 model_name 查找配置，支持轮询负载均衡。
- **2. Protocol Parsing:** `pkg/providers/factory_provider.go:ExtractProtocol` 解析 "openai/gpt-4o" 格式，提取协议和模型 ID。
- **3. Factory Creation:** `pkg/providers/factory_provider.go:CreateProviderFromConfig` 根据协议前缀创建对应提供商实例。
- **4. Default API Base:** `pkg/providers/factory_provider.go:getDefaultAPIBase` 为 15+ 协议提供默认 API 地址。

### 3.2 Chat Request Flow

- **1. Candidate Resolution:** `pkg/providers/fallback.go:ResolveCandidatesWithLookup` 解析主模型和降级候选列表。
- **2. Cooldown Check:** `pkg/providers/fallback.go:Execute` 调用 `pkg/providers/cooldown.go:IsAvailable` 检查提供商可用性。
- **3. Request Execution:** `pkg/providers/openai_compat/provider.go:Chat` 或 `pkg/providers/anthropic/provider.go:Chat` 执行实际请求。
- **4. Error Classification:** `pkg/providers/error_classifier.go:ClassifyError` 分类错误并判断可重试性。
- **5. Fallback Decision:** 若错误可重试，切换到下一个候选；若不可用，标记冷却状态。
- **6. Success Recording:** `pkg/providers/cooldown.go:MarkSuccess` 重置提供商冷却状态。

### 3.3 Error Classification Flow

- **1. HTTP Status Check:** `pkg/providers/error_classifier.go:classifyByStatus` 映射状态码：401/403=auth, 402=billing, 429=rate_limit, 500/502/503=overloaded。
- **2. Message Pattern Match:** `pkg/providers/error_classifier.go:classifyByMessage` 使用 ~40 个模式匹配错误消息。
- **3. Retriability:** `pkg/providers/types.go:IsRetriable` 根据 FailoverReason 判断是否可重试。

### 3.4 Cooldown Calculation

- **1. Standard Cooldown:** `pkg/providers/cooldown.go:calculateStandardCooldown` 指数退避：1次=1分钟, 2次=5分钟, 3次=25分钟, 4+次=1小时。
- **2. Billing Cooldown:** `pkg/providers/cooldown.go:calculateBillingCooldown` 计费错误退避：1次=5小时, 2次=10小时, 3次=20小时, 4+次=24小时。
- **3. Window Reset:** 若上次失败超过 24 小时，重置计数器。

## 4. Design Rationale

### Protocol Prefix Design

使用 "provider/model" 格式（如 "openai/gpt-4o", "anthropic/claude-3-opus"）统一标识模型，解耦用户配置与实现细节。`NormalizeProvider` 函数处理别名映射（如 "gpt"->"openai", "claude"->"anthropic"）。

### OpenAI-Compatible Abstraction

大多数提供商使用 OpenAI 兼容 API，通过 `openai_compat.Provider` 统一处理：
- 请求/响应序列化
- 多模态内容（文本 + 图像）
- 工具调用
- 推理内容（reasoning_content）

提供商特定适配通过配置处理（如 `MaxTokensField` 字段名差异）。

### Fallback Strategy

- **Primary + Fallbacks:** `ModelConfig` 支持主模型和降级列表。
- **Load Balancing:** 相同 `model_name` 的多个配置项使用轮询选择。
- **Cooldown Tracking:** 失败提供商暂时排除，避免无效重试。

### Thinking Support

Anthropic 提供商原生支持扩展思考（Extended Thinking），通过 `ThinkingLevel` 配置：
- `low`: 4096 tokens
- `medium`: 16384 tokens
- `high`: 32000 tokens
- `xhigh`: 64000 tokens
- `adaptive`: 使用自适应思考 API

## 5. Related Documents

- `/llmdoc/reference/supported-providers.md` - 支持的提供商完整列表
- `/llmdoc/guides/how-to-add-provider.md` - 添加新提供商指南
