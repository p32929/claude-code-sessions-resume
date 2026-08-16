package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
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

// Chrome is indented one column on every screen so the left edge lines up from
// header to footer.
var pad = lipgloss.NewStyle().Padding(0, 1)

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

	// Status rows stay in the footer grey; the value of a setting is picked out
	// with weight rather than another colour, so the palette doesn't grow.
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
)

// sep joins the fields of a status row.
var sep = dimStyle.Render(" · ")

// field renders one "label value" pair for a status row.
func field(name, val string) string {
	return labelStyle.Render(name+": ") + valueStyle.Render(val)
}

// ---------- list items ----------

type projItem struct{ p Project }

func (i projItem) Title() string { return i.p.RealPath }
func (i projItem) Description() string {
	return fmt.Sprintf("%s · last used %s", plural(i.p.NumSess, "session"), relTime(i.p.LastUsed))
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

// newList builds a list that keeps its stock appearance — the default delegate
// and title styling are better tuned than anything hand-rolled here. Only the
// chrome this app draws itself is switched off: the status bar (its item count
// lives in the status row) and the help line (the footer lists the keys).
func newList(title string) list.Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	return l
}

func newModel() model {
	pi := textinput.New()
	pi.Placeholder = "/Users/you/path/to/project"
	pi.Prompt = "› "
	pi.CharLimit = 4096
	pi.Width = 60

	pl := newList("Claude Code Projects")
	sl := newList("Sessions")

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
			case "q":
				return m, tea.Quit
			case "esc":
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
				m.status = "sorted by " + sortModes[m.sortMode].Name
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
		case "q":
			return m, tea.Quit
		case "esc":
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
	// Rows each screen spends on chrome below its body:
	//   projects      state + keys                              = 2
	//   sessions      state + command(2) + keys                 = 4
	//   conversation  header + sticky above; state + command(2) + keys below = 6
	m.projList.SetSize(m.width, atLeast(m.height-2, 3))
	m.sessList.SetSize(m.width, atLeast(m.height-4, 3))
	m.convVP.Width = m.width
	m.convVP.Height = atLeast(m.height-6, 3)
}

// ---------- view ----------

func (m model) View() string {
	// Everything gets clamped to the terminal width here, once. Individual rows
	// don't have to be defensive about long paths, long commands or narrow
	// windows, and nothing can wrap and push the layout down a line.
	return m.clamp(m.screenBody())
}

func (m model) screenBody() string {
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

// Screens are stacked the same way everywhere, so your eye learns one shape:
//
//	body    the list or transcript
//	state   what's currently set (sort / filter / search) — always present
//	detail  the resume command, on screens where one applies
//	keys    what you can press
func (m model) viewProjectsRender() string {
	return m.screen(
		m.projList.View(),
		m.statusLine(filterStatus(m.projList, "project")),
		"",
		m.fitKeys("↑/↓ move · enter open · / filter · p paste path · q quit",
			"↑/↓ move · enter open · / filter · q quit"),
	)
}

// screen assembles the standard stack. Empty sections are skipped, and the
// error (if any) replaces the keys line so it can't be missed.
func (m model) screen(body, state, detail, keys string) string {
	out := body
	for _, s := range []string{state, detail} {
		if s != "" {
			out += "\n" + s
		}
	}
	if m.err != "" {
		return out + "\n" + errStyle.Render(oneLine(m.err, m.textWidth()))
	}
	return out + "\n" + footerStyle.Render(keys)
}

// fitKeys picks the fullest key hint that fits, so the footer never ends in a
// word chopped in half. Pass them longest first.
func (m model) fitKeys(options ...string) string {
	for _, o := range options {
		if lipgloss.Width(o)+2 <= m.width {
			return o
		}
	}
	return options[len(options)-1]
}

// textWidth is the usable width inside the one-column chrome padding.
func (m model) textWidth() int {
	if m.width < 20 {
		return 20
	}
	return m.width - 2
}

func (m model) viewPastePathRender() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Open a project by path") + "\n\n")
	b.WriteString(pad.Render(m.pathIn.View()) + "\n\n")
	b.WriteString(dimStyle.Render(pad.Render("Looks the folder up in ~/.claude/projects/<encoded>")) + "\n")
	if m.err != "" {
		b.WriteString("\n" + errStyle.Render(m.err) + "\n")
	}
	b.WriteString("\n" + footerStyle.Render("enter open · esc back · ctrl+c quit"))
	return b.String()
}

func (m model) viewSessionsRender() string {
	m.sessList.Title = "Sessions in " + m.curProject.RealPath
	it, ok := m.sessList.SelectedItem().(sessItem)
	if !ok {
		return m.screen(m.sessList.View(),
			m.statusLine(field("sort", sortModes[m.sortMode].Name), filterStatus(m.sessList, "session")),
			"", m.fitKeys("s sort · / filter · esc back · q quit", "esc back · q quit"))
	}
	return m.screen(
		m.sessList.View(),
		m.statusLine(field("sort", sortModes[m.sortMode].Name), filterStatus(m.sessList, "session")),
		m.commandBlock(it.s),
		m.fitKeys("↑/↓ move · enter read · c copy · m mode · s sort · / filter · esc back · q quit",
			"↑/↓ move · enter read · c copy · m mode · s sort · esc back · q quit",
			"enter read · c copy · m mode · esc back · q quit"),
	)
}

