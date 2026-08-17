# claude-code-sessions-resume

A tiny terminal UI (TUI) to **browse your Claude Code sessions and get the exact command to resume any of them.**

It mostly just *shows* you what's there — your projects, every session in them, the full transcript of any session, and the precise `claude --resume …` command to pick that session back up. It never edits or restores anything, and never runs `claude` for you.

The one exception is `d`, which permanently deletes transcripts after you confirm — one session, or every session in a project. That is the only thing here that writes to disk; everything else is read-only.

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

1. **Pick a project** — opens on a list of every folder that has Claude Code sessions (read from `~/.claude/projects`), each with its session count and **how much disk it's using**. Press `s` to re-sort by recent / session count / size / path. Or press `p` to paste/type any folder path directly.
2. **Browse sessions** — for the chosen folder, lists every session with:
   - short session id
   - the first prompt (used as a title)
   - message count, last-used time, and file size — and the project's running total in the status row
   - the exact **resume command** and the directory to run it from
   - press `s` to re-sort by recent / message count / size / title, and `c` to copy the resume command straight to your clipboard.

3. **Read the full conversation** — press `enter` on a session to scroll its entire transcript (in true chronological order): your prompts, Claude's replies, thinking, tool calls, and tool results. The prompt you're currently reading under stays pinned at the top as you scroll. You can `g`/`G` jump to top/bottom, `[`/`]` hop between your prompts, and `/` to search the transcript (`n`/`N` step through matches). The resume command and its `run from:` directory stay visible in the footer here too, so you can read a session and copy its command without backing out to the list.
4. **Choose a resume mode** — press `m` to cycle the shown resume command through every Claude Code permission mode (see below).

The app runs no `claude` commands — it only displays the command for *you* to run. It reads your session files and never modifies them; the single exception is deleting them outright with `d`, which asks first.

### Screen layout

Every screen is stacked the same way, so there's one shape to learn:

```
   Sessions in /Users/mac/dev/claude-sessions-tui        ← where you are

 │ add loading spinner and all resume modes              ← the list, or the transcript
 │ 0f8e2a91 · 185 msgs · 4h ago · 444.3KB

   remember the last selected resume mode across runs
   0f8e2a91 · 148 msgs · 3h ago · 355.5KB

 sort: size · filter: off · 5 sessions · on disk: 1.4MB  ← what's set right now
 resume:  claude --resume 0f8e2a91-… --permission-mode plan
 mode: plan — read-only; plans before making changes
 ↑/↓ move · enter read · c copy · m mode · s sort · / filter · d delete · esc back · q quit
```

The **status row** is always present, including when nothing is set (`filter: off`, `search: off`), so it never blinks in and out and never has to be guessed at. It survives filtering too — the sort stays readable while you're typing, which is exactly when the list is shifting under you.

Each fact appears once: the item count sits with the filter that changes it, the permission mode is explained under the command that carries it, and the key hints appear only in the footer. The footer shortens itself on a narrow terminal rather than truncating mid-word.

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
| Projects      | `↑/↓` move · `/` filter · `s` cycle sort · `enter` open · `p` paste path · `d` delete whole project · `q` quit |
| Paste path    | type/paste a path · `enter` resolve · `esc` back                     |
| Sessions      | `↑/↓` move · `/` filter · `enter` view · `c` copy command · `m` cycle mode · `s` cycle sort · `d` delete · `esc` back · `q` quit |
| Conversation  | `↑/↓ pgup/pgdn` scroll · `g`/`G` top/bottom · `[`/`]` prev/next prompt · `/` search · `n`/`N` next/prev match · `c` copy command · `m` cycle mode · `d` delete · `esc` back · `q` quit |

All keybindings are always shown in the footer of each screen.

**`q` quits the app from any screen**, however deep you are — `esc` is the one that steps back a screen. Both `ctrl+c` and `q` exit from anywhere. While you're typing (a filter, a transcript search, or a path) `q` is just a letter; press `esc` first to leave the input.

---

## Disk usage

Both lists tell you where your disk is going, so you can find what's worth clearing out:

- **Projects** — each row shows its session count and the total size of every transcript in it, and the status row carries the grand total across all of them: `sort: size · filter: off · 12 projects · on disk: 4.2GB`.
- **Sessions** — each row shows its own size, and the status row carries that project's total: `sort: size · filter: off · 24 sessions · on disk: 1.2GB`.

**`s` cycles the sort on both lists** — press it until the sort reads `size` to put the biggest first. Projects sort by `recent · sessions · size · path`; sessions by `recent · messages · size · title`. Both are remembered between runs, independently. Deleting one updates the session list, the status row total, and the project's row on the previous screen straight away — no stale figures left counting space you've just freed.

---

## Deleting

`d` deletes, on every list — and nothing happens until you confirm. The footer turns red and names exactly what is about to go.

**One session** — press `d` on the sessions list, or while reading a transcript:

```
delete 0f8e2a91 "add loading spinner and all resume modes" (444.3KB) permanently? this cannot be undone — y to delete, any other key cancels
```

**A whole project** — press `d` on the projects list to clear out every transcript in it at once. The count leads, since that's the number you'd most regret being wrong about:

```
delete ALL 24 sessions (1.2GB) in /Users/mac/dev/app? this cannot be undone — y to delete, any other key cancels
```

Only `y` goes through. **Every** other key cancels — arrows, `enter`, `esc`, anything — so you cannot destroy anything by mashing keys. The prompt shortens on a narrow terminal but always keeps the two things that matter: that it is irreversible, and which key does what.

This really does remove the `.jsonl` files from `~/.claude/projects/…`. There is no trash, no undo, and no copy anywhere else — `claude --resume` cannot bring back a session whose file is gone.

Both deletes are fenced in:

- a session delete is refused for any path that isn't a `.jsonl` inside `~/.claude/projects`;
- a project delete is refused for anything that isn't a folder **directly** under `~/.claude/projects` — not the root itself, not a nested folder, not somewhere else on disk;
- a project delete only removes `.jsonl` files. Anything else you keep in that folder stays, and the folder itself is only removed if that leaves it empty.

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

Your selected mode **and both sort orders** are **remembered between runs** (saved to `ccsessions/config.json` in your OS config dir, e.g. `~/.config/ccsessions/` on Linux or `~/Library/Application Support/ccsessions/` on macOS), so you don't have to re-pick them every time.

Press `c` on the sessions or conversation screen to copy the resume command to your clipboard. The copied command is prefixed with `cd <working-dir> && …` so it works no matter where you paste it.

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

It only ever **reads** these files, apart from `d`, which deletes the selected session's `.jsonl` — or a whole project's worth — after confirmation. There is no undo and no copy elsewhere: Claude Code cannot resume a session whose file is gone.

---

## License

MIT
