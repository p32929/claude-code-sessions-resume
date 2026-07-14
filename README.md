# ccsessions — Claude Code session browser (TUI)

A read-only terminal UI to browse your Claude Code sessions. It **only shows**
things — it never edits, deletes, or restores anything. For resuming, it just
displays the command you'd run yourself.

## Build & run

```bash
go build -o ccsessions .
./ccsessions
```

## What it does

1. **Pick a project** — opens on a list of every folder that has Claude Code
   sessions (read from `~/.claude/projects`), newest first. Or press `p` to
   paste/type any folder path directly.
2. **Browse sessions** — for the chosen folder, lists every session with its
   short id, first prompt, message count, last-used time and size. The footer
   shows the exact `claude --resume <id>` command and the directory to run it
   from.
3. **View a conversation** — press `enter` on a session to scroll its full
   transcript (your prompts, Claude's replies, thinking, tool calls & results).

## Keys

| Screen        | Keys                                                        |
|---------------|-------------------------------------------------------------|
| Projects      | `↑/↓` move · `/` filter · `enter` open · `p` paste path · `q` quit |
| Paste path    | type/paste · `enter` resolve · `esc` back                   |
| Sessions      | `↑/↓` move · `/` filter · `enter` view · `m` resume mode · `esc`/`q` back |
| Conversation  | `↑/↓ pgup/pgdn` scroll · `m` resume mode · `esc`/`q` back    |

`ctrl+c` quits from anywhere.

## Resume mode

Press `m` on the sessions (or conversation) screen to toggle the resume
command shown between:

- **normal** — `claude --resume <id>`
- **bypass permissions** — `claude --resume <id> --dangerously-skip-permissions`
  (resumes without asking for any permission approvals)

The footer shows the active mode, e.g. `m mode [bypass permissions]`.