// commandBlock renders the resume command and, under it, what the active mode
// does — or the transient status ("copied ✓") right where you just acted.
// The mode is not spelled out anywhere else: the command itself carries the
// flag, and this line explains it.
func (m model) commandBlock(s Session) string {
	mode := ResumeModes[m.resumeMode]
	cmd := resumeStyle.Render("resume:  " + s.ResumeCommand(mode))
	if m.status != "" {
		return cmd + "\n" + statusStyle.Render(m.status)
	}
	return cmd + "\n" + dimStyle.Render(pad.Render(fmt.Sprintf("mode: %s — %s", mode.Name, mode.Desc)))
}

// statusLine renders the always-on "what's set right now" row.
func (m model) statusLine(fields ...string) string {
	return pad.Render(strings.Join(fields, sep))
}

// clamp cuts a rendered line to the terminal width. It has to be ANSI-aware —
// these lines are already styled, so counting raw bytes would slice an escape
// sequence in half and bleed colour across the rest of the screen.
func (m model) clamp(s string) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func (m model) viewConversationRender() string {
	// The header carries the session identity and how far down you are; the
	// sticky row carries which prompt you're reading under.
	head := titleStyle.Render(fmt.Sprintf("%s  ·  %s", short(m.curSession.ID),
		oneLine(m.curSession.Title, atLeast(m.textWidth()-20, 8)))) +
		dimStyle.Render(fmt.Sprintf("  %.0f%%", m.convVP.ScrollPercent()*100))

	keys := m.fitKeys(
		"↑/↓ scroll · [ ] prev/next prompt · / search · n/N matches · g/G top/bottom · c copy · m mode · esc back · q quit",
		"↑/↓ scroll · [ ] prompt · / search · n/N match · g/G ends · c copy · m mode · esc back · q quit",
		"↑/↓ scroll · / search · c copy · m mode · esc back · q quit",
	)
	if m.searching {
		keys = "enter jump to first match · esc cancel"
	}
	return head + "\n" +
		m.stickyHeader() + "\n" +
		m.convVP.View() + "\n" +
		m.statusLine(m.searchLine()) + "\n" +
		m.commandBlock(m.curSession) + "\n" +
		footerStyle.Render(keys)
}

// searchLine is the search field itself while you type, and the search state
// otherwise — the same row either way, so the layout never shifts.
func (m model) searchLine() string {
	if m.searching {
		return m.searchIn.View()
	}
	return m.searchStatus()
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

// filterStatus describes a list's filter in every state, including the
// unfiltered one, so the indicator never blinks out of existence. It also
// carries the item count, which is why the list's own status bar is switched
// off — that count used to appear twice, phrased two different ways.
func filterStatus(l list.Model, noun string) string {
	total := len(l.Items())
	shown := len(l.VisibleItems())
	switch l.FilterState() {
	case list.Filtering:
		return field("filter", quoteOr(l.FilterInput.Value(), "…")) +
			sep + field("showing", fmt.Sprintf("%d of %d", shown, total))
	case list.FilterApplied:
		return field("filter", quoteOr(strings.TrimSpace(l.FilterInput.Value()), "—")) +
			sep + field("showing", fmt.Sprintf("%d of %d", shown, total))
	default:
		return field("filter", "off") + sep + labelStyle.Render(plural(total, noun))
	}
}

// searchStatus describes the conversation search in every state, mirroring
// filterStatus so both screens read the same way.
func (m model) searchStatus() string {
	switch {
	case m.searching:
		return field("search", quoteOr(m.searchIn.Value(), "…"))
	case m.searchQ == "":
		return field("search", "off")
	case len(m.matches) == 0:
		return field("search", strconv.Quote(m.searchQ)) + sep + labelStyle.Render("no matches")
	default:
		return field("search", strconv.Quote(m.searchQ)) +
			sep + field("match", fmt.Sprintf("%d of %d", m.matchIdx+1, len(m.matches)))
	}
}

func quoteOr(s, empty string) string {
	if s == "" {
		return empty
	}
	return strconv.Quote(s)
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
	// The command itself is already on screen right above this line, so report
	// only what the clipboard adds to it: the cd that makes it runnable anywhere.
	if s.Cwd != "" {
		return "copied ✓  prefixed with cd " + s.Cwd
	}
	return "copied ✓"
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

// atLeast clamps a computed pane height so a very short terminal can't produce
// a zero or negative size.
func atLeast(n, min int) int {
	if n < min {
		return min
	}
	return n
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
				// Kept long; the sticky header trims it to the terminal width.
				anchors = append(anchors, userAnchor{line: lineCount, text: oneLine(t.Text, maxStoredTitle)})
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
		return labelStyle.Render(" (no displayable messages in this session)"), nil
	}
	// Indent to the same column as the header, state and footer rows. Done after
	// the fact so it can't disturb the line counts the anchors were built from.
	return indent(b.String()), anchors
}

// indent shifts every line one column right. Safe on already-styled text: a
// leading plain space can't land inside an escape sequence.
func indent(s string) string {
	return " " + strings.ReplaceAll(s, "\n", "\n ")
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
