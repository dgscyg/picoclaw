# How to Add a New Tool

This guide describes how to implement and register a new tool in PicoClaw.

## 1. Create the Tool Struct

Create a new file in `pkg/tools/` with a struct implementing the `Tool` interface:

```go
type MyTool struct {
    // configuration fields
}

func (t *MyTool) Name() string {
    return "my_tool"
}

func (t *MyTool) Description() string {
    return "Brief description of what the tool does"
}

func (t *MyTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "param1": map[string]any{
                "type":        "string",
                "description": "Parameter description",
            },
        },
        "required": []string{"param1"},
    }
}

func (t *MyTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
    // Implementation
    return NewToolResult("success")
}
```

## 2. Create a Constructor

Provide a constructor with configuration options:

```go
type MyToolOptions struct {
    // Configuration fields
}

func NewMyTool(opts MyToolOptions) *MyTool {
    return &MyTool{ /* initialize */ }
}
```

## 3. Register in Agent Instance

Register the tool in `pkg/agent/instance.go` during agent initialization:

```go
if cfg.Tools.MyTool.Enabled {
    registry.Register(tools.NewMyTool(tools.MyToolOptions{...}))
}
```

## 4. Optional: Async Execution

For long-running operations, implement `AsyncExecutor`:

```go
func (t *MyTool) ExecuteAsync(ctx context.Context, args map[string]any, cb AsyncCallback) *ToolResult {
    go func() {
        result := t.runLongOperation(ctx, args)
        if cb != nil {
            cb(ctx, result)
        }
    }()
    return AsyncResult("Operation started")
}
```

## 5. Verification

1. Run tests: `go test ./pkg/tools/...`
2. Build the project: `go build ./cmd/picoclaw`
3. Verify tool appears in LLM tool definitions by checking logs
