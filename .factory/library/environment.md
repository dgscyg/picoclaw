# Environment

Environment variables, external dependencies, and setup notes for the transparent Muninn mission.

**What belongs here:** required services, local dependency locations, generated validation files, and secret-handling rules.
**What does NOT belong here:** service start/stop commands and ports (use `.factory/services.yaml`).

---

- Reuse the existing Muninn REST service on `http://127.0.0.1:8475`.
- Reuse the existing Muninn MCP listener on `http://127.0.0.1:8750`.
- User-approved boundary change 2026-03-19: validation now targets the default Muninn vault `default`.
- Local claweb validation depends on the sibling checkout at `../claweb/access/frontdoor`.
- `.factory/init.sh` generates these untracked local validation files under `tmp/`:
  - `tmp/claweb.token`
  - `tmp/claweb-dryrun.json`
- The dry-run config is built from the local source config (`config/config.json` by default) so secrets stay local and are not copied into committed artifacts.
- Current local reality: `config.memory.muninndb.mcp_endpoint` has been pointing at the REST port, but the live Muninn deployment exposes MCP separately on `8750`. Workers may need to normalize this mismatch in product code.
- Current local lint reality: the installed `golangci-lint` 2.4.0 binary rejects the disabled `modernize` entry in `.golangci.yaml`, and repo-wide `golangci-lint run` also reports many legacy non-Muninn findings. `.factory/scripts/lint-changed.mjs` now provides the approved change-scoped wrapper used by `commands.lint`; keep reporting the branch-wide lint debt separately.
- Tooling confirmed available during planning: `go`, `node`, `npm`, `bash`, `golangci-lint`.
- `muninn` CLI was not found during planning; workers should not assume CLI-based vault administration is available.
