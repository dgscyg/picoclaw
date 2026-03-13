package cron

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("cron store has permission %04o, want 0600", perm)
	}
}

func TestCheckJobs_DoesNotRewriteStoreWhenNoJobsDue(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)
	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	infoBefore, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat before checkJobs failed: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	cs.running = true
	cs.checkJobs()
	cs.running = false

	infoAfter, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat after checkJobs failed: %v", err)
	}

	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("store modtime changed without due jobs: before=%v after=%v", infoBefore.ModTime(), infoAfter.ModTime())
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
