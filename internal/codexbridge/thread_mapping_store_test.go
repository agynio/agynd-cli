package codexbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeMappingFile(t *testing.T, path string, record ThreadMappingRecord) {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal mapping: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), threadMappingDirMode); err != nil {
		t.Fatalf("mkdir mapping dir: %v", err)
	}
	if err := os.WriteFile(path, payload, threadMappingFileMode); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
}

func TestThreadMappingStoreSaveLoad(t *testing.T) {
	homeDir := t.TempDir()
	store := NewThreadMappingStore(homeDir)
	record := ThreadMappingRecord{
		PlatformThreadID: "platform-1",
		CodexThreadID:    "codex-1",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000001234,
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("expected save to succeed, got %v", err)
	}
	got, ok, err := store.Load("platform-1")
	if err != nil {
		t.Fatalf("expected load to succeed, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to be found")
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("unexpected record: %#v", got)
	}
	path, err := store.mappingPath("platform-1")
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
		PlatformThreadID: "platform-atomic",
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
		PlatformThreadID: "platform-atomic",
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

func TestThreadMappingStoreLoadLegacyMigrates(t *testing.T) {
	homeDir := t.TempDir()
	store := NewThreadMappingStore(homeDir)
	legacyRecord := ThreadMappingRecord{
		PlatformThreadID: "platform-legacy",
		CodexThreadID:    "codex-legacy",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000100,
	}
	legacyPath, err := store.legacyMappingPath(legacyRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected legacy path, got %v", err)
	}
	writeMappingFile(t, legacyPath, legacyRecord)
	got, ok, err := store.Load(legacyRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected load to succeed, got %v", err)
	}
	if !ok {
		t.Fatal("expected legacy mapping to load")
	}
	if !reflect.DeepEqual(got, legacyRecord) {
		t.Fatalf("unexpected record: %#v", got)
	}
	newPath, err := store.mappingPath(legacyRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected new path, got %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected migrated mapping to exist, got %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected legacy mapping to remain, got %v", err)
	}
}

func TestThreadMappingStoreLoadPrefersNew(t *testing.T) {
	homeDir := t.TempDir()
	store := NewThreadMappingStore(homeDir)
	newRecord := ThreadMappingRecord{
		PlatformThreadID: "platform-dual",
		CodexThreadID:    "codex-new",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000100,
	}
	legacyRecord := ThreadMappingRecord{
		PlatformThreadID: "platform-dual",
		CodexThreadID:    "codex-legacy",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000200,
	}
	newPath, err := store.mappingPath(newRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected new path, got %v", err)
	}
	legacyPath, err := store.legacyMappingPath(newRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected legacy path, got %v", err)
	}
	writeMappingFile(t, newPath, newRecord)
	writeMappingFile(t, legacyPath, legacyRecord)
	var buffer bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(originalOutput) })
	got, ok, err := store.Load(newRecord.PlatformThreadID)
	if err != nil {
		t.Fatalf("expected load to succeed, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to load")
	}
	if !reflect.DeepEqual(got, newRecord) {
		t.Fatalf("expected new record to win, got %#v", got)
	}
	if !bytes.Contains(buffer.Bytes(), []byte("legacy codex thread")) {
		t.Fatalf("expected warning about legacy mismatch, got %q", buffer.String())
	}
}
