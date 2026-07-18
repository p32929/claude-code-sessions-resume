package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ansiRe matches SGR color escapes so we can recover the plain text of a
// styled transcript line for searching.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

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
	stickyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Background(lipgloss.Color("236")).Bold(true)
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Padding(0, 1)
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
	searchIn textinput.Model
	convVP   viewport.Model
	spin     spinner.Model

	loading  bool
	loadWhat string
	status   string // transient one-line feedback (e.g. "copied ✓")

	curProject  Project
	curSession  Session
	curSessions []Session    // raw sessions for the current project, kept so we can re-sort
	resumeMode  int          // index into ResumeModes
	sortMode    int          // index into sortModes
	curTurns    []Turn       // parsed transcript, kept so we can re-wrap on resize
	convAnchors []userAnchor // where each "You" prompt sits in the transcript
	convWidth   int          // width the transcript was last wrapped to
	convPlain   []string     // lowercased plain text per rendered line, for search

	searching bool   // conversation search input is focused
	searchQ   string // active search query
	matches   []int  // rendered line indices that contain the query
	matchIdx  int    // which match we're currently parked on
}

// userAnchor records the line at which a user prompt starts in the rendered
// transcript, plus a one-line summary used for the sticky header.
type userAnchor struct {
	line int
	text string
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

	si := textinput.New()
	si.Placeholder = "search…"
	si.Prompt = "/"
	si.CharLimit = 200
	si.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	m := model{
		state:      viewProjects,
		projList:   pl,
		sessList:   sl,
		pathIn:     pi,
		searchIn:   si,
		convVP:     viewport.New(0, 0),
		spin:       sp,
		loading:    true,
		loadWhat:   "Loading projects",
		resumeMode: loadResumeModeIndex(),
		sortMode:   loadSortMode(),
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
		// The transcript is pre-wrapped to a fixed width, so it only needs a
		// (potentially expensive) re-wrap when the WIDTH changes. Height-only
		// resizes just re-size the viewport, which is cheap.
		if m.state == viewConversation && m.width != m.convWidth {
			m.setConversationContent(true)
		}
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
		m.status = ""
		m.curProject = msg.project
		m.curSessions = msg.sessions
		m.applySessionSort()
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
		m.curTurns = msg.turns
		m.searching = false
		m.searchIn.Blur()
		m.searchQ = ""
		m.matches = nil
		m.matchIdx = 0
		m.status = ""
		m.setConversationContent(false)
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
				saveResumeModeIndex(m.resumeMode)
				m.status = ""
				return m, nil
			case "s":
				m.sortMode = (m.sortMode + 1) % len(sortModes)
				saveSortMode(m.sortMode)
				m.applySessionSort()
				m.sessList.ResetSelected()
				m.status = ""
				return m, nil
			case "c":
				m.status = m.copyResume()
				return m, nil
			case "enter":
				if it, ok := m.sessList.SelectedItem().(sessItem); ok {
					m.curSession = it.s
					cmd := m.startLoad("Loading conversation", loadConversationCmd(it.s.FilePath))
					return m, cmd
				}
			}
		}
		m.status = ""
		var cmd tea.Cmd
		m.sessList, cmd = m.sessList.Update(msg)
		return m, cmd

	case viewConversation:
		// While the search field is focused, it consumes typing.
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchIn.Blur()
				return m, nil
			case "enter":
				m.searchQ = strings.TrimSpace(m.searchIn.Value())
				m.searching = false
				m.searchIn.Blur()
				m.recomputeMatches()
				m.jumpToMatch()
				return m, nil
			}
			var cmd tea.Cmd
			m.searchIn, cmd = m.searchIn.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "q", "esc":
			m.state = viewSessions
			return m, nil
		case "m":
			m.resumeMode = cycleMode(m.resumeMode)
			saveResumeModeIndex(m.resumeMode)
			m.status = ""
			return m, nil
		case "c":
			m.status = m.copyResume()
			return m, nil
		case "/":
			m.searching = true
			m.searchIn.SetValue(m.searchQ)
			m.searchIn.CursorEnd()
			m.searchIn.Focus()
			m.status = ""
			return m, textinput.Blink
		case "n":
			if len(m.matches) > 0 {
				m.matchIdx++
				m.jumpToMatch()
			}
			return m, nil
		case "N":
			if len(m.matches) > 0 {
				m.matchIdx--
				m.jumpToMatch()
			}
			return m, nil
		case "]", "}":
			m.jumpPrompt(1)
			return m, nil
		case "[", "{":
			m.jumpPrompt(-1)
			return m, nil
		case "g", "home":
			m.convVP.GotoTop()
			return m, nil
		case "G", "end":
			m.convVP.GotoBottom()
			return m, nil
		}
		m.status = ""
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
	// Sessions view reserves 4 footer lines (resume, cwd, mode+sort, keys).
	m.sessList.SetSize(m.width, m.height-5)
	m.convVP.Width = m.width
	// Reserve rows for: title, sticky prompt header, and a 2-line footer.
	m.convVP.Height = m.height - 5
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
		modeLine := dimStyle.Render(fmt.Sprintf("mode: %s — %s     sort: %s", mode.Name, mode.Desc, sortModes[m.sortMode].Name))
		last := footerStyle.Render("enter view · c copy cmd · m mode · s sort · / filter · esc back")
		if m.status != "" {
			last = statusStyle.Render(m.status)
		}
		footer = resume + "\n" + cwd + "\n" + modeLine + "\n" + last
	} else {
		footer = footerStyle.Render("no sessions · s sort · esc back")
	}
	if m.err != "" {
		footer = errStyle.Render(m.err) + "\n" + footer
	}
	return body + "\n" + footer
}

