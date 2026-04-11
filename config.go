package agileconfig

import "sync"

// configStore holds configuration key-value pairs in a thread-safe map.
// Keys are stored as "group:key" when a group exists, or just "key" when no group.
type configStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newConfigStore() *configStore {
	return &configStore{
		data: make(map[string]string),
	}
}

func storeKey(group, key string) string {
	if group == "" {
		return key
	}
	return group + ":" + key
}

func (s *configStore) get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

func (s *configStore) getByGroup(group, key string) (string, bool) {
	return s.get(storeKey(group, key))
}

func (s *configStore) getAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.data))
	for k, v := range s.data {
		cp[k] = v
	}
	return cp
}

// reload replaces all stored data with newData and returns the list of keys
// whose values changed (added, removed, or modified).
func (s *configStore) reload(newData map[string]string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var changed []string
	for k, v := range newData {
		if old, ok := s.data[k]; !ok || old != v {
			changed = append(changed, k)
		}
	}
	for k := range s.data {
		if _, ok := newData[k]; !ok {
			changed = append(changed, k)
		}
	}

	s.data = newData
	return changed
}
