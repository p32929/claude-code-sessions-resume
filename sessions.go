package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------- Raw JSONL line ----------

// rawLine is a permissive view of one line in a session .jsonl file.
// Claude Code writes many event types; we only decode the fields we need.
type rawLine struct {
	Type         string          `json:"type"`
	Message      *rawMessage     `json:"message"`
	Timestamp    string          `json:"timestamp"`
	Cwd          string          `json:"cwd"`
	SessionID    string          `json:"sessionId"`
	GitBranch    string          `json:"gitBranch"`
	Version      string          `json:"version"`
	IsSidechain  bool            `json:"isSidechain"`
	Origin       *originInfo     `json:"origin"`
	PromptSource string          `json:"promptSource"`
	Summary      string          `json:"summary"` // present on {"type":"summary"} lines
}

type originInfo struct {
	Kind string `json:"kind"`
}

type rawMessage struct {
	Role  string          `json:"role"`
	Model string          `json:"model"`
	// Content is either a JSON string or an array of content blocks.
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Thinking string         `json:"thinking"`
	Name    string          `json:"name"`  // tool_use
	Input   json.RawMessage `json:"input"` // tool_use
	Content json.RawMessage `json:"content"` // tool_result (string or array)
	ToolUseID string        `json:"tool_use_id"`
	IsError bool            `json:"is_error"`
}

// ---------- Session metadata ----------

type Session struct {
	ID        string
	FilePath  string
	Cwd       string
	GitBranch string
	Version   string
	Title     string // first human prompt, trimmed
	MsgCount  int    // user + assistant turns
	Start     time.Time
	End       time.Time
	SizeBytes int64
}

// ResumeMode is one of Claude Code's permission modes, plus the flag needed
// to launch a resumed session in that mode.
type ResumeMode struct {
	Name string // short label shown in the UI
	Flag string // flag(s) appended to the resume command ("" = plain default)
	Desc string // one-line hint about what the mode does
}

// ResumeModes lists every mode the user can cycle through with the mode key.
// Order matches Claude Code's escalation from safest to most permissive.
var ResumeModes = []ResumeMode{
	{"normal", "", "permissions work normally (asks before acting)"},
	{"plan", "--permission-mode plan", "read-only; plans before making changes"},
	{"accept edits", "--permission-mode acceptEdits", "auto-accepts file edits"},
	{"auto", "--permission-mode auto", "auto-approves allowed actions"},
	{"don't ask", "--permission-mode dontAsk", "does not prompt for permissions"},
	{"bypass permissions", "--dangerously-skip-permissions", "skips ALL permission checks"},
}

// ResumeCommand returns the shell command to resume this session in the
// given mode.
func (s Session) ResumeCommand(mode ResumeMode) string {
	if mode.Flag == "" {
		return fmt.Sprintf("claude --resume %s", s.ID)
	}
	return fmt.Sprintf("claude --resume %s %s", s.ID, mode.Flag)
}

// ---------- Project (a folder that has Claude sessions) ----------

type Project struct {
	EncodedDir string // absolute path under ~/.claude/projects
	RealPath   string // decoded original working directory
	NumSess    int
	LastUsed   time.Time
}

// projectsRoot returns ~/.claude/projects
func projectsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// encodePath maps a real folder path to Claude Code's encoded dir name.
// Claude replaces every "/" (and ".") in the path with "-".
func encodePath(p string) string {
	p = strings.TrimRight(p, "/")
	r := strings.NewReplacer("/", "-", ".", "-", "_", "-")
	return r.Replace(p)
}

// decodeDirName is a best-effort reverse of the encoding. Because the
// encoding is lossy (both "/" and "-" map to "-"), we recover the real path
// from a session file's cwd field when possible; this is only the fallback.
func decodeDirName(name string) string {
	if strings.HasPrefix(name, "-") {
		return "/" + strings.ReplaceAll(name[1:], "-", "/")
	}
	return strings.ReplaceAll(name, "-", "/")
}

// ListProjects scans ~/.claude/projects and returns one entry per folder.
func ListProjects() ([]Project, error) {
	root, err := projectsRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var projs []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, real, last := scanDirQuick(dir)
		if files == 0 {
			continue
		}
		if real == "" {
			real = decodeDirName(e.Name())
		}
		projs = append(projs, Project{
			EncodedDir: dir,
			RealPath:   real,
			NumSess:    files,
			LastUsed:   last,
		})
	}
	sort.Slice(projs, func(i, j int) bool {
		return projs[i].LastUsed.After(projs[j].LastUsed)
	})
	return projs, nil
}

