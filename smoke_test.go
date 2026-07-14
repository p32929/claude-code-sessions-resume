package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndParseRealData(t *testing.T) {
	projs, err := ListProjects()
	if err != nil {
		t.Skipf("no projects dir: %v", err)
	}
	if len(projs) == 0 {
		t.Skip("no projects found")
	}
	t.Logf("found %d projects; top: %s (%d sessions)", len(projs), projs[0].RealPath, projs[0].NumSess)

	// Load sessions for the first project.
	sess, err := LoadSessions(projs[0].EncodedDir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	t.Logf("loaded %d sessions", len(sess))
	if len(sess) == 0 {
		return
	}
	s := sess[0]
	t.Logf("session %s title=%q msgs=%d cwd=%s resume=%q",
		short(s.ID), s.Title, s.MsgCount, s.Cwd, s.ResumeCommand(ResumeModes[0]))
	for _, mode := range ResumeModes {
		t.Logf("mode %-18s -> %q", mode.Name, s.ResumeCommand(mode))
	}
	if s.ID == "" {
		t.Error("empty session id")
	}

	// Parse one full conversation.
	turns, err := LoadConversation(s.FilePath)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	t.Logf("conversation has %d turns", len(turns))
	out := renderConversation(turns, 80)
	if len(out) == 0 {
		t.Error("empty render")
	}
}

func TestEncodePath(t *testing.T) {
	// Verify encoding matches a real dir name if present.
	home, _ := os.UserHomeDir()
	got := encodePath(filepath.Join(home, "Documents", "Fayaz", "codes", "go"))
	t.Logf("encoded: %s", got)
	if got == "" {
		t.Fatal("empty encode")
	}
}

func TestResumeModePersistence(t *testing.T) {
	path, err := configPath()
	if err != nil {
		t.Skip("no config dir")
	}
	// Back up any existing config so we don't clobber the user's real one.
	orig, hadOrig := os.ReadFile(path)
	t.Cleanup(func() {
		if hadOrig == nil {
			os.WriteFile(path, orig, 0o644)
		} else {
			os.Remove(path)
		}
	})

	// Save the last mode (bypass) and read it back.
	want := len(ResumeModes) - 1
	saveResumeModeIndex(want)
	got := loadResumeModeIndex()
	if got != want {
		t.Fatalf("round-trip failed: saved %d (%s), loaded %d (%s)",
			want, ResumeModes[want].Name, got, ResumeModes[got].Name)
	}
	t.Logf("persisted mode: %s", ResumeModes[got].Name)
}
