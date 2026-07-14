# claude-code-sessions-resume

A tiny, read-only terminal UI (TUI) to **browse your Claude Code sessions and get the exact command to resume any of them.**

It never edits, deletes, or restores anything. It just *shows* you what's there — your projects, every session in them, the full transcript of any session, and the precise `claude --resume …` command to pick that session back up.

---

## Why this exists

Claude Code keeps a full history of every session on disk, but getting back into the *right* one isn't always easy:

- **Claude Code crashed, the terminal closed, or your machine restarted.** Now you have a great session buried somewhere and no obvious way back to it.
- **`claude --resume` shows a picker, but it's per-directory and hard to scan** when you have dozens of sessions — you can't easily see which is which, how long it was, or what it was about.
- **You want to resume in a different mode** than you left in (e.g. jump straight into a plan-only run, or a no-prompts run) and don't remember the flags.
- **You just want to re-read an old conversation** without resuming it at all.

This tool solves all of that: point it at a folder, see every session with its first prompt, size, message count and last-used time, read the whole thing if you want, and copy the resume command it hands you.

---

## What it does

1. **Pick a project** — opens on a list of every folder that has Claude Code sessions (read from `~/.claude/projects`), most recently used first. Or press `p` to paste/type any folder path directly.
2. **Browse sessions** — for the chosen folder, lists every session with:
   - short session id
   - the first prompt (used as a title)
   - message count, last-used time, and file size
   - the exact **resume command** and the directory to run it from
3. **Read the full conversation** — press `enter` on a session to scroll its entire transcript: your prompts, Claude's replies, thinking, tool calls, and tool results.
4. **Choose a resume mode** — press `m` to cycle the shown resume command through every Claude Code permission mode (see below).

Everything is **read-only**. The app runs no `claude` commands and touches none of your session files — it only displays the command for *you* to run.

---

## Install & run

Requires [Go](https://go.dev/dl/) 1.21+.

```bash
git clone https://github.com/p32929/claude-code-sessions-resume.git
cd claude-code-sessions-resume
go build -o ccsessions .
./ccsessions
```

Or use the helper script (builds, then runs):

```bash
./run.sh
```

---

## Keys

| Screen        | Keys                                                                 |
|---------------|---------------------------------------------------------------------|
| Projects      | `↑/↓` move · `/` filter · `enter` open · `p` paste path · `q` quit   |
| Paste path    | type/paste a path · `enter` resolve · `esc` back                     |
| Sessions      | `↑/↓` move · `/` filter · `enter` view · `m` cycle resume mode · `esc`/`q` back |
| Conversation  | `↑/↓ pgup/pgdn` scroll · `m` cycle resume mode · `esc`/`q` back      |

`ctrl+c` quits from anywhere.

---

## Resume modes

Press `m` on the sessions or conversation screen to cycle the resume command through every Claude Code permission mode. The footer shows the active mode and a short hint.

| Mode                 | Command shown                                             |
|----------------------|----------------------------------------------------------|
| normal               | `claude --resume <id>`                                    |
| plan                 | `claude --resume <id> --permission-mode plan`            |
| accept edits         | `claude --resume <id> --permission-mode acceptEdits`     |
| auto                 | `claude --resume <id> --permission-mode auto`            |
| don't ask            | `claude --resume <id> --permission-mode dontAsk`         |
| bypass permissions   | `claude --resume <id> --dangerously-skip-permissions`    |

> **Note:** the mode is a display setting only — the app never runs anything. Copy the command it shows and run it yourself.

Run the resume command **from the session's original working directory** (shown in the `run from:` line), since `claude --resume` is directory-scoped.

---

## Loading indicator

Reading a folder's sessions or a large conversation can take a moment (some sessions have thousands of messages), so a centered spinner shows while that work is in flight — nothing looks frozen.

---

## How it works

Claude Code stores sessions as newline-delimited JSON under:

```
~/.claude/projects/<encoded-folder-path>/<session-id>.jsonl
```

The folder path is encoded by replacing `/` (and a few other characters) with `-`, e.g. `/Users/you/dev/app` → `-Users-you-dev-app`. This tool:

- scans `~/.claude/projects` to list projects,
- reads each `.jsonl` file to pull out metadata and the full conversation,
- and derives the resume command from each file's session id.

It only ever **reads** these files.

---

## License

MIT
