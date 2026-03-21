# Architecture

Architecture notes for the transparent Muninn memory-layer mission.

**What belongs here:** current code touchpoints, desired layering, and explicit non-goals.
**What does NOT belong here:** step-by-step validation instructions (use `user-testing.md`).

---

## Current PicoClaw touchpoints

- `pkg/agent/loop.go` — current agent loop and the natural insertion points before first LLM reasoning and after the turn completes.
- `pkg/agent/context.go` — current dynamic/system prompt builder; Muninn mode currently skips `GetMemoryContext()` and only adds prompt rules.
- `pkg/agent/instance.go` — Muninn mode detection, Noop memory provider selection, local `memory/` deny-list behavior.
- `pkg/agent/loop_mcp.go` — MCP registration and forced `vault` argument injection for Muninn tools.
- `pkg/tools/mcp_tool.go` — generic MCP wrapper; useful if structured Muninn results need friendlier extraction.
- `pkg/muninndb/client.go` and `pkg/muninndb/types.go` — local Muninn REST client and data structures, including `Activate`, `WriteEngram`, explanation-related fields, and vault-aware requests.
- Important environment mismatch discovered during planning: the live Muninn deployment exposes REST on `8475` and MCP on `8750`, while current PicoClaw configuration semantics still overload `memory.muninndb.mcp_endpoint`.

## Target architecture for this mission

- Keep the transparent layer inside `pkg/agent`, not in a new external proxy.
- Use a planner-oriented flow:
  1. inbound turn enters agent loop
  2. transparent recall planner optionally queries Muninn
  3. bounded relevant-memory block is injected
  4. LLM runs normally and may still call `mcp_muninn_*`
  5. transparent capture planner runs after the turn
  6. consolidation/caching/conflict logic improves later turns
- Preserve the current manual Muninn MCP exploration path; automatic recall is additive, not a replacement.
- Treat transparent recall/capture (likely REST-driven) and manual Muninn MCP exploration as two cooperating integration surfaces that may need endpoint normalization or explicit separation in code.

## Non-goals

- Do not port `mem-mesh`'s external HTTP proxy or file-based storage model.
- Do not rewrite provider adapters in `pkg/providers/*` unless a future scope change explicitly requires it.
- Do not change behavior for non-Muninn mode beyond necessary regression-safe refactors.