func (m model) viewConversationRender() string {
	header := titleStyle.Render(fmt.Sprintf("%s  ·  %s", short(m.curSession.ID), oneLine(m.curSession.Title, 60)))
	sticky := m.stickyHeader()
	return header + "\n" + sticky + "\n" + m.convVP.View() + "\n" + m.convFooter()
}

// convFooter renders the two-line conversation footer: a search line and a
// keybinding line (or a transient status in place of the keys).
func (m model) convFooter() string {
	mode := ResumeModes[m.resumeMode]
	scrollPct := fmt.Sprintf("%3.0f%%", m.convVP.ScrollPercent()*100)

	// Line 1: the search field while typing, otherwise navigation help + counter.
	var line1 string
	if m.searching {
		line1 = footerStyle.Render(m.searchIn.View() + "   (enter jump · esc cancel)")
	} else {
		nav := "↑/↓/pgup/pgdn scroll · [ ] prev/next prompt · / search · n/N matches · g/G top/bottom"
		if m.searchQ != "" {
			if len(m.matches) == 0 {
				nav += fmt.Sprintf("   no match for \"%s\"", m.searchQ)
			} else {
				nav += fmt.Sprintf("   match %d/%d for \"%s\"", m.matchIdx+1, len(m.matches), m.searchQ)
			}
		}
		line1 = footerStyle.Render(nav)
	}

	// Line 2: status (if any) else copy/mode/back keys.
	var line2 string
	if m.status != "" {
		line2 = statusStyle.Render(m.status)
	} else {
		line2 = footerStyle.Render(fmt.Sprintf("c copy cmd · m mode [%s] · esc back · %s", mode.Name, scrollPct))
	}
	return line1 + "\n" + line2
}

// setConversationContent (re-)wraps the stored transcript to the current width
// and loads it into the viewport. When preserve is true the scroll position is
// kept as close as possible, so a resize doesn't jump the reader around.
func (m *model) setConversationContent(preserve bool) {
	off := m.convVP.YOffset
	body, anchors := renderConversation(m.curTurns, m.width)
	m.convVP.SetContent(body)
	m.convAnchors = anchors
	m.convWidth = m.width
	// Keep a lowercased, un-styled copy of each rendered line so search can run
	// against plain text. Line count matches the viewport 1:1 (styling adds no
	// newlines and the transcript is pre-wrapped).
	lines := strings.Split(stripANSI(body), "\n")
	m.convPlain = make([]string, len(lines))
	for i, ln := range lines {
		m.convPlain[i] = strings.ToLower(ln)
	}
	if preserve {
		m.convVP.SetYOffset(off)
	}
	// A width change re-numbers every line, so any active search must be redone.
	if m.searchQ != "" {
		m.recomputeMatches()
	}
}

// stickyHeader renders the user prompt that the current scroll position sits
// under, pinned above the transcript so you always know which turn you're in.
func (m model) stickyHeader() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	label := "▶ You"
	if len(m.convAnchors) > 0 {
		off := m.convVP.YOffset
		cur := m.convAnchors[0]
		for _, a := range m.convAnchors {
			if a.line <= off {
				cur = a
			} else {
				break
			}
		}
		if cur.text != "" {
			label = "▶ You: " + cur.text
		}
	}
	return stickyStyle.Width(w).Render(oneLine(label, w-1))
}

