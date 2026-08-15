package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// atomicWriteFile writes through a sibling temporary file and atomically replaces
// the destination. Existing content is never truncated before the replacement.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cc-connect-memory-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(perm); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// backupFile creates a timestamped sibling backup before a managed rewrite.
func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	backup := fmt.Sprintf("%s.bak-%s", path, stamp)
	return atomicWriteFile(backup, data, 0o644)
}

// maintainWorkspaceMemoryFiles backs up the two workspace memory files. AGENTS.md
// is deliberately never rewritten automatically; it contains stable instructions.
func maintainWorkspaceMemoryFiles(workspaceDir string) error {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if workspaceDir == "" {
		return nil
	}
	var firstErr error
	for _, name := range []string{"MEMORY.md", "AGENTS.md"} {
		if err := backupFile(filepath.Join(workspaceDir, name)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// syncStructuredMemoryBackup mirrors the structured store into the private
// backup repository using the same atomic replacement semantics.
func syncStructuredMemoryBackup(storePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	target := filepath.Join(home, ".codex", "cc-connect", "sessions", filepath.Base(storePath))
	data, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := backupFile(target); err != nil {
		return err
	}
	return atomicWriteFile(target, data, 0o644)
}
