package codexbridge

import "sync"

type ThreadMapping struct {
	mu              sync.RWMutex
	platformToCodex map[string]ThreadMappingRecord
	codexToPlatform map[string]string
}

func NewThreadMapping() *ThreadMapping {
	return &ThreadMapping{
		platformToCodex: make(map[string]ThreadMappingRecord),
		codexToPlatform: make(map[string]string),
	}
}

func (m *ThreadMapping) SetRecord(record ThreadMappingRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.platformToCodex[record.PlatformThreadID]; ok {
		if existing.CodexThreadID != record.CodexThreadID {
			delete(m.codexToPlatform, existing.CodexThreadID)
		}
	}
	m.platformToCodex[record.PlatformThreadID] = record
	m.codexToPlatform[record.CodexThreadID] = record.PlatformThreadID
}

func (m *ThreadMapping) RecordForPlatform(platformThreadID string) (ThreadMappingRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.platformToCodex[platformThreadID]
	return record, ok
}

func (m *ThreadMapping) CodexForPlatform(platformThreadID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.platformToCodex[platformThreadID]
	return record.CodexThreadID, ok
}

func (m *ThreadMapping) PlatformForCodex(codexThreadID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	platformThreadID, ok := m.codexToPlatform[codexThreadID]
	return platformThreadID, ok
}