// cycleMode advances to the next resume mode, wrapping around.
func cycleMode(i int) int {
	return (i + 1) % len(ResumeModes)
}

// copyResume copies the resume command for the current selection to the system
// clipboard and returns a one-line status describing the outcome.
func (m *model) copyResume() string {
	var s Session
	switch {
	case m.state == viewConversation:
		s = m.curSession
	default:
		it, ok := m.sessList.SelectedItem().(sessItem)
		if !ok {
			return "nothing to copy"
		}
		s = it.s
	}
	cmd := s.ResumeCommandCd(ResumeModes[m.resumeMode])
	if err := clipboard.WriteAll(cmd); err != nil {
		return "copy failed: " + err.Error()
	}
	return "copied ✓  " + s.ResumeCommand(ResumeModes[m.resumeMode])
}

// applySessionSort re-orders the current project's sessions by the active sort
// mode and rebuilds the list items.
func (m *model) applySessionSort() {
	sortSessions(m.curSessions, m.sortMode)
	items := make([]list.Item, len(m.curSessions))
	for i, s := range m.curSessions {
		items[i] = sessItem{s}
	}
	m.sessList.SetItems(items)
}

// recomputeMatches rebuilds the list of rendered line indices that contain the
// active search query (case-insensitive).
func (m *model) recomputeMatches() {
	m.matches = m.matches[:0]
	m.matchIdx = 0
	q := strings.ToLower(strings.TrimSpace(m.searchQ))
	if q == "" {
		return
	}
	for i, ln := range m.convPlain {
		if strings.Contains(ln, q) {
			m.matches = append(m.matches, i)
		}
	}
}

// jumpToMatch scrolls the viewport so the current match sits near the top.
func (m *model) jumpToMatch() {
	if len(m.matches) == 0 {
		return
	}
	if m.matchIdx < 0 {
		m.matchIdx = len(m.matches) - 1
	}
	if m.matchIdx >= len(m.matches) {
		m.matchIdx = 0
	}
	m.convVP.SetYOffset(m.matches[m.matchIdx])
}

// jumpPrompt moves the viewport to the next (dir>0) or previous (dir<0) user
// prompt relative to the current scroll position.
func (m *model) jumpPrompt(dir int) {
	if len(m.convAnchors) == 0 {
		return
	}
	off := m.convVP.YOffset
	if dir > 0 {
		for _, a := range m.convAnchors {
			if a.line > off {
				m.convVP.SetYOffset(a.line)
				return
			}
		}
		return
	}
	target := -1
	for _, a := range m.convAnchors {
		if a.line < off {
			target = a.line
		} else {
			break
		}
	}
	if target >= 0 {
		m.convVP.SetYOffset(target)
	}
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

func renderConversation(turns []Turn, width int) (string, []userAnchor) {
	if width <= 0 {
		width = 80
	}
	wrap := width - 2
	if wrap < 20 {
		wrap = 20
	}
	var b strings.Builder
	var anchors []userAnchor
	lineCount := 0
	// write appends text and keeps a running count of the lines emitted so far,
	// so anchors can point at the exact line a prompt begins on.
	write := func(s string) {
		b.WriteString(s)
		lineCount += strings.Count(s, "\n")
	}
	for _, t := range turns {
		switch t.Kind {
		case "text":
			if t.Role == "user" {
				anchors = append(anchors, userAnchor{line: lineCount, text: oneLine(t.Text, 100)})
				write(userStyle.Render("▶ You") + "\n")
			} else {
				write(claudeStyle.Render("● Claude") + "\n")
			}
			write(wrapText(t.Text, wrap) + "\n\n")
		case "thinking":
			write(thinkStyle.Render("· thinking") + "\n")
			write(thinkStyle.Render(wrapText(t.Text, wrap)) + "\n\n")
		case "tool_use":
			line := fmt.Sprintf("⚙ %s(%s)", t.Name, oneLine(t.Text, wrap-len(t.Name)-4))
			write(toolStyle.Render(wrapText(line, wrap)) + "\n\n")
		case "tool_result":
			style := dimStyle
			label := "⤷ result"
			if t.IsError {
				style = errTurnStyle
				label = "⤷ error"
			}
			write(style.Render(label) + "\n")
			write(style.Render(wrapText(oneLineBudget(t.Text, wrap, 12), wrap)) + "\n\n")
		}
	}
	if b.Len() == 0 {
		return dimStyle.Render("  (no displayable messages in this session)"), nil
	}
	return b.String(), anchors
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
