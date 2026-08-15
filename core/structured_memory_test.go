package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestStructuredMemorySearchReadableOutput(t *testing.T) {
	s := NewStructuredMemoryStore(t.TempDir())
	_ = s.Add("k", "rule", "用中文回答")
	out := s.RenderSearch("k", "中文")
	for _, want := range []string{"记忆检索", "类型: 规则", "置信度:", "状态: 有效", "用中文回答"} {
		if !strings.Contains(out, want) {
			t.Fatalf("search output missing %q: %s", want, out)
		}
	}
}

func TestStructuredMemoryMaintainDeduplicatesAndRebuildsProjection(t *testing.T) {
	s := NewStructuredMemoryStore(t.TempDir())
	_ = s.Add("k", "rule", "保持中文回答")
	s.mu.Lock()
	m := s.data.Chats["k"]
	now := time.Now()
	m.Records = append(m.Records, MemoryRecord{
		ID: "dup", Kind: "rule", Text: "保持中文回答", Confidence: 0.2,
		CreatedAt: now, UpdatedAt: now, Status: "active",
	})
	m.DurableRules = []string{"保持中文回答", "已归档旧规则"}
	s.mu.Unlock()
	if err := s.Maintain(); err != nil {
		t.Fatal(err)
	}
	got := s.Get("k")
	if len(got.DurableRules) != 1 || got.DurableRules[0] != "保持中文回答" {
		t.Fatalf("projection not rebuilt: %#v", got.DurableRules)
	}
	active := 0
	for _, r := range got.Records {
		if r.Kind == "rule" && r.Status == "active" {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("expected one active duplicate winner, got %d", active)
	}
}

func TestStructuredMemoryMaintainCompactsCounters(t *testing.T) {
	s := NewStructuredMemoryStore(t.TempDir())
	for i := 0; i < memoryCompactTurns; i++ {
		_ = s.AddTurn("k", memoryCompactTokens/memoryCompactTurns)
	}
	before := s.Get("k")
	if before.TurnsSinceCompact == 0 || before.TokensSinceCompact == 0 {
		t.Fatal("turn counters not recorded")
	}
	if err := s.Maintain(); err != nil {
		t.Fatal(err)
	}
	after := s.Get("k")
	if after.TurnsSinceCompact != 0 || after.TokensSinceCompact != 0 || after.CompactedAt.IsZero() {
		t.Fatalf("compaction not recorded: %+v", after)
	}
}
