package codexbridge

import "sync"

// ThreadMapping records which codex thread an agent instance talks in. It is
// keyed by instance rather than by platform thread: an instance serves many
// threads and they share one conversation.
type ThreadMapping struct {
	mu              sync.RWMutex
	instanceToCodex map[string]ThreadMappingRecord
	codexToInstance map[string]string
}

func NewThreadMapping() *ThreadMapping {
	return &ThreadMapping{
		instanceToCodex: make(map[string]ThreadMappingRecord),
		codexToInstance: make(map[string]string),
	}
}

func (m *ThreadMapping) SetRecord(record ThreadMappingRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.instanceToCodex[record.InstanceID]; ok {
		if existing.CodexThreadID != record.CodexThreadID {
			delete(m.codexToInstance, existing.CodexThreadID)
		}
	}
	m.instanceToCodex[record.InstanceID] = record
	m.codexToInstance[record.CodexThreadID] = record.InstanceID
}

func (m *ThreadMapping) RecordForInstance(instanceID string) (ThreadMappingRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.instanceToCodex[instanceID]
	return record, ok
}

func (m *ThreadMapping) CodexForInstance(instanceID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.instanceToCodex[instanceID]
	return record.CodexThreadID, ok
}

func (m *ThreadMapping) InstanceForCodex(codexThreadID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instanceID, ok := m.codexToInstance[codexThreadID]
	return instanceID, ok
}
