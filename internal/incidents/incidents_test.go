package incidents

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if store.Count() != 0 {
		t.Errorf("new store should have 0 incidents, got %d", store.Count())
	}
}

func TestStore_Add(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	inc, err := store.Add("src/auth/jwt.go", "high", "JWT expiry bug", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if inc.ID == "" {
		t.Error("incident should have an ID")
	}
	if inc.File != "src/auth/jwt.go" {
		t.Errorf("file should be src/auth/jwt.go, got %q", inc.File)
	}
	if store.Count() != 1 {
		t.Errorf("store should have 1 incident, got %d", store.Count())
	}
}

func TestStore_Add_InvalidSeverity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	store, _ := NewStore(path)

	_, err := store.Add("file.go", "extreme", "bad severity", time.Now())
	if err == nil {
		t.Error("should reject invalid severity")
	}
}

func TestStore_ForFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	store, _ := NewStore(path)

	now := time.Now()
	_, _ = store.Add("src/auth/jwt.go", "high", "Bug 1", now)
	_, _ = store.Add("src/api/handler.go", "medium", "Bug 2", now)
	_, _ = store.Add("src/auth/jwt.go", "critical", "Bug 3", now)

	jwtIncidents := store.ForFile("src/auth/jwt.go")
	if len(jwtIncidents) != 2 {
		t.Errorf("jwt.go should have 2 incidents, got %d", len(jwtIncidents))
	}

	handlerIncidents := store.ForFile("src/api/handler.go")
	if len(handlerIncidents) != 1 {
		t.Errorf("handler.go should have 1 incident, got %d", len(handlerIncidents))
	}

	noIncidents := store.ForFile("nonexistent.go")
	if len(noIncidents) != 0 {
		t.Errorf("nonexistent should have 0 incidents, got %d", len(noIncidents))
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	// Create and add
	store1, _ := NewStore(path)
	_, _ = store1.Add("file.go", "high", "Bug", time.Now())

	// Reload from same file
	store2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if store2.Count() != 1 {
		t.Errorf("reloaded store should have 1 incident, got %d", store2.Count())
	}

	all := store2.All()
	if all[0].Description != "Bug" {
		t.Errorf("description should persist, got %q", all[0].Description)
	}
}

func TestStore_MultipleAdds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidents.json")

	store, _ := NewStore(path)

	for i := 0; i < 5; i++ {
		_, err := store.Add("file.go", "medium", "Bug", time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}

	if store.Count() != 5 {
		t.Errorf("should have 5 incidents, got %d", store.Count())
	}
}
