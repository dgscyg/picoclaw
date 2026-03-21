# Muninn Transparent Layer

Mission-specific knowledge for the transparent Muninn proxy layer.

**What belongs here:** recall/capture/consolidation expectations, preferred logging vocabulary, and the allowed borrowing from `mem-mesh`.
**What does NOT belong here:** runtime service commands (use `.factory/services.yaml`).

---

## Approved design references

- Primary design: `llmdoc/architecture/transparent-proxy-layer.md`
- Secondary reference only for patterns: `../mem-mesh`
  - borrow: front-loaded injection and asynchronous extraction mindset
  - do not borrow: external proxy topology, file-backed memory store, or cross-tool HTTP middleware
- Integration reality in this repo/environment:
  - REST memory operations are currently available on `127.0.0.1:8475`
  - Muninn MCP listens separately on `127.0.0.1:8750`
  - current PicoClaw MCP negotiation against the live Muninn listener is not yet healthy, so the mission may need to fix endpoint/transport compatibility before manual `mcp_muninn_*` validation can pass

## Expected recall behavior

- Trigger only in Muninn mode.
- Query from the current user message plus available session/channel/sender context.
- Inject a bounded dedicated memory block such as `Relevant Memory (Muninn Auto-Recall)`.
- Keep injected memory out of persisted session history.
- Preserve manual `mcp_muninn_*` tool usage for deeper exploration.

## Expected capture behavior

- Run after the turn completes; never block the current reply.
- Start with high-confidence durable categories only:
  - user preferences
  - contact mappings
  - project decisions
  - explicit constraints
- Drop noisy or ambiguous candidates.
- Deduplicate repeated confirmations.

## Expected consolidation behavior

- Prefer concise `why` / rationale fields over raw activation dumps.
- Allow limited recall caching for closely related turns.
- Invalidate or refresh cache on material context change.
- Handle conflicting facts explicitly rather than silently reinforcing stale memory.

## Preferred structured log vocabulary

Use existing PicoClaw logger style, but keep event names/facets consistent. Suggested vocabulary:

- `muninn_auto_recall_start`
- `muninn_auto_recall_hit`
- `muninn_auto_recall_miss`
- `muninn_auto_recall_timeout`
- `muninn_auto_recall_error`
- `muninn_auto_capture_candidate`
- `muninn_auto_capture_written`
- `muninn_auto_capture_dropped`
- `muninn_auto_capture_error`
- `muninn_consolidation_cache_hit`
- `muninn_consolidation_cache_miss`
- `muninn_consolidation_conflict`

Keep logs structured, include the vault name, and never emit secrets.
