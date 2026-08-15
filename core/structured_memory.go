package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StructuredMemory struct {
	Summary            string                `json:"summary,omitempty"`
	DurableRules       []string              `json:"durable_rules,omitempty"`
	Preferences        []string              `json:"preferences,omitempty"`
	Decisions          []string              `json:"decisions,omitempty"`
	SkillIdeas         []string              `json:"skill_ideas,omitempty"`
	UpdatedAt          time.Time             `json:"updated_at"`
	CompactedAt        time.Time             `json:"compacted_at"`
	TurnsSinceCompact  int                   `json:"turns_since_compact"`
	TokensSinceCompact int                   `json:"tokens_since_compact"`
	Metadata           map[string]MemoryMeta `json:"metadata,omitempty"`
	Records            []MemoryRecord        `json:"records,omitempty"`
}
type MemoryRecord struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Text       string     `json:"text"`
	Scope      string     `json:"scope,omitempty"`
	Source     string     `json:"source,omitempty"`
	Confidence float64    `json:"confidence"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Status     string     `json:"status"`
	Tags       []string   `json:"tags,omitempty"`
}
type MemoryMeta struct {
	Source     string     `json:"source,omitempty"`
	Confidence float64    `json:"confidence,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}
type StructuredMemoryFile struct {
	Chats map[string]*StructuredMemory `json:"chats"`
}

type StructuredMemoryStore struct {
	path string
	mu   sync.Mutex
	data StructuredMemoryFile
}

func NewStructuredMemoryStore(dir string) *StructuredMemoryStore {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	s := &StructuredMemoryStore{path: filepath.Join(dir, "memory.json"), data: StructuredMemoryFile{Chats: map[string]*StructuredMemory{}}}
	_ = s.load()
	return s
}
func (s *StructuredMemoryStore) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var d StructuredMemoryFile
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	if d.Chats == nil {
		d.Chats = map[string]*StructuredMemory{}
	}
	for _, m := range d.Chats {
		if m.Metadata == nil {
			m.Metadata = map[string]MemoryMeta{}
		}
		m.migrateRecords()
	}
	s.data = d
	return nil
}
func (m *StructuredMemory) migrateRecords() {
	if len(m.Records) > 0 {
		return
	}
	now := time.Now()
	add := func(kind string, items []string) {
		for i, text := range items {
			m.Records = append(m.Records, MemoryRecord{ID: kind + "-" + strconv.Itoa(i+1), Kind: kind, Text: text, Scope: "session", Source: "legacy", Confidence: 0.7, CreatedAt: now, UpdatedAt: now, Status: "active"})
		}
	}
	add("rule", m.DurableRules)
	add("pref", m.Preferences)
	add("decision", m.Decisions)
	add("idea", m.SkillIdeas)
}
func cleanItems(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		norm := strings.ToLower(strings.Join(strings.Fields(v), " "))
		if v == "" || len([]rune(v)) > 500 || seen[norm] || strings.Contains(strings.ToLower(v), "api_key") || strings.Contains(strings.ToLower(v), "token=") || strings.Contains(strings.ToLower(v), "authorization:") {
			continue
		}
		seen[norm] = true
		out = append(out, v)
	}
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}
func (s *StructuredMemoryStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.data, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
func (s *StructuredMemoryStore) Get(key string) StructuredMemory {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		return StructuredMemory{}
	}
	return *m
}
func (s *StructuredMemoryStore) Add(key, kind, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		m = &StructuredMemory{Metadata: map[string]MemoryMeta{}}
		s.data.Chats[key] = m
	}
	if m.Metadata == nil {
		m.Metadata = map[string]MemoryMeta{}
	}
	switch kind {
	case "rule":
		m.DurableRules = cleanItems(append(m.DurableRules, text))
	case "pref":
		m.Preferences = cleanItems(append(m.Preferences, text))
	case "idea":
		m.SkillIdeas = cleanItems(append(m.SkillIdeas, text))
	case "decision":
		m.Decisions = cleanItems(append(m.Decisions, text))
	case "summary":
		m.Summary = text
	}
	if kind != "summary" {
		m.Metadata[kind+"\x00"+text] = MemoryMeta{Source: "user_or_agent", Confidence: 0.8, UpdatedAt: time.Now()}
		now := time.Now()
		m.Records = append(m.Records, MemoryRecord{ID: kind + "-" + strconv.FormatInt(now.UnixNano(), 10), Kind: kind, Text: text, Scope: "session", Source: "user_or_agent", Confidence: 0.8, CreatedAt: now, UpdatedAt: now, Status: "active"})
		if len(m.Records) > 120 {
			m.Records = m.Records[len(m.Records)-120:]
		}
	}
	m.UpdatedAt = time.Now()
	return s.saveLocked()
}
func (s *StructuredMemoryStore) Forget(key, kind string, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		return nil
	}
	var p *[]string
	switch kind {
	case "rule":
		p = &m.DurableRules
	case "pref":
		p = &m.Preferences
	case "idea":
		p = &m.SkillIdeas
	case "decision":
		p = &m.Decisions
	}
	if p != nil && n > 0 && n <= len(*p) {
		if m.Metadata != nil {
			delete(m.Metadata, kind+"\x00"+(*p)[n-1])
		}
		*p = append((*p)[:n-1], (*p)[n:]...)
	}
	m.UpdatedAt = time.Now()
	return s.saveLocked()
}
func (s *StructuredMemoryStore) AddTurn(key string, tokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		m = &StructuredMemory{Metadata: map[string]MemoryMeta{}}
		s.data.Chats[key] = m
	}
	m.TurnsSinceCompact++
	m.TokensSinceCompact += tokens
	m.UpdatedAt = time.Now()
	return s.saveLocked()
}
func (s *StructuredMemoryStore) Render(key string) string {
	m := s.Get(key)
	var b strings.Builder
	if m.Summary != "" {
		b.WriteString("summary: " + m.Summary + "\n")
	}
	renderItems := func(kind, title string, items []string) {
		active := make([]string, 0, len(items))
		now := time.Now()
		for _, item := range items {
			if meta, ok := m.Metadata[kind+"\x00"+item]; ok && meta.ExpiresAt != nil && meta.ExpiresAt.Before(now) {
				continue
			}
			active = append(active, item)
		}
		if len(active) > 0 {
			b.WriteString(title + ":\n- " + strings.Join(active, "\n- ") + "\n")
		}
	}
	renderItems("rule", "durable rules", m.DurableRules)
	renderItems("pref", "preferences", m.Preferences)
	renderItems("decision", "decisions", m.Decisions)
	renderItems("idea", "skill ideas", m.SkillIdeas)
	if b.Len() > 3500 {
		return b.String()[:3500]
	}
	return b.String()
}
