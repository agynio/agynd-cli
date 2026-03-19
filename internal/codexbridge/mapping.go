package codexbridge

import "sync"

type ThreadMapping struct {
	mu              sync.RWMutex
	platformToCodex map[string]string
	codexToPlatform map[string]string
}

func NewThreadMapping() *ThreadMapping {
	return &ThreadMapping{
		platformToCodex: make(map[string]string),
		codexToPlatform: make(map[string]string),
	}
}

func (m *ThreadMapping) Set(platformThreadID, codexThreadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.platformToCodex[platformThreadID] = codexThreadID
	m.codexToPlatform[codexThreadID] = platformThreadID
}

func (m *ThreadMapping) CodexForPlatform(platformThreadID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	codexThreadID, ok := m.platformToCodex[platformThreadID]
	return codexThreadID, ok
}

func (m *ThreadMapping) PlatformForCodex(codexThreadID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	platformThreadID, ok := m.codexToPlatform[codexThreadID]
	return platformThreadID, ok
}
