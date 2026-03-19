# User Testing

Testing surface notes for validators and workers.

**What belongs here:** validation surfaces, dry-run findings, concurrency guidance, and practical gotchas.
**What does NOT belong here:** implementation design rationale (use `architecture.md` / `muninn-transparent-layer.md`).

---

## Validation Surface

### Surface: live Muninn integration

- Reuse the existing REST service at `http://127.0.0.1:8475` and the existing MCP listener at `http://127.0.0.1:8750`.
- Validate against the dedicated vault `picoclaw-transparent-layer-test` only.
- Primary assertions covered here:
  - auto recall hit/miss behavior
  - capture write/drop behavior
  - consolidation refresh/conflict behavior

### Surface: claweb end-to-end flow

- Allowed path: sibling frontdoor at `../claweb/access/frontdoor`
- Required local flow:
  1. start isolated PicoClaw gateway
  2. start isolated claweb frontdoor
  3. run `.factory/scripts/claweb-e2e.mjs`
- Expected path: `POST /login -> WS /ws -> hello -> ready -> assistant reply`

## Dry-Run Findings

- `go test ./pkg/agent ./pkg/config ./pkg/mcp ./pkg/muninndb` passed during planning.
- `go test ./pkg/tools -run "Test(NewMCPTool|MCPTool_.*|MergeMCPArgs_ForcedOverridesProvided)"` passed during planning.
- `go test ./pkg/channels/claweb` passed during planning.
- Local claweb dry run succeeded end-to-end with an assistant reply of `pong`.
- Planning also surfaced a current infrastructure/code mismatch: PicoClaw's current Muninn MCP client failed to negotiate successfully against the live local MCP listener, so workers should expect early features or fixes in this mission to resolve MCP endpoint/transport compatibility before relying on manual `mcp_muninn_*` validation.

## Validation Concurrency

### Surface: claweb

- Machine profile observed during planning: 16 logical CPUs, ~15.69 GB RAM.
- Dry-run free RAM moved from ~4.12 GB to ~3.98 GB.
- Observed process footprint during dry run:
  - frontdoor: ~49 MB working set / ~62 MB private memory
  - picoclaw gateway: ~22 MB working set / ~20 MB private memory
- User-approved max concurrent validators for claweb: **3**.
- Validators may reduce concurrency if isolation or runtime contention makes `3` unsafe.

### Surface: live Muninn

- Treat Muninn as a shared dependency, not a multiply-spawned service.
- Avoid parallel flows that intentionally mutate the same fact set in the dedicated vault unless the test is explicitly about dedupe/conflict handling.

## Practical Gotchas

- The upstream frontdoor smoke scripts currently assume HTTPS helpers even when pointed at `http://` URLs. For local HTTP validation, use `.factory/scripts/claweb-e2e.mjs` instead.
- Repository-wide `pkg/tools/shell_test.go` coverage is currently a pre-existing Windows blocker unrelated to this mission. The mission baseline therefore skips only the failing shell-tool cases in `.factory/services.yaml`; do not treat those exclusions as Muninn regressions unless scope explicitly expands.
- Validation setup must not write to the default Muninn vault.
- If the dedicated test vault cannot be written and no safe code-path exists to create it implicitly, stop and return to orchestrator instead of contaminating another vault.
