# PicoClaw x MuninnDB Memory Upgrade Plan

## 1. Goal

Upgrade PicoClaw from using MuninnDB only as a basic remote memory backend to using MuninnDB as a higher-value cognitive memory service.

## 2. First-Round Delivery

The first round focuses on 4 high-value capabilities:

1. `link`
2. `traverse`
3. `explain`
4. `contradictions`

## 3. Task Breakdown

- extend `pkg/muninndb/types.go`
- extend `pkg/muninndb/client.go` and tests
- add `pkg/tools/muninn_*.go`
- register tools in `pkg/agent/instance.go`
- add `cmd/picoclaw/internal/memory/*` verification commands

## 4. Acceptance Criteria

- client methods exist for link, traverse, explain, contradictions
- dedicated MuninnDB tools are registered only when supported
- matching CLI commands exist for manual verification
- file-memory mode is not broken
- focused tests pass for touched packages
