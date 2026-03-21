---
name: go-memory-worker
description: Implement transparent Muninn memory-layer features in PicoClaw with Go tests first, structured logging, and claweb/live-Muninn verification.
---

# Go Memory Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE for transparent Muninn memory-layer features.

## When to Use This Skill

Use this skill for features that modify PicoClaw's Go-based agent/runtime behavior for:
- Muninn transparent recall, injection, capture, or consolidation
- Muninn MCP coexistence and vault enforcement behavior
- Claweb-backed end-to-end validation helpers that support the transparent memory mission
- Related tests in `pkg/agent`, `pkg/muninndb`, `pkg/tools`, `pkg/config`, or `pkg/channels/claweb`

## Work Procedure

1. Read the mission artifacts first: `mission.md`, `AGENTS.md`, `validation-contract.md`, `.factory/library/*.md`, and `.factory/services.yaml`.
2. Confirm the feature's claimed assertions and verification steps before editing code. If the feature description is too ambiguous to know what must pass, return to orchestrator.
3. Run `.factory/init.sh` if startup did not already do so. Verify that:
   - the validation vault target is `default`
   - the local dry-run claweb config/token files exist
   - the existing Muninn service on `127.0.0.1:8475` is reachable
4. Write tests first (red). Add or extend focused tests that fail for the missing behavior before implementing code. Prefer the smallest package scope that proves the feature.
5. Implement the feature in the existing PicoClaw architecture. Keep non-Muninn behavior unchanged, preserve `mcp_muninn_*` interoperability, and use structured logs via existing logger patterns.
6. Run targeted tests until green. Then run the mission-level test command from `.factory/services.yaml` if the feature touches shared behavior.
7. If the feature affects end-to-end behavior or any cross-turn flow, manually verify through the allowed claweb surface or the designated direct-agent harness. Use the approved validation vault `default` only.
8. Run lint before handoff. Do not hand off with failing lint or tests unless the orchestrator explicitly approved that exception.
9. Inspect your diff for accidental secret exposure, default-vault writes, or non-claweb channel changes. Revert those before finishing.
10. In the handoff, be explicit about:
    - which assertions are now fully testable
    - the validation vault used in verification
    - whether claweb e2e was exercised
    - any Muninn limitations, misses, or recovery behavior you observed

## Example Handoff

```json
{
  "salientSummary": "Implemented transparent auto-recall in Muninn mode with a bounded Relevant Memory block and graceful miss handling; preserved manual mcp_muninn deepening and confirmed non-Muninn mode stayed unchanged.",
  "whatWasImplemented": "Added a Muninn recall planner and prompt-injection path under pkg/agent, kept the injected block out of persisted history, reused the configured validation vault for both auto recall and manual Muninn MCP calls, and added focused regression tests for miss/error handling plus non-Muninn behavior.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      {
        "command": "go test -p 3 ./pkg/agent -run \"Test.*Muninn.*Recall|Test.*RelevantMemory|Test.*History\" -count=1",
        "exitCode": 0,
        "observation": "Focused recall and history tests passed after the injection block stopped leaking into persisted session history."
      },
      {
        "command": "go test -p 3 ./pkg/tools -run \"Test(NewMCPTool|MCPTool_.*|MergeMCPArgs_ForcedOverridesProvided)\" -count=1",
        "exitCode": 0,
        "observation": "Manual Muninn MCP interoperability still worked and forced vault arguments remained intact."
      },
      {
        "command": "node ./.factory/scripts/lint-changed.mjs",
        "exitCode": 0,
        "observation": "Change-scoped lint passed with no new findings."
      }
    ],
    "interactiveChecks": [
      {
        "action": "Seeded the approved validation vault `default` with a known preference, started the local gateway/frontdoor validation stack, then asked a recall-dependent claweb question without manually invoking a Muninn tool.",
        "observed": "Assistant answered using the seeded preference; gateway logs showed auto recall hit and injected_count > 0 while the run stayed within the approved validation-vault boundary."
      }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "pkg/agent/muninn_proxy_test.go",
        "cases": [
          {
            "name": "auto recall injects bounded relevant memory block",
            "verifies": "Muninn-mode turns receive summarized recall context without persisting the block into session history."
          },
          {
            "name": "recall miss still returns assistant reply",
            "verifies": "Timeouts/misses degrade gracefully and only structured logs record the failure reason."
          }
        ]
      }
    ]
  },
  "discoveredIssues": [
    {
      "severity": "medium",
      "description": "The upstream claweb smoke scripts still assume HTTPS even for local HTTP validation, so the mission-specific `.factory/scripts/claweb-e2e.mjs` harness remains necessary for local user-surface checks.",
      "suggestedFix": "Keep using the mission harness unless a future mission explicitly patches the sibling frontdoor scripts."
    }
  ]
}
```

## When to Return to Orchestrator

- The feature requires writing to any Muninn vault other than the approved validation vault `default`, or any non-claweb channel, to proceed.
- The approved validation vault `default` cannot be written or validated and there is no safe code-only fallback.
- Claweb validation requires changes in the sibling `../claweb` repo beyond the mission's approved validation harness usage.
- The needed behavior would require a provider-adapter rewrite or a scope expansion not covered by the current feature list.
