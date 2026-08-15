package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type P7AgentState struct {
	Role      string    `json:"role"`
	UpdatedAt time.Time `json:"updated_at"`
}

type P7AgentManager struct {
	home, path string
	mu         sync.Mutex
	state      P7AgentState
}

func NewP7AgentManager() *P7AgentManager {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("USERPROFILE"), ".cc-connect", "codex-home")
	}
	m := &P7AgentManager{home: home, path: filepath.Join(home, ".codex", "agent-state.json")}
	m.load()
	return m
}
func validP7Role(r string) bool { return r == "explorer" || r == "worker" || r == "reviewer" }
func (m *P7AgentManager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(b, &m.state)
	}
	if !validP7Role(m.state.Role) {
		m.state.Role = "explorer"
	}
}
func (m *P7AgentManager) Role() string { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Role }
func (m *P7AgentManager) SetRole(r string) error {
	r = strings.ToLower(strings.TrimSpace(r))
	if !validP7Role(r) {
		return fmt.Errorf("invalid role %q (use explorer, worker, reviewer)", r)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = P7AgentState{Role: r, UpdatedAt: time.Now()}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m.state, "", "  ")
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
func (m *P7AgentManager) Status(backend string) string {
	return fmt.Sprintf("Agent\nBackend: %s\nRole: %s\nWorkflow: Research → Plan → Execute → Review → Ship\nCodex Home: %s\nWorktree: E:\\CodexTelegram\\.worktrees", backend, m.Role(), m.home)
}

// PromptPreamble returns the selected role instructions for the next session.
// The TOML is intentionally passed through as text so role files remain
// independently editable and isolated under the cc-connect Codex Home.
func (m *P7AgentManager) PromptPreamble() string {
	role := m.Role()
	b, err := os.ReadFile(filepath.Join(m.home, ".codex", "agents", role+".toml"))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("Selected Codex role: %s\n\nRole configuration:\n%s", role, strings.TrimSpace(string(b)))
}
