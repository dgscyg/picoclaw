# PicoClaw Documentation Index

This index serves as the entry point for LLMs to navigate the PicoClaw documentation system.

## Overview

High-level project context answering "What is this project?"

- [project-overview.md](overview/project-overview.md) - Project identity: an ultra-lightweight personal AI Assistant written in Go, designed for $10 hardware with <10MB RAM. Covers tech stack, architecture highlights, and core components.

## Guides

Step-by-step operational instructions answering "How do I do X?"

- [cli-reference.md](guides/cli-reference.md) - Complete CLI command reference with all flags, environment variables, and usage examples for picoclaw commands (onboard, agent, gateway, auth, cron, skills, migrate, status, version).
- [how-to-add-channel.md](guides/how-to-add-channel.md) - Step-by-step guide for implementing a new messaging platform channel: create package, implement Channel interface, optional capabilities, factory registration, and message handling.
- [how-to-add-provider.md](guides/how-to-add-provider.md) - Step-by-step guide for adding new LLM provider support: OpenAI-compatible configuration, protocol prefix registration, and custom provider implementation.
- [how-to-add-tool.md](guides/how-to-add-tool.md) - Step-by-step guide for implementing a new tool: create struct implementing Tool interface, register in agent instance, and optional async execution pattern.
- [how-to-configure-auth.md](guides/how-to-configure-auth.md) - Configure authentication for OpenAI (browser/device code), Anthropic (API key/setup token), and Google Antigravity. Covers credential storage and Web UI authentication.
- [how-to-configure-scheduled-tasks.md](guides/how-to-configure-scheduled-tasks.md) - Configure cron jobs and heartbeat tasks: enable/disable services, create jobs via agent conversation, manage existing jobs, and verify configuration.
- [how-to-configure-session.md](guides/how-to-configure-session.md) - Configure session persistence, history compression thresholds, model routing with light/heavy selection, workspace state persistence, and session key patterns.
- [how-to-extend-agent.md](guides/how-to-extend-agent.md) - Extend agent behavior: add new tools, customize context building, configure fallback providers, add memory types, and custom agent routing.
- [how-to-configure-memory.md](guides/how-to-configure-memory.md) - Configure memory backends: file-based storage vs MuninnDB cognitive database, environment variables, fallback behavior, and CLI commands.

## Architecture

How the system is built - the "LLM Retrieval Map" answering "How does it work?"

- [agent-core.md](architecture/agent-core.md) - Central message processing engine: AgentLoop, ContextBuilder with mtime caching, AgentInstance/Registry, MemoryStore (long-term + daily notes), fallback chain, and model routing.
- [auth-identity-system.md](architecture/auth-identity-system.md) - OAuth 2.0 with PKCE authentication, browser and device code flows, token refresh, credential storage, and cross-platform identity matching.
- [channel-system.md](architecture/channel-system.md) - Registry-based multi-platform messaging: Channel interface, BaseChannel abstraction, optional capability interfaces (TypingCapable, MessageEditor, etc.), Manager with rate limiting, and message bus integration.
- [cli-system.md](architecture/cli-system.md) - Cobra-based CLI framework: command hierarchy, configuration loading with environment variable override, and subcommand isolation pattern.
- [cron-heartbeat-system.md](architecture/cron-heartbeat-system.md) - Scheduled task subsystem: CronService with JSON persistence, HeartbeatService with activity-aware execution, SpawnTool for async subagents, and schedule types (one-time, interval, cron expression).
- [qclaw-channel.md](architecture/qclaw-channel.md) - QClaw channel for WeChat service account integration: AGP WebSocket protocol, OAuth login flow, streaming responses, prompt task tracking, and CLI commands.
- [provider-system.md](architecture/provider-system.md) - LLM provider abstraction: factory pattern, protocol prefix design, OpenAI-compatible abstraction, fallback chain with cooldown tracking, error classification, and Anthropic extended thinking support.
- [session-bus-system.md](architecture/session-bus-system.md) - Message routing and conversation persistence: three-channel MessageBus design, SessionManager with atomic JSON persistence, StateManager for workspace state, history compression (proactive/reactive), and CJK-aware token estimation.
- [tool-system.md](architecture/tool-system.md) - Extensible tool execution framework: Tool interface, ToolRegistry with context injection, dual-output ToolResult (ForLLM/ForUser), RunToolLoop iteration, sandbox abstraction, shell safety patterns, and MCP integration.
- [memory-system.md](architecture/memory-system.md) - Memory backend abstraction, file and MuninnDB implementations, fallback behavior, and prompt/tool integration data flow.

## Reference

Factual, transcribed lookup information answering "What are the specifics of X?"

- [agent-api.md](reference/agent-api.md) - Key data structures: AgentLoop, AgentInstance, ContextBuilder, MemoryStore, ThinkingLevel, Tool interface, AsyncExecutor interface, and configuration examples.
- [auth-providers.md](reference/auth-providers.md) - Supported authentication providers: OpenAI (OAuth with PKCE), Anthropic (API key/setup token), Google Antigravity (OAuth). Token lifecycle and storage location.
- [builtin-tools.md](reference/builtin-tools.md) - All 15+ built-in tools: filesystem (read_file, write_file, list_dir, edit_file, append_file), shell (exec), web (web_search, web_fetch), communication (message, send_file), subagent (subagent, spawn), skills (find_skills, install_skill), scheduling (cron), hardware (i2c, spi), and MCP tools.
- [coding-conventions.md](reference/coding-conventions.md) - Go naming conventions, interface abstraction patterns, factory registration, atomic file writes, context injection, thread safety, file organization, and testing standards.
- [git-conventions.md](reference/git-conventions.md) - Branch strategy (dev/main/release), Conventional Commits format, PR workflow with AI disclosure requirement, and squash merge process.
- [supported-providers.md](reference/supported-providers.md) - All 15+ supported LLM providers with protocol prefixes: OpenAI, Anthropic, Groq, Zhipu, Gemini, DeepSeek, Mistral, Moonshot, OpenRouter, LiteLLM, VLLM, Ollama, Nvidia, Cerebras, CLI providers, and special auth providers.
