package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	out, anchors := renderConversation(turns, 80)
	if len(out) == 0 {
		t.Error("empty render")
	}
	t.Logf("render has %d user-prompt anchors", len(anchors))
	// Turns must be ordered chronologically.
	for i := 1; i < len(turns); i++ {
		if turns[i].Time.Before(turns[i-1].Time) {
			t.Errorf("turns out of order at %d: %v before %v", i, turns[i].Time, turns[i-1].Time)
		}
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

func TestConfigPersistence(t *testing.T) {
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

	// Save the last resume mode (bypass) and read it back.
	wantMode := len(ResumeModes) - 1
	saveResumeModeIndex(wantMode)
	// Saving a sort mode must NOT clobber the resume mode (read-modify-write).
	wantSort := len(sortModes) - 1
	saveSortMode(wantSort)

	if got := loadResumeModeIndex(); got != wantMode {
		t.Fatalf("resume mode round-trip failed: saved %d (%s), loaded %d (%s)",
			wantMode, ResumeModes[wantMode].Name, got, ResumeModes[got].Name)
	}
	if got := loadSortMode(); got != wantSort {
		t.Fatalf("sort mode round-trip failed: saved %d (%s), loaded %d (%s)",
			wantSort, sortModes[wantSort].Name, got, sortModes[got].Name)
	}
	t.Logf("persisted mode=%s sort=%s (independently)", ResumeModes[wantMode].Name, sortModes[wantSort].Name)
}

func TestSortSessions(t *testing.T) {
	ss := []Session{
		{ID: "a", Title: "Zebra", MsgCount: 5, SizeBytes: 100, End: time.Unix(300, 0)},
		{ID: "b", Title: "apple", MsgCount: 50, SizeBytes: 10, End: time.Unix(100, 0)},
		{ID: "c", Title: "Mango", MsgCount: 1, SizeBytes: 999, End: time.Unix(200, 0)},
	}
	check := func(mode int, wantIDs string) {
		cp := append([]Session(nil), ss...)
		sortSessions(cp, mode)
		var got string
		for _, s := range cp {
			got += s.ID
		}
		if got != wantIDs {
			t.Errorf("sort %q: got %q want %q", sortModes[mode].Name, got, wantIDs)
		}
	}
	check(0, "acb") // recent: End desc -> 300,200,100
	check(1, "bac") // messages: 50,5,1
	check(2, "cab") // size: 999,100,10
	check(3, "bca") // title: apple, Mango, Zebra (case-insensitive)
}

func TestShellQuoteAndResumeCd(t *testing.T) {
	if got := shellQuote("/Users/me/proj"); got != "/Users/me/proj" {
		t.Errorf("plain path should not be quoted: %q", got)
	}
	if got := shellQuote("/Users/me/my proj"); got != "'/Users/me/my proj'" {
		t.Errorf("spaced path quote: %q", got)
	}
	s := Session{ID: "abc", Cwd: "/Users/me/proj"}
	want := "cd /Users/me/proj && claude --resume abc"
	if got := s.ResumeCommandCd(ResumeModes[0]); got != want {
		t.Errorf("ResumeCommandCd: got %q want %q", got, want)
	}
	// No cwd -> bare command, no cd prefix.
	s2 := Session{ID: "abc"}
	if got := s2.ResumeCommandCd(ResumeModes[0]); got != "claude --resume abc" {
		t.Errorf("ResumeCommandCd without cwd: got %q", got)
	}
}

func TestStripANSI(t *testing.T) {
	styled := userStyle.Render("▶ You")
	plain := stripANSI(styled)
	if plain != "▶ You" {
		t.Errorf("stripANSI: got %q want %q", plain, "▶ You")
	}
}
