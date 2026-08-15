package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredMemoryIsolationAndPersistence(t *testing.T) {
	d := t.TempDir()
	s := NewStructuredMemoryStore(d)
	if err := s.Add("telegram:1:user-a", "rule", "用中文回答"); err != nil {
		t.Fatal(err)
	}
	if got := s.Render("telegram:1:user-b"); got != "" {
		t.Fatal("memory leaked across sessions")
	}
	if !strings.Contains(s.Render("telegram:1:user-a"), "用中文回答") {
		t.Fatal("missing rule")
	}
	if _, err := os.Stat(filepath.Join(d, "memory.json")); err != nil {
		t.Fatal(err)
	}
	s2 := NewStructuredMemoryStore(d)
	if !strings.Contains(s2.Render("telegram:1:user-a"), "用中文回答") {
		t.Fatal("not persisted")
	}
}

func TestStructuredMemoryDedupAndSecretFilter(t *testing.T) {
	s := NewStructuredMemoryStore(t.TempDir())
	_ = s.Add("k", "pref", "dark mode")
	_ = s.Add("k", "pref", "dark mode")
	_ = s.Add("k", "pref", "api_key=secret")
	m := s.Get("k")
	if len(m.Preferences) != 1 || m.Preferences[0] != "dark mode" {
		t.Fatalf("unexpected prefs: %#v", m.Preferences)
	}
}
