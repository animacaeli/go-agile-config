package agileconfig

import (
	"testing"
)

func TestConfigStore_GetAfterReload(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"db.host": "localhost"})

	val, ok := s.get("db.host")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "localhost" {
		t.Fatalf("expected localhost, got %s", val)
	}
}

func TestConfigStore_GetMissing(t *testing.T) {
	s := newConfigStore()
	_, ok := s.get("missing")
	if ok {
		t.Fatal("expected key to not exist")
	}
}

func TestConfigStore_GetByGroup(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"database:host": "127.0.0.1"})

	val, ok := s.getByGroup("database", "host")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %s", val)
	}
}

func TestConfigStore_GetByGroup_Missing(t *testing.T) {
	s := newConfigStore()
	_, ok := s.getByGroup("database", "port")
	if ok {
		t.Fatal("expected key to not exist")
	}
}

func TestConfigStore_GetAll(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"a": "1", "b": "2"})

	all := s.getAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 items, got %d", len(all))
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("unexpected values: %v", all)
	}
}

func TestConfigStore_GetAll_ReturnsCopy(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"a": "1"})

	all := s.getAll()
	all["a"] = "mutated"

	val, _ := s.get("a")
	if val != "1" {
		t.Fatal("GetAll should return a copy, not a reference to internal map")
	}
}

func TestConfigStore_Reload_DetectsChanges(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"a": "1", "b": "2"})

	newData := map[string]string{
		"a": "1",
		"b": "3",
		"c": "4",
	}

	changed := s.reload(newData)

	if len(changed) != 2 {
		t.Fatalf("expected 2 changed keys, got %d: %v", len(changed), changed)
	}

	hasB, hasC := false, false
	for _, k := range changed {
		if k == "b" {
			hasB = true
		}
		if k == "c" {
			hasC = true
		}
	}
	if !hasB || !hasC {
		t.Fatalf("expected changed keys to include 'b' and 'c', got %v", changed)
	}
}

func TestConfigStore_Reload_NoChanges(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"a": "1"})

	newData := map[string]string{"a": "1"}
	changed := s.reload(newData)

	if len(changed) != 0 {
		t.Fatalf("expected 0 changed keys, got %d: %v", len(changed), changed)
	}
}

func TestConfigStore_Reload_ReplacesAll(t *testing.T) {
	s := newConfigStore()
	s.reload(map[string]string{"a": "1", "b": "2"})

	newData := map[string]string{"c": "3"}
	s.reload(newData)

	_, okA := s.get("a")
	_, okB := s.get("b")
	_, okC := s.get("c")

	if okA || okB {
		t.Fatal("old keys should be removed after reload")
	}
	if !okC {
		t.Fatal("new key should exist after reload")
	}
}