// scanDirQuick counts .jsonl files, sniffs the real cwd from the newest file,
// and returns the newest mod time. Cheap: reads only a few lines.
func scanDirQuick(dir string) (count int, realPath string, last time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, "", time.Time{}
	}
	var newest os.FileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		count++
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest = info
		}
	}
	if newest != nil {
		last = newest.ModTime()
		realPath = sniffCwd(filepath.Join(dir, newest.Name()))
	}
	return count, realPath, last
}

// sniffCwd reads the first lines of a session file to find its cwd.
func sniffCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for i := 0; sc.Scan() && i < 50; i++ {
		var l rawLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Cwd != "" {
			return l.Cwd
		}
	}
	return ""
}

// LoadSessions returns metadata for every session file in an encoded dir.
func LoadSessions(encodedDir string) ([]Session, error) {
	entries, err := os.ReadDir(encodedDir)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(encodedDir, e.Name())
		s, err := readSessionMeta(path)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].End.After(sessions[j].End)
	})
	return sessions, nil
}

// readSessionMeta streams a session file to collect summary metadata.
func readSessionMeta(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	info, _ := f.Stat()
	s := Session{
		ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		FilePath:  path,
		SizeBytes: info.Size(),
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var l rawLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if s.Cwd == "" && l.Cwd != "" {
			s.Cwd = l.Cwd
		}
		if s.GitBranch == "" && l.GitBranch != "" {
			s.GitBranch = l.GitBranch
		}
		if s.Version == "" && l.Version != "" {
			s.Version = l.Version
		}
		if ts := parseTime(l.Timestamp); !ts.IsZero() {
			if s.Start.IsZero() || ts.Before(s.Start) {
				s.Start = ts
			}
			if ts.After(s.End) {
				s.End = ts
			}
		}
		if l.Type == "user" || l.Type == "assistant" {
			s.MsgCount++
		}
		// First human-typed prompt becomes the title.
		if s.Title == "" && l.Type == "user" && l.Message != nil {
			if t := firstPromptText(l); t != "" {
				s.Title = t
			}
		}
	}
	if s.End.IsZero() && info != nil {
		s.End = info.ModTime()
	}
	if s.Title == "" {
		s.Title = "(no prompt text)"
	}
	return s, nil
}

// firstPromptText extracts a human prompt string from a user line,
// ignoring tool_result-only messages and command metadata.
func firstPromptText(l rawLine) string {
	if l.IsSidechain {
		return ""
	}
	text := messageText(l.Message.Content)
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Skip pure command / caveat noise.
	if strings.HasPrefix(text, "<") && strings.Contains(text, ">") {
		// e.g. <command-name> or <local-command-stdout>: keep only if there's
		// real text after stripping tags.
		text = stripTags(text)
		text = strings.TrimSpace(text)
		if text == "" {
			return ""
		}
	}
	return oneLine(text, 120)
}

// messageText flattens a message Content (string or block array) to plain text.
func messageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string form first.
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---------- Full conversation loading ----------

type Turn struct {
	Role    string // "user", "assistant", "tool"
	Kind    string // "text", "thinking", "tool_use", "tool_result"
	Name    string // tool name for tool_use
	Text    string
	Time    time.Time
	IsError bool
}

// LoadConversation parses a full session file into ordered display turns.
func LoadConversation(path string) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var turns []Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		var l rawLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Message == nil || (l.Type != "user" && l.Type != "assistant") {
			continue
		}
		ts := parseTime(l.Timestamp)
		role := l.Message.Role
		if role == "" {
			role = l.Type
		}
		blocks := decodeBlocks(l.Message.Content)
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				turns = append(turns, Turn{Role: role, Kind: "text", Text: b.Text, Time: ts})
			case "thinking":
				if strings.TrimSpace(b.Thinking) == "" {
					continue
				}
				turns = append(turns, Turn{Role: role, Kind: "thinking", Text: b.Thinking, Time: ts})
			case "tool_use":
				turns = append(turns, Turn{
					Role: role, Kind: "tool_use", Name: b.Name,
					Text: compactJSON(b.Input), Time: ts,
				})
			case "tool_result":
				turns = append(turns, Turn{
					Role: "tool", Kind: "tool_result",
					Text: rawContentText(b.Content), Time: ts, IsError: b.IsError,
				})
			}
		}
	}
	return turns, sc.Err()
}

// decodeBlocks turns a message Content into a slice of blocks. A plain-string
// content becomes a single synthetic text block.
func decodeBlocks(raw json.RawMessage) []contentBlock {
	if len(raw) == 0 {
		return nil
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return []contentBlock{{Type: "text", Text: str}}
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	return blocks
}

// rawContentText flattens a tool_result content (string or block array).
func rawContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// ---------- small string helpers ----------

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, max)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
