# anytext-explorer — the files of an installation, in a terminal

A file manager drawn in a terminal, for the machine where an AnyText installation runs. It shows the
files of a folder with one thing a file manager usually does not have: **what the search holds of each
file** — in the search, skipped and why, excluded, not scanned yet — and the text that was taken from
it.

```
person ──► the window's terminal (docs/TERMINAL.md) ──► anytext-explorer
                                                          │
                                              the disk ───┘  files, sizes, dates
                                              the installation /api/command:
                                                              doc.list, doc.text,
                                                              doc.delete, doc.restore,
                                                              doc.describe, bucket.scan
```

## Why a terminal program and not a module

Written down on 25.11.2026, when the choice was made.

A module is a process with its own HTTP server, its own port and a single-file page drawn in an
iframe through the relay (`docs/MODULES.md`). For a file manager that is a lot of scaffolding for a
screen: the terminal already carries the transport (fetch with the account token, polling), the
identity and the audit, and the module catalogue does not exist yet — so a module would be a feature
nobody can install.

The terminal program needs none of it: it is one binary, it runs where the installation lives, and
inside the terminal it has exactly the trust the shell already has. It is a first slice, not the last
word: if it turns out that Windows users and phones need the same screen, a module will be written on
top of the same half of the code (tree, keys, index states).

## What it needs

- An AnyText installation on the same machine — the desktop application, or a НУУ container.
- Its address: `ANYTEXT_SELF` from the terminal's environment, `-install`, or `127.0.0.1:7787`.
- No module registry entry, no port, no MCP server, no token. The terminal right (`terminal`) and the
  switch in the settings are what let a person in (`docs/TERMINAL.md`).

## Build and run

```bash
CGO_ENABLED=0 go build -o ~/.local/bin/anytext-explorer .
anytext-explorer                 # the current folder
anytext-explorer -dir ~/Documents/todo
anytext-explorer -lang en        # the screen speaks Russian by default
```

The window's own terminal opens at the home folder of the installation; `anytext-explorer` there is
the whole path to the screen.

## The screen

```
 anytext explorer  /home/kirill/Documents/todo
 коллекция: todo   ● 40 в поиске ○ 1 пропущено ⊘ 0 убрано · 0 не скан.
╔ файлы ══════════════════════════════════╗┌ просмотр ─────────────────────────┐
║  размер имя                             ║│заметка.md                         │
║         ..                              ║│md · 99 Б · изменён 25.11.26 08:01 │
║         Заметки/                        ║│состояние: в поиске                │
║●   99 Б заметка.md                      ║│───────────────────────────────────│
║○    4 Б запись.opus                     ║│# Заметка                          │
║·   39 Б отчёт-за-октябрь.md             ║│                                   │
║⊘    6 Б старый-файл.xls                 ║│Это текст, который видно в файле.  │
╚═════════════════════════════════════════╝└───────────────────────────────────┘
 Enter открыть · BkSp вверх · Tab панель · F3 текст из индекса · F5 обновить
 F7 просканировать · F8 убрать из поиска / вернуть · F9 описать моделью · q выход
```

The marks of the state column, and the same marks with their counts in the header:

| Mark | State | What it means |
|---|---|---|
| `●` | в поиске | the search finds it: text, description or transcript is in the index |
| `○` | пропущено | the scan looked at it and took nothing; the preview says why |
| `⊘` | убрано вами | excluded by a person; the file is where it is |
| `·` | не сканировалось | no scan has reached it yet |
| `·` grey | outside collections | the installation knows nothing about this file |

The pane is a glance: the file itself when it is text, and the line "текст есть только в индексе: F3"
when the text lives in the index (a PDF, a picture, a recording).

## Keys

| Key | What it does |
|---|---|
| `Enter` | enter a folder; on a file — show the text the search holds (F3) |
| `↑` `↓`, `PgUp` `PgDn`, `Home` `End` | move in the list |
| `BkSp`, `Esc` | up to the parent folder; from the text pane — back to the list |
| `Tab` | the keyboard between the list and the text; arrows then scroll the text |
| `F3` | the text from the index: what the search holds, with the description and the transcript |
| `F5` | re-read the folder and the index state |
| `F7` | scan the collection: new and changed files only (asks first) |
| `F8` | exclude from the search / put back |
| `F9` | describe a picture with the vision model |
| `q`, `Ctrl+C` | quit |

## What it does not do yet

- No file operations: rename, copy, move, delete a file. The next slice; inside the terminal the disk
  is reachable, so those are a decision about keys and confirmations, not about the installation.
- No pictures: a terminal shows text. The address of a file in the browser is `/api/file`, and the
  explorer does not open browsers.
- Windows: the program builds and runs there in an ordinary console, but the window has no terminal
  (`creack/pty` has no Windows support), so there is no way to reach a remote installation through it.
  A module would be the answer for remote Windows users, and that is what the next decision is about.
- No mouse: the keys are the interface.

## When the screen stops

A terminal program cannot tell a person whether it is stuck or has stopped hearing the keyboard: a
frame that is never drawn and a key that never arrives look the same. The explorer writes a trace of
both to `~/.cache/anytext-explorer/trace.log` (rewritten on every start, `-trace <path>` to move it):

```
   0.997 key 258        ← the key arrived
   1.022 draw           ← and the screen answered
   1.081 preview t2.txt in 0s
```

Reading it after a freeze:

- **keys with no draw after them** — the program is stuck in rendering;
- **no keys at all while the person was pressing** — the input never reached the program: the
  terminal's transport, not the explorer;
- **`doc.list start` with no `done`** — the installation is thinking, and the list is waiting for it.

## Tests

```bash
go test ./...                        # the screen is drawn on a simulation screen and read back
DUMP=1 go test -run TestDumpScreen -v  # look at the layout as text, without a terminal
```

The first test reads the cells of the drawn screen and checks that the files, the states and the
preview are there — a terminal interface cannot be checked by a screenshot, but it can be read.
