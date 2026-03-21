// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package agent

import (
	"context"
	"testing"
)

// MockMemoryProvider implements MemoryProvider for testing
 type MockMemoryProvider struct {
	RecallFunc     func(ctx context.Context, query string, limit int) (*MemoryResult, error)
	MemorizeFunc   func(ctx context.Context, content string, opts MemorizeOptions) error
	GetContextFunc func(ctx context.Context) (string, error)
	CloseFunc      func() error
}

func (m *MockMemoryProvider) Recall(ctx context.Context, query string, limit int) (*MemoryResult, error) {
	if m.RecallFunc != nil {
		return m.RecallFunc(ctx, query, limit)
	}
	return &MemoryResult{Entries: []string{}}, nil
}

func (m *MockMemoryProvider) Memorize(ctx context.Context, content string, opts MemorizeOptions) error {
	if m.MemorizeFunc != nil {
		return m.MemorizeFunc(ctx, content, opts)
	}
	return nil
}

func (m *MockMemoryProvider) GetMemoryContext(ctx context.Context) (string, error) {
	if m.GetContextFunc != nil {
		return m.GetContextFunc(ctx)
	}
	return "", nil
}

func (m *MockMemoryProvider) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func TestMemoryProviderInterface(t *testing.T) {
	var _ MemoryProvider = (*MockMemoryProvider)(nil)
}

func TestMockMemoryProvider_Recall(t *testing.T) {
	mock := &MockMemoryProvider{
		RecallFunc: func(ctx context.Context, query string, limit int) (*MemoryResult, error) {
			return &MemoryResult{
				Entries: []string{"memory 1", "memory 2"},
			}, nil
		},
	}

	result, err := mock.Recall(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(result.Entries))
	}
}

func TestMockMemoryProvider_Memorize(t *testing.T) {
	var storedContent string
	mock := &MockMemoryProvider{
		MemorizeFunc: func(ctx context.Context, content string, opts MemorizeOptions) error {
			storedContent = content
			return nil
		},
	}

	err := mock.Memorize(context.Background(), "test content", MemorizeOptions{LongTerm: true})
	if err != nil {
		t.Fatalf("Memorize failed: %v", err)
	}

	if storedContent != "test content" {
		t.Errorf("Expected 'test content', got '%s'", storedContent)
	}
}

func TestMockMemoryProvider_GetMemoryContext(t *testing.T) {
	mock := &MockMemoryProvider{
		GetContextFunc: func(ctx context.Context) (string, error) {
			return "## Memory\n\nTest memory content", nil
		},
	}

	ctx, err := mock.GetMemoryContext(context.Background())
	if err != nil {
		t.Fatalf("GetMemoryContext failed: %v", err)
	}

	if ctx == "" {
		t.Error("Expected non-empty context")
	}
}

func TestMockMemoryProvider_Close(t *testing.T) {
	closed := false
	mock := &MockMemoryProvider{
		CloseFunc: func() error {
			closed = true
			return nil
		},
	}

	err := mock.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !closed {
		t.Error("Expected Close to be called")
	}
}
