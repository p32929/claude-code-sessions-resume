package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeHome builds a throwaway ~/.claude/projects with real session files.
func fakeHome(t *testing.T) (encDir string, paths []string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	encDir = filepath.Join(home, ".claude", "projects", "-tmp-demo")
	if err := os.MkdirAll(encDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"aaa11111-0000", "bbb22222-0000", "ccc33333-0000"} {
		p := filepath.Join(encDir, id+".jsonl")
		line := `{"type":"user","cwd":"/tmp/demo","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"prompt ` + id + `"}}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return encDir, paths
}

func delModel(t *testing.T, encDir string) model {
	t.Helper()
	ss, err := LoadSessions(encDir)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel()
	m.loading = false
	m.width, m.height = 110, 24
	m.state = viewSessions
	m.curProject = Project{EncodedDir: encDir, RealPath: "/tmp/demo", NumSess: len(ss)}
	m.curSessions = ss
	m.applySessionSort()
	m.layout()
	return m
}

func key(m model, k string) (model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	}
	mm, cmd := m.handleKey(msg)
	return mm.(model), cmd
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestDeleteAsksBeforeDeleting(t *testing.T) {
	encDir, paths := fakeHome(t)
	m := delModel(t, encDir)

	m, _ = key(m, "d")
	if m.pending == nil {
		t.Fatal("d did not arm a confirmation")
	}
	for _, p := range paths {
		if !exists(p) {
			t.Fatalf("%s deleted before confirming", p)
		}
	}
	out := stripANSI(m.View())
	// The wording adapts to the width; what must always be there is that it is
	// irreversible, and which key does what.
	if !strings.Contains(out, "cannot be undone") || !strings.Contains(out, "cancels") {
		t.Errorf("confirmation prompt missing:\n%s", out)
	}
	t.Logf("PROMPT: %s", strings.TrimSpace(strings.Split(out, "\n")[len(strings.Split(out, "\n"))-1]))
}

func TestAnyOtherKeyCancels(t *testing.T) {
	encDir, paths := fakeHome(t)
	for _, cancel := range []string{"n", "esc", "down", "x", "enter", "q"} {
		m := delModel(t, encDir)
		m, _ = key(m, "d")
		m, cmd := key(m, cancel)
		if m.pending != nil {
			t.Errorf("%q left the delete armed", cancel)
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Errorf("%q quit the app instead of cancelling", cancel)
			}
		}
		for _, p := range paths {
			if !exists(p) {
				t.Fatalf("%q deleted %s", cancel, p)
			}
		}
		if m.status != "delete cancelled" {
			t.Errorf("%q status = %q", cancel, m.status)
		}
	}
}

func TestYDeletesOnlyTheSelectedFile(t *testing.T) {
	encDir, paths := fakeHome(t)
	m := delModel(t, encDir)
	target := m.curSessions[0]

	m, _ = key(m, "d")
	m, _ = key(m, "y")

	if exists(target.FilePath) {
		t.Fatal("confirmed delete did not remove the file")
	}
	survived := 0
	for _, p := range paths {
		if p != target.FilePath && exists(p) {
			survived++
		}
	}
	if survived != 2 {
		t.Errorf("%d of 2 other sessions survived", survived)
	}
	if len(m.curSessions) != 2 {
		t.Errorf("list still has %d sessions", len(m.curSessions))
	}
	if m.curProject.NumSess != 2 {
		t.Errorf("project count = %d, want 2", m.curProject.NumSess)
	}
	if !strings.Contains(m.status, "deleted") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(stripANSI(m.View()), "2 sessions") {
		t.Error("status row not updated")
	}
}

func TestDeleteLastRowThenEmpty(t *testing.T) {
	encDir, _ := fakeHome(t)
	m := delModel(t, encDir)
	for i := 0; i < 3; i++ {
		m, _ = key(m, "down") // sit on the last row where possible
		m, _ = key(m, "d")
		m, _ = key(m, "y")
		if m.err != "" {
			t.Fatalf("round %d: %s", i, m.err)
		}
		_ = m.View() // must not panic with a shrinking / empty list
	}
	if len(m.curSessions) != 0 {
		t.Fatalf("expected empty list, got %d", len(m.curSessions))
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "0 sessions") {
		t.Errorf("empty state:\n%s", out)
	}
}

func TestDeleteFromConversationReturnsToList(t *testing.T) {
	encDir, _ := fakeHome(t)
	m := delModel(t, encDir)
	m.state = viewConversation
	m.curSession = m.curSessions[0]
	m.curTurns = []Turn{{Role: "user", Kind: "text", Text: "hi", Time: time.Now()}}
	m.setConversationContent(false)

	m, _ = key(m, "d")
	if m.pending == nil {
		t.Fatal("d did not arm on the conversation screen")
	}
	m, _ = key(m, "y")
	if m.state != viewSessions {
		t.Errorf("state = %v, want sessions", m.state)
	}
	if exists(m.curSession.FilePath) {
		t.Error("file not removed")
	}
}

func TestDeleteSessionRefusesForeignPaths(t *testing.T) {
	_, _ = fakeHome(t)
	outside := filepath.Join(t.TempDir(), "important.jsonl")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSession(outside); err == nil {
		t.Error("deleted a .jsonl outside ~/.claude/projects")
	}
	if !exists(outside) {
		t.Fatal("file outside the projects root was removed")
	}

	home, _ := os.UserHomeDir()
	notJSONL := filepath.Join(home, ".claude", "projects", "-tmp-demo", "notes.txt")
	if err := os.WriteFile(notJSONL, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSession(notJSONL); err == nil {
		t.Error("deleted a non-.jsonl file inside the projects root")
	}
	if !exists(notJSONL) {
		t.Fatal("non-session file was removed")
	}
}

func TestDeleteReportsFailure(t *testing.T) {
	encDir, _ := fakeHome(t)
	m := delModel(t, encDir)
	target := m.curSessions[0]
	if err := os.Remove(target.FilePath); err != nil { // vanished behind our back
		t.Fatal(err)
	}
	m, _ = key(m, "d")
	m, _ = key(m, "y")
	if m.err == "" {
		t.Error("no error surfaced when the file was already gone")
	}
	if !strings.Contains(stripANSI(m.View()), "delete failed") {
		t.Error("failure not shown in the UI")
	}
	t.Logf("err: %s", m.err)
}

func TestConfirmPromptFitsAnyWidth(t *testing.T) {
	encDir, _ := fakeHome(t)
	for _, w := range []int{30, 60, 90, 110, 160} {
		m := delModel(t, encDir)
		m.width = w
		m.layout()
		m, _ = key(m, "d")
		last := ""
		for _, ln := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.TrimSpace(ln) != "" {
				last = strings.TrimRight(ln, " ")
			}
		}
		t.Logf("w=%3d %s", w, strings.TrimSpace(last))
		if !strings.Contains(last, "cancels") {
			t.Errorf("w=%d: prompt lost the cancel instruction: %q", w, last)
		}
		if !strings.Contains(last, "y") {
			t.Errorf("w=%d: prompt lost the confirm key: %q", w, last)
		}
	}
}

func TestDeleteUpdatesProjectSizeAndCount(t *testing.T) {
	encDir, _ := fakeHome(t)
	projs, err := ListProjects()
	if err != nil || len(projs) != 1 {
		t.Fatalf("projects=%v err=%v", projs, err)
	}
	before := projs[0]
	if before.NumSess != 3 || before.SizeBytes == 0 {
		t.Fatalf("project scan wrong: %d sessions, %d bytes", before.NumSess, before.SizeBytes)
	}

	m := delModel(t, encDir)
	m.curProject = before
	m.curProjects = projs
	m.applyProjectSort()

	target := m.curSessions[0]
	m, _ = key(m, "d")
	m, _ = key(m, "y")

	wantSize := before.SizeBytes - target.SizeBytes
	if m.curProject.SizeBytes != wantSize {
		t.Errorf("project size = %d, want %d", m.curProject.SizeBytes, wantSize)
	}
	if m.curProject.NumSess != 2 {
		t.Errorf("project count = %d, want 2", m.curProject.NumSess)
	}

	// The projects list itself must agree, not just the model's copy.
	row, ok := m.projList.Items()[0].(projItem)
	if !ok {
		t.Fatal("projects list row is not a projItem")
	}
	if row.p.NumSess != 2 || row.p.SizeBytes != wantSize {
		t.Errorf("projects row stale: %d sessions, %d bytes; want 2 and %d",
			row.p.NumSess, row.p.SizeBytes, wantSize)
	}

	// And a fresh scan of the folder must match what the UI is claiming.
	rescan, err := ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(rescan) != 1 || rescan[0].SizeBytes != wantSize || rescan[0].NumSess != 2 {
		t.Errorf("on-disk scan disagrees with the UI: %+v", rescan)
	}
}

// projModel puts the app on the projects screen with a real scan behind it.
func projModel(t *testing.T) model {
	t.Helper()
	ps, err := ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel()
	m.loading = false
	m.width, m.height = 120, 24
	m.state = viewProjects
	m.projSort = 0 // "recent", regardless of any saved preference
	m.curProjects = ps
	m.applyProjectSort()
	m.layout()
	return m
}

func TestProjectDeleteAsksFirst(t *testing.T) {
	_, paths := fakeHome(t)
	m := projModel(t)

	m, _ = key(m, "d")
	if m.pending == nil || !m.pending.isProject {
		t.Fatal("d on the projects list did not arm a project delete")
	}
	for _, p := range paths {
		if !exists(p) {
			t.Fatalf("%s deleted before confirming", p)
		}
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "ALL 3 sessions") || !strings.Contains(out, "cannot be undone") {
		t.Errorf("project prompt does not say how much goes:\n%s", out)
	}
	t.Logf("PROMPT: %s", strings.TrimSpace(strings.Split(out, "\n")[len(strings.Split(out, "\n"))-1]))

	m, _ = key(m, "n")
	if m.pending != nil {
		t.Error("n left the project delete armed")
	}
	for _, p := range paths {
		if !exists(p) {
			t.Fatalf("n deleted %s", p)
		}
	}
}

func TestProjectDeleteRemovesEverything(t *testing.T) {
	encDir, paths := fakeHome(t)
	m := projModel(t)

	m, _ = key(m, "d")
	m, _ = key(m, "y")

	for _, p := range paths {
		if exists(p) {
			t.Errorf("%s survived", p)
		}
	}
	if exists(encDir) {
		t.Error("empty project folder was left behind")
	}
	if n := len(m.projList.Items()); n != 0 {
		t.Errorf("projects list still has %d rows", n)
	}
	if !strings.Contains(m.status, "deleted") || !strings.Contains(m.status, "3 sessions") {
		t.Errorf("status = %q", m.status)
	}
	if ps, _ := ListProjects(); len(ps) != 0 {
		t.Errorf("rescan still finds %d projects", len(ps))
	}
	_ = m.View() // empty list must render
}

func TestProjectDeleteLeavesOtherProjectsAlone(t *testing.T) {
	_, paths := fakeHome(t)
	home, _ := os.UserHomeDir()
	other := filepath.Join(home, ".claude", "projects", "-tmp-keep")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(other, "ddd44444-0000.jsonl")
	if err := os.WriteFile(keep, []byte(`{"type":"user","cwd":"/tmp/keep","message":{"role":"user","content":"keep me"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := projModel(t)
	// Delete whichever row is selected, then check the other survived intact.
	row := m.projList.SelectedItem().(projItem).p
	m, _ = key(m, "d")
	m, _ = key(m, "y")

	if row.EncodedDir == other {
		for _, p := range paths {
			if !exists(p) {
				t.Errorf("deleting -tmp-keep took %s with it", p)
			}
		}
	} else {
		if !exists(keep) {
			t.Error("deleting -tmp-demo took the other project's session with it")
		}
		if !exists(other) {
			t.Error("other project folder was removed")
		}
	}
	if n := len(m.projList.Items()); n != 1 {
		t.Errorf("projects list has %d rows, want 1", n)
	}
}

func TestProjectDeleteKeepsNonSessionFiles(t *testing.T) {
	encDir, _ := fakeHome(t)
	note := filepath.Join(encDir, "notes.txt")
	if err := os.WriteFile(note, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := projModel(t)
	m, _ = key(m, "d")
	m, _ = key(m, "y")

	if !exists(note) {
		t.Error("a non-session file in the folder was deleted")
	}
	if !exists(encDir) {
		t.Error("folder removed even though it still held a file")
	}
	if m.err != "" {
		t.Errorf("unexpected error: %s", m.err)
	}
}

func TestDeleteProjectRefusesForeignDirs(t *testing.T) {
	_, _ = fakeHome(t)
	outside := t.TempDir()
	if _, err := DeleteProject(outside); err == nil {
		t.Error("deleted a folder outside ~/.claude/projects")
	}
	if !exists(outside) {
		t.Fatal("folder outside the projects root was removed")
	}
	home, _ := os.UserHomeDir()
	if _, err := DeleteProject(filepath.Join(home, ".claude", "projects")); err == nil {
		t.Error("deleted the projects root itself")
	}
	nested := filepath.Join(home, ".claude", "projects", "-tmp-demo", "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteProject(nested); err == nil {
		t.Error("deleted a nested folder, not a top-level project")
	}
}

func TestProjectDeleteClearsLoadedSessions(t *testing.T) {
	encDir, _ := fakeHome(t)
	ss, _ := LoadSessions(encDir)
	m := projModel(t)
	m.curProject = Project{EncodedDir: encDir, RealPath: "/tmp/demo", NumSess: len(ss)}
	m.curSessions = ss
	m.applySessionSort()

	m, _ = key(m, "d")
	m, _ = key(m, "y")

	if len(m.curSessions) != 0 {
		t.Errorf("%d sessions still loaded from a deleted project", len(m.curSessions))
	}
	m.state = viewSessions
	_ = m.View() // must not panic pointing at a project that no longer exists
}

// buildProjects writes several projects with deliberately different counts,
// sizes and mtimes so every sort order is distinguishable.
func buildProjects(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	spec := []struct {
		dir   string
		files []int
		age   time.Duration
	}{
		{"-zeta", []int{500_000, 300_000}, 3 * time.Hour},        // biggest, 2 sessions
		{"-alpha", []int{1_000, 1_000, 1_000, 1_000}, time.Hour}, // most sessions, small
		{"-mid", []int{40_000}, 90 * time.Minute},
	}
	for _, sp := range spec {
		enc := filepath.Join(home, ".claude", "projects", sp.dir)
		if err := os.MkdirAll(enc, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, n := range sp.files {
			body := []byte(`{"type":"user","cwd":"` + sp.dir + `","message":{"role":"user","content":"p"}}` + "\n")
			body = append(body, make([]byte, n-len(body))...)
			f := filepath.Join(enc, "aaaa000"+string(rune('0'+i))+"-1111.jsonl")
			if err := os.WriteFile(f, body, 0o644); err != nil {
				t.Fatal(err)
			}
			when := time.Now().Add(-sp.age)
			if err := os.Chtimes(f, when, when); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func projOrder(m model) []string {
	var out []string
	for _, it := range m.projList.Items() {
		out = append(out, it.(projItem).p.RealPath)
	}
	return out
}

func TestProjectSortCycles(t *testing.T) {
	buildProjects(t)
	m := projModel(t)

	want := map[string][]string{
		"recent":   {"-alpha", "-mid", "-zeta"}, // newest mtime first
		"sessions": {"-alpha", "-zeta", "-mid"}, // 4, 2, 1
		"size":     {"-zeta", "-mid", "-alpha"}, // 800k, 40k, 4k
		"path":     {"-alpha", "-mid", "-zeta"}, // alphabetical
	}
	for i := 0; i < len(projectSortModes); i++ {
		name := projectSortModes[m.projSort].Name
		got := projOrder(m)
		t.Logf("sort %-8s %v", name, got)
		if exp, ok := want[name]; ok {
			for j := range exp {
				if j >= len(got) || got[j] != exp[j] {
					t.Errorf("sort %q: got %v, want %v", name, got, exp)
					break
				}
			}
		}
		out := stripANSI(m.View())
		if !strings.Contains(out, "sort: "+name) {
			t.Errorf("status row does not show sort %q:\n%s", name, out)
		}
		m, _ = key(m, "s")
	}
	if projectSortModes[m.projSort].Name != "recent" {
		t.Errorf("sort did not wrap around, landed on %q", projectSortModes[m.projSort].Name)
	}
}

func TestProjectSortIsRemembered(t *testing.T) {
	buildProjects(t)
	m := projModel(t)
	m, _ = key(m, "s")
	m, _ = key(m, "s") // -> "size"
	if got := projectSortModes[m.projSort].Name; got != "size" {
		t.Fatalf("sort = %q, want size", got)
	}
	if !strings.Contains(m.status, "sorted by size") {
		t.Errorf("no feedback for the sort change: %q", m.status)
	}
	if !strings.Contains(stripANSI(m.View()), "sorted by size") {
		t.Error("sort feedback not shown on the projects screen")
	}
	// A key that does nothing else must hand the keys line back.
	m, _ = key(m, "down")
	if out := stripANSI(m.View()); strings.Contains(out, "sorted by size") || !strings.Contains(out, "s sort") {
		t.Error("status did not clear on the next keypress")
	}
	// Fresh model, same HOME: the preference survives.
	if got := projectSortModes[loadProjectSort()].Name; got != "size" {
		t.Errorf("saved project sort = %q, want size", got)
	}
	if got := projectSortModes[newModel().projSort].Name; got != "size" {
		t.Errorf("new model project sort = %q, want size", got)
	}
}

func TestProjectStatusRowShowsTotals(t *testing.T) {
	buildProjects(t)
	m := projModel(t)
	out := stripANSI(m.View())
	t.Logf("PROJECTS\n%s", out)
	for _, want := range []string{"sort: recent", "filter: off", "3 projects", "on disk: "} {
		if !strings.Contains(out, want) {
			t.Errorf("status row missing %q", want)
		}
	}
	if want := humanSize(TotalProjectSize(m.curProjects)); !strings.Contains(out, "on disk: "+want) {
		t.Errorf("total should be %s:\n%s", want, out)
	}
}
