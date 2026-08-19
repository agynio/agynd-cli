package codexbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestThreadMappingStoreSaveLoad(t *testing.T) {
	homeDir := t.TempDir()
	store := NewThreadMappingStore(homeDir)
	record := ThreadMappingRecord{
		InstanceID:       "instance-1",
		CodexThreadID:    "codex-1",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000001234,
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("expected save to succeed, got %v", err)
	}
	got, ok, err := store.Load("instance-1")
	if err != nil {
		t.Fatalf("expected load to succeed, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to be found")
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("unexpected record: %#v", got)
	}
	path, err := store.mappingPath("instance-1")
	if err != nil {
		t.Fatalf("expected mapping path, got %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected mapping file to exist, got %v", err)
	}
	if fileInfo.Mode().Perm() != threadMappingFileMode {
		t.Fatalf("expected file mode %o, got %o", threadMappingFileMode, fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("expected mapping dir to exist, got %v", err)
	}
	if dirInfo.Mode().Perm() != threadMappingDirMode {
		t.Fatalf("expected dir mode %o, got %o", threadMappingDirMode, dirInfo.Mode().Perm())
	}
}

func TestThreadMappingStoreLoadMissing(t *testing.T) {
	store := NewThreadMappingStore(t.TempDir())
	_, ok, err := store.Load("platform-missing")
	if err != nil {
		t.Fatalf("expected missing load to succeed, got %v", err)
	}
	if ok {
		t.Fatal("expected missing mapping to return ok=false")
	}
}

func TestThreadMappingStoreSaveAtomic(t *testing.T) {
	homeDir := t.TempDir()
	store := NewThreadMappingStore(homeDir)
	original := ThreadMappingRecord{
		InstanceID:       "platform-atomic",
		CodexThreadID:    "codex-old",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000100,
	}
	if err := store.Save(original); err != nil {
		t.Fatalf("expected initial save to succeed, got %v", err)
	}
	store.rename = func(oldpath, newpath string) error {
		return fmt.Errorf("rename failed")
	}
	updated := ThreadMappingRecord{
		InstanceID:       "platform-atomic",
		CodexThreadID:    "codex-new",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000200,
	}
	if err := store.Save(updated); err == nil {
		t.Fatal("expected save to fail when rename fails")
	}
	got, ok, err := store.Load("platform-atomic")
	if err != nil {
		t.Fatalf("expected load after failed save to succeed, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to still exist after failed save")
	}
	if got.CodexThreadID != original.CodexThreadID {
		t.Fatalf("expected original mapping to remain, got %q", got.CodexThreadID)
	}
	path, err := store.mappingPath("platform-atomic")
	if err != nil {
		t.Fatalf("expected mapping path, got %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("expected mapping dir to be readable, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected no temp files, found %d entries", len(entries))
	}
}
