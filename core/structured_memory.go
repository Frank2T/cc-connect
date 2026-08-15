package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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
	Abstract   string     `json:"abstract,omitempty"`
	Overview   string     `json:"overview,omitempty"`
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

// Records are the single authoritative store. The legacy arrays are only a
// derived projection kept for backward-compatible rendering and migration.
func (m *StructuredMemory) rebuildProjections() {
	if len(m.Records) == 0 {
		return
	}
	m.DurableRules, m.Preferences, m.Decisions, m.SkillIdeas = nil, nil, nil, nil
	seen := map[string]bool{}
	for _, r := range m.Records {
		if r.Status != "active" {
			continue
		}
		k := r.Kind + "\x00" + r.Text
		if seen[k] {
			continue
		}
		seen[k] = true
		switch r.Kind {
		case "rule":
			m.DurableRules = append(m.DurableRules, r.Text)
		case "pref":
			m.Preferences = append(m.Preferences, r.Text)
		case "decision":
			m.Decisions = append(m.Decisions, r.Text)
		case "idea":
			m.SkillIdeas = append(m.SkillIdeas, r.Text)
		}
	}
}

const (
	memoryMaintenanceInterval = 6 * time.Hour
	memoryStaleAfter           = 30 * 24 * time.Hour
)

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
		m.rebuildProjections()
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
	for _, m := range s.data.Chats {
		if m != nil {
			m.rebuildProjections()
		}
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
	if len(cleanItems([]string{text})) == 0 {
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
	// 自动治理：同类且有明显词汇重叠但内容不同的旧记录标记为 conflict，
	// 新记录保留为 active，避免旧规则与新规则同时伪装成无冲突事实。
	now := time.Now()
	for i := range m.Records {
		r := &m.Records[i]
		if r.Kind == kind && r.Status == "active" &&
			!strings.EqualFold(strings.TrimSpace(r.Text), text) {
			hit := 0
			want := memoryTokens(text)
			for t := range memoryTokens(r.Text) {
				if want[t] {
					hit++
				}
			}
			if hit >= 2 {
				r.Status = "conflict"
				r.UpdatedAt = now
			}
		}
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
		m.Metadata[kind+"\x00"+text] = MemoryMeta{Source: "user_or_agent", Confidence: 0.8, UpdatedAt: now}
		m.Records = append(m.Records, MemoryRecord{ID: kind + "-" + strconv.FormatInt(now.UnixNano(), 10), Kind: kind, Text: text, Abstract: text, Overview: "用户明确确认的" + kind + "记忆", Scope: "session", Source: "user_or_agent", Confidence: 0.8, CreatedAt: now, UpdatedAt: now, Status: "active"})
		if len(m.Records) > 120 {
			m.Records = m.Records[len(m.Records)-120:]
		}
	}
	m.UpdatedAt = time.Now()
	return s.saveLocked()
}

func memoryTokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("，。！？；：,.!?;:", r) }) {
		if len([]rune(part)) >= 2 {
			out[part] = true
		}
	}
	return out
}

func memorySimilarity(a, b string) float64 {
	ta, tb := memoryTokens(a), memoryTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	hit := 0
	for t := range ta {
		if tb[t] {
			hit++
		}
	}
	union := len(ta) + len(tb) - hit
	if union == 0 {
		return 0
	}
	return float64(hit) / float64(union)
}

// Conflicts returns active records of the same kind with substantial token overlap
// but different text, allowing callers to ask for confirmation instead of silently overwriting.
func (s *StructuredMemoryStore) Conflicts(key, kind, text string) []MemoryRecord {
	m := s.Get(key)
	want := memoryTokens(text)
	var out []MemoryRecord
	for _, r := range m.Records {
		if r.Kind != kind || r.Status != "active" || strings.EqualFold(strings.TrimSpace(r.Text), strings.TrimSpace(text)) {
			continue
		}
		hit := 0
		for t := range memoryTokens(r.Text) {
			if want[t] {
				hit++
			}
		}
		if hit >= 1 {
			out = append(out, r)
		}
	}
	return out
}

// Promote changes a candidate record into durable active memory after confirmation.
func (s *StructuredMemoryStore) Promote(key, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		return nil
	}
	for i := range m.Records {
		if m.Records[i].ID == id {
			m.Records[i].Status = "active"
			m.Records[i].Confidence = 1
			m.Records[i].UpdatedAt = time.Now()
			return s.saveLocked()
		}
	}
	return nil
}

// PurgeExpired archives expired records without deleting them.
func (s *StructuredMemoryStore) PurgeExpired(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.data.Chats[key]
	if m == nil {
		return nil
	}
	now := time.Now()
	changed := false
	for i := range m.Records {
		if m.Records[i].ExpiresAt != nil && m.Records[i].ExpiresAt.Before(now) && m.Records[i].Status == "active" {
			m.Records[i].Status = "archived"
			changed = true
		}
	}
	if changed {
		m.UpdatedAt = now
		return s.saveLocked()
	}
	return nil
}

// Maintain archives expired records and stale superseded/conflict records.
// Records are retained for auditability; only their status changes.
func (s *StructuredMemoryStore) Maintain() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	changed := false
	for _, m := range s.data.Chats {
		if m == nil {
			continue
		}
		chatChanged := false
		for i := range m.Records {
			r := &m.Records[i]
			if r.Status == "active" && r.ExpiresAt != nil && r.ExpiresAt.Before(now) {
				r.Status = "archived"
				r.UpdatedAt = now
				changed = true
				chatChanged = true
				continue
			}
			if (r.Status == "conflict" || r.Status == "superseded") &&
				!r.UpdatedAt.IsZero() && now.Sub(r.UpdatedAt) >= memoryStaleAfter {
				r.Status = "archived"
				r.UpdatedAt = now
				changed = true
				chatChanged = true
			}
		}
		if chatChanged {
			m.UpdatedAt = now
		}
	}
	if !changed {
		return nil
	}
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
	return s.RenderRelevant(key, "")
}

func (s *StructuredMemoryStore) RenderRelevant(key, query string) string {
	m := s.Get(key)
	activeText := map[string]bool{}
	for _, r := range m.Records {
		if r.Status == "active" {
			activeText[r.Kind+"\x00"+r.Text] = true
		}
	}
	var b strings.Builder
	if m.Summary != "" {
		b.WriteString("summary: " + m.Summary + "\n")
	}
	renderItems := func(kind, title string, items []string) {
		type candidate struct {
			text  string
			score float64
		}
		active := make([]candidate, 0, len(items))
		now := time.Now()
		qtokens := memoryTokens(query)
		for _, item := range items {
			if len(m.Records) > 0 && !activeText[kind+"\x00"+item] {
				continue
			}
			if meta, ok := m.Metadata[kind+"\x00"+item]; ok && meta.ExpiresAt != nil && meta.ExpiresAt.Before(now) {
				continue
			}
			if len(qtokens) > 0 {
				if memorySimilarity(item, query) == 0 {
					continue
				}
			}
			active = append(active, candidate{text: item, score: memorySimilarity(item, query)})
		}
		if len(active) > 0 {
			sort.SliceStable(active, func(i, j int) bool { return active[i].score > active[j].score })
			lines := make([]string, 0, len(active))
			for _, c := range active {
				lines = append(lines, c.text)
			}
			b.WriteString(title + ":\n- " + strings.Join(lines, "\n- ") + "\n")
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
