package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------- view states ----------

type view int

const (
	viewProjects view = iota
	viewPastePath
	viewSessions
	viewConversation
)

// ---------- styles ----------

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Padding(0, 1)
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	resumeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Padding(0, 1)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Padding(0, 1)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	userStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	claudeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	thinkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	errTurnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	loadStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Padding(0, 1)
)

// ---------- list items ----------

type projItem struct{ p Project }

func (i projItem) Title() string { return i.p.RealPath }
func (i projItem) Description() string {
	return fmt.Sprintf("%d session(s) · last used %s", i.p.NumSess, relTime(i.p.LastUsed))
}
func (i projItem) FilterValue() string { return i.p.RealPath }

type sessItem struct{ s Session }

func (i sessItem) Title() string { return i.s.Title }
func (i sessItem) Description() string {
	return fmt.Sprintf("%s · %d msgs · %s · %s",
		short(i.s.ID), i.s.MsgCount, relTime(i.s.End), humanSize(i.s.SizeBytes))
}
func (i sessItem) FilterValue() string { return i.s.Title + " " + i.s.ID }

func short(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// ---------- model ----------

type model struct {
	state  view
	width  int
	height int
	err    string

	projList list.Model
	sessList list.Model
	pathIn   textinput.Model
	convVP   viewport.Model
	spin     spinner.Model

	loading  bool
	loadWhat string

	curProject Project
	curSession Session
	resumeMode int // index into ResumeModes
}

func newModel() model {
	pi := textinput.New()
	pi.Placeholder = "/Users/you/path/to/project"
	pi.Prompt = "› "
	pi.CharLimit = 4096
	pi.Width = 60

	projDelegate := list.NewDefaultDelegate()
	pl := list.New(nil, projDelegate, 0, 0)
	pl.Title = "Claude Code Projects"
	pl.SetShowStatusBar(true)
	pl.SetFilteringEnabled(true)

	sessDelegate := list.NewDefaultDelegate()
	sl := list.New(nil, sessDelegate, 0, 0)
	sl.Title = "Sessions"
	sl.SetShowStatusBar(true)
	sl.SetFilteringEnabled(true)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	m := model{
		state:    viewProjects,
		projList: pl,
		sessList: sl,
		pathIn:   pi,
		convVP:   viewport.New(0, 0),
		spin:     sp,
		loading:  true,
		loadWhat: "Loading projects",
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(loadProjectsCmd, m.spin.Tick)
}

// ---------- messages ----------

type projectsLoadedMsg struct {
	projects []Project
	err      error
}
type sessionsLoadedMsg struct {
	project  Project
	sessions []Session
	err      error
}
type conversationLoadedMsg struct {
	turns []Turn
	err   error
}

func loadProjectsCmd() tea.Msg {
	ps, err := ListProjects()
	return projectsLoadedMsg{projects: ps, err: err}
}

func loadSessionsCmd(p Project) tea.Cmd {
	return func() tea.Msg {
		ss, err := LoadSessions(p.EncodedDir)
		return sessionsLoadedMsg{project: p, sessions: ss, err: err}
	}
}

func loadConversationCmd(path string) tea.Cmd {
	return func() tea.Msg {
		ts, err := LoadConversation(path)
		return conversationLoadedMsg{turns: ts, err: err}
	}
}

// resolvePathCmd builds a Project from a user-pasted folder path.
func resolvePathCmd(raw string) tea.Cmd {
	return func() tea.Msg {
		p := strings.TrimSpace(raw)
		p = expandHome(p)
		root, err := projectsRoot()
		if err != nil {
			return sessionsLoadedMsg{err: err}
		}
		enc := encodePath(p)
		dir := root + string(os.PathSeparator) + enc
		if _, statErr := os.Stat(dir); statErr != nil {
			return sessionsLoadedMsg{err: fmt.Errorf("no Claude sessions found for:\n  %s\n(looked in %s)", p, dir)}
		}
		proj := Project{EncodedDir: dir, RealPath: p}
		ss, err := LoadSessions(dir)
		proj.NumSess = len(ss)
		return sessionsLoadedMsg{project: proj, sessions: ss, err: err}
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// ---------- update ----------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case projectsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		items := make([]list.Item, len(msg.projects))
		for i, p := range msg.projects {
			items[i] = projItem{p}
		}
		m.projList.SetItems(items)
		return m, nil

	case sessionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.state = viewProjects
			return m, nil
		}
		m.err = ""
		m.curProject = msg.project
		items := make([]list.Item, len(msg.sessions))
		for i, s := range msg.sessions {
			items[i] = sessItem{s}
		}
		m.sessList.SetItems(items)
		m.sessList.Title = "Sessions · " + msg.project.RealPath
		m.sessList.ResetSelected()
		m.state = viewSessions
		m.layout()
		return m, nil

	case conversationLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.convVP.SetContent(renderConversation(msg.turns, m.width))
		m.convVP.GotoTop()
		m.state = viewConversation
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m.routeUpdate(msg)
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit (but let the list's filter input consume typing).
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.state {
	case viewProjects:
		if m.projList.FilterState().String() != "filtering" {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "p":
				m.state = viewPastePath
				m.pathIn.Focus()
				return m, textinput.Blink
			case "enter":
				if it, ok := m.projList.SelectedItem().(projItem); ok {
					cmd := m.startLoad("Loading sessions", loadSessionsCmd(it.p))
					return m, cmd
				}
			}
		}
		var cmd tea.Cmd
		m.projList, cmd = m.projList.Update(msg)
		return m, cmd

	case viewPastePath:
		switch msg.String() {
		case "esc":
			m.state = viewProjects
			m.pathIn.Blur()
			return m, nil
		case "enter":
			val := m.pathIn.Value()
			if strings.TrimSpace(val) == "" {
				return m, nil
			}
			cmd := m.startLoad("Resolving path", resolvePathCmd(val))
			return m, cmd
		}
		var cmd tea.Cmd
		m.pathIn, cmd = m.pathIn.Update(msg)
		return m, cmd

	case viewSessions:
		if m.sessList.FilterState().String() != "filtering" {
			switch msg.String() {
			case "q", "esc":
				m.state = viewProjects
				return m, nil
			case "m":
				m.resumeMode = cycleMode(m.resumeMode)
				return m, nil
			case "enter":
				if it, ok := m.sessList.SelectedItem().(sessItem); ok {
					m.curSession = it.s
					cmd := m.startLoad("Loading conversation", loadConversationCmd(it.s.FilePath))
					return m, cmd
				}
			}
		}
		var cmd tea.Cmd
		m.sessList, cmd = m.sessList.Update(msg)
		return m, cmd

	case viewConversation:
		switch msg.String() {
		case "q", "esc":
			m.state = viewSessions
			return m, nil
		case "m":
			m.resumeMode = cycleMode(m.resumeMode)
			return m, nil
		}
		var cmd tea.Cmd
		m.convVP, cmd = m.convVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) routeUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.state {
	case viewProjects:
		m.projList, cmd = m.projList.Update(msg)
	case viewSessions:
		m.sessList, cmd = m.sessList.Update(msg)
	case viewConversation:
		m.convVP, cmd = m.convVP.Update(msg)
	case viewPastePath:
		m.pathIn, cmd = m.pathIn.Update(msg)
	}
	return m, cmd
}

// layout resizes child components to the current terminal size.
func (m *model) layout() {
	if m.width == 0 {
		return
	}
	listH := m.height - 2
	if listH < 3 {
		listH = 3
	}
	m.projList.SetSize(m.width, listH)
	// Sessions view reserves 3 footer lines for the resume command.
	m.sessList.SetSize(m.width, m.height-4)
	m.convVP.Width = m.width
	m.convVP.Height = m.height - 3
}

// ---------- view ----------

func (m model) View() string {
	if m.loading {
		return m.loaderView()
	}
	switch m.state {
	case viewProjects:
		return m.viewProjectsRender()
	case viewPastePath:
		return m.viewPastePathRender()
	case viewSessions:
		return m.viewSessionsRender()
	case viewConversation:
		return m.viewConversationRender()
	}
	return ""
}

// loaderView shows a centered spinner with a label while work is in flight.
func (m model) loaderView() string {
	content := m.spin.View() + loadStyle.Render(m.loadWhat+" …")
	w, h := m.width, m.height
	if w <= 0 {
		return content
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

func (m model) viewProjectsRender() string {
	body := m.projList.View()
	footer := footerStyle.Render("↑/↓ move · / filter · enter open · p paste path · q quit")
	if m.err != "" {
		footer = errStyle.Render(m.err)
	}
	return body + "\n" + footer
}

func (m model) viewPastePathRender() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Paste or type a project folder path") + "\n\n")
	b.WriteString(m.pathIn.View() + "\n\n")
	b.WriteString(footerStyle.Render("enter resolve · esc back · ctrl+c quit") + "\n")
	if m.err != "" {
		b.WriteString("\n" + errStyle.Render(m.err) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("Tip: this maps your folder to ~/.claude/projects/<encoded>"))
	return b.String()
}

func (m model) viewSessionsRender() string {
	body := m.sessList.View()
	var footer string
	if it, ok := m.sessList.SelectedItem().(sessItem); ok {
		mode := ResumeModes[m.resumeMode]
		resume := resumeStyle.Render("resume:  " + it.s.ResumeCommand(mode))
		cwd := dimStyle.Render("run from: " + orDash(it.s.Cwd))
		modeLine := dimStyle.Render(fmt.Sprintf("mode:    %s — %s", mode.Name, mode.Desc))
		help := footerStyle.Render("enter view · / filter · m cycle mode · esc back")
		footer = resume + "\n" + cwd + "\n" + modeLine + "\n" + help
	} else {
		footer = footerStyle.Render("no session selected · esc back")
	}
	if m.err != "" {
		footer = errStyle.Render(m.err) + "\n" + footer
	}
	return body + "\n" + footer
}

func (m model) viewConversationRender() string {
	header := titleStyle.Render(fmt.Sprintf("%s  ·  %s", short(m.curSession.ID), oneLine(m.curSession.Title, 60)))
	mode := ResumeModes[m.resumeMode]
	scrollPct := fmt.Sprintf("%3.0f%%", m.convVP.ScrollPercent()*100)
	footer := footerStyle.Render(fmt.Sprintf("↑/↓/pgup/pgdn scroll · %s · m mode [%s] · esc back    resume: %s",
		scrollPct, mode.Name, m.curSession.ResumeCommand(mode)))
	return header + "\n" + m.convVP.View() + "\n" + footer
}

// cycleMode advances to the next resume mode, wrapping around.
func cycleMode(i int) int {
	return (i + 1) % len(ResumeModes)
}

// startLoad marks the model busy with a labelled spinner and returns a command
// that runs the given work alongside the spinner animation.
func (m *model) startLoad(what string, work tea.Cmd) tea.Cmd {
	m.loading = true
	m.loadWhat = what
	m.err = ""
	return tea.Batch(work, m.spin.Tick)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---------- conversation rendering ----------

func renderConversation(turns []Turn, width int) string {
	if width <= 0 {
		width = 80
	}
	wrap := width - 2
	if wrap < 20 {
		wrap = 20
	}
	var b strings.Builder
	for _, t := range turns {
		switch t.Kind {
		case "text":
			if t.Role == "user" {
				b.WriteString(userStyle.Render("▶ You") + "\n")
			} else {
				b.WriteString(claudeStyle.Render("● Claude") + "\n")
			}
			b.WriteString(wrapText(t.Text, wrap) + "\n\n")
		case "thinking":
			b.WriteString(thinkStyle.Render("· thinking") + "\n")
			b.WriteString(thinkStyle.Render(wrapText(t.Text, wrap)) + "\n\n")
		case "tool_use":
			line := fmt.Sprintf("⚙ %s(%s)", t.Name, oneLine(t.Text, wrap-len(t.Name)-4))
			b.WriteString(toolStyle.Render(wrapText(line, wrap)) + "\n\n")
		case "tool_result":
			style := dimStyle
			label := "⤷ result"
			if t.IsError {
				style = errTurnStyle
				label = "⤷ error"
			}
			b.WriteString(style.Render(label) + "\n")
			b.WriteString(style.Render(wrapText(oneLineBudget(t.Text, wrap, 12), wrap)) + "\n\n")
		}
	}
	if b.Len() == 0 {
		return dimStyle.Render("  (no displayable messages in this session)")
	}
	return b.String()
}

// oneLineBudget caps a tool result to at most `maxLines` wrapped-ish lines.
func oneLineBudget(s string, width, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("… (%d more lines)", len(lines)-maxLines))
	}
	return strings.Join(lines, "\n")
}

// wrapText hard-wraps text to the given width, preserving existing newlines.
func wrapText(s string, width int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) string2Slice {
	if width <= 0 {
		return []string{line}
	}
	var res []string
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	cur := ""
	for _, w := range words {
		// Break a single over-long word.
		for len([]rune(w)) > width {
			if cur != "" {
				res = append(res, cur)
				cur = ""
			}
			res = append(res, string([]rune(w)[:width]))
			w = string([]rune(w)[width:])
		}
		if cur == "" {
			cur = w
		} else if len([]rune(cur))+1+len([]rune(w)) <= width {
			cur += " " + w
		} else {
			res = append(res, cur)
			cur = w
		}
	}
	if cur != "" {
		res = append(res, cur)
	}
	return res
}

type string2Slice = []string

// ---------- main ----------

func main() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
