// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// FileMemoryStore manages persistent memory using workspace files.
// - Long-term memory: memory/MEMORY.md
// - Daily notes: memory/YYYYMM/YYYYMMDD.md
type FileMemoryStore struct {
	workspace  string
	memoryDir  string
	memoryFile string
}

// NewFileMemoryStore creates a new FileMemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewFileMemoryStore(workspace string) *FileMemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	_ = os.MkdirAll(memoryDir, 0o755)

	return &FileMemoryStore{
		workspace:  workspace,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getTodayFile returns the path to today's daily note file (memory/YYYYMM/YYYYMMDD.md).
func (ms *FileMemoryStore) getTodayFile() string {
	today := time.Now().Format("20060102") // YYYYMMDD
	monthDir := today[:6]                  // YYYYMM
	filePath := filepath.Join(ms.memoryDir, monthDir, today+".md")
	return filePath
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *FileMemoryStore) ReadLongTerm(ctx context.Context) string {
	_ = ctx
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *FileMemoryStore) WriteLongTerm(ctx context.Context, content string) error {
	_ = ctx
	// Use unified atomic write utility with explicit sync for flash storage reliability.
	// Using 0o600 (owner read/write only) for secure default permissions.
	return fileutil.WriteFileAtomic(ms.memoryFile, []byte(content), 0o600)
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist.
func (ms *FileMemoryStore) ReadToday(ctx context.Context) string {
	_ = ctx
	todayFile := ms.getTodayFile()
	if data, err := os.ReadFile(todayFile); err == nil {
		return string(data)
	}
	return ""
}

// AppendToday appends content to today's daily note.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *FileMemoryStore) AppendToday(ctx context.Context, content string) error {
	_ = ctx
	todayFile := ms.getTodayFile()

	// Ensure month directory exists
	monthDir := filepath.Dir(todayFile)
	if err := os.MkdirAll(monthDir, 0o755); err != nil {
		return err
	}

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		// Append to existing content
		newContent = existingContent + "\n" + content
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	return fileutil.WriteFileAtomic(todayFile, []byte(newContent), 0o600)
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *FileMemoryStore) GetRecentDailyNotes(ctx context.Context, days int) string {
	_ = ctx
	var sb strings.Builder
	first := true

	for i := range days {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("20060102") // YYYYMMDD
		monthDir := dateStr[:6]            // YYYYMM
		filePath := filepath.Join(ms.memoryDir, monthDir, dateStr+".md")

		if data, err := os.ReadFile(filePath); err == nil {
			if !first {
				sb.WriteString("\n\n---\n\n")
			}
			sb.Write(data)
			first = false
		}
	}

	return sb.String()
}

var _ tools.MemoryAccess = (*FileMemoryStore)(nil)

// Recall performs a simple substring search over long-term memory and recent daily notes.
func (ms *FileMemoryStore) Recall(ctx context.Context, query string, limit int) (*tools.MemoryQueryResult, error) {
	if limit <= 0 {
		limit = 5
	}

	query = strings.TrimSpace(strings.ToLower(query))
	candidates := []string{}

	longTerm := strings.TrimSpace(ms.ReadLongTerm(ctx))
	if longTerm != "" {
		candidates = append(candidates, longTerm)
	}

	recentNotes := strings.TrimSpace(ms.GetRecentDailyNotes(ctx, 7))
	if recentNotes != "" {
		candidates = append(candidates, strings.Split(recentNotes, "\n\n---\n\n")...)
	}

	entries := make([]string, 0, limit)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(candidate), query) {
			entries = append(entries, candidate)
			if len(entries) >= limit {
				break
			}
		}
	}

	return &tools.MemoryQueryResult{Entries: entries}, nil
}

// Memorize stores content in long-term memory or today's daily note.
func (ms *FileMemoryStore) Memorize(ctx context.Context, content string, opts tools.MemoryWriteOptions) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	if opts.LongTerm {
		return ms.WriteLongTerm(ctx, content)
	}
	return ms.AppendToday(ctx, content)
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory and recent daily notes.
func (ms *FileMemoryStore) GetMemoryContext(ctx context.Context) (string, error) {
	longTerm := ms.ReadLongTerm(ctx)
	recentNotes := ms.GetRecentDailyNotes(ctx, 3)

	if longTerm == "" && recentNotes == "" {
		return "", nil
	}

	var sb strings.Builder

	if longTerm != "" {
		sb.WriteString("## Long-term Memory\n\n")
		sb.WriteString(longTerm)
	}

	if recentNotes != "" {
		if longTerm != "" {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString("## Recent Daily Notes\n\n")
		sb.WriteString(recentNotes)
	}

	return sb.String(), nil
}

// Close releases file memory store resources.
func (ms *FileMemoryStore) Close() error {
	return nil
}
