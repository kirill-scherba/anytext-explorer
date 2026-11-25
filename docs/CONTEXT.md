# CONTEXT: anytext-explorer

The state of the project and the decisions behind it. Newest on top; history is appended, not
rewritten.

## 25.11.2026 — the first slice

**The decision.** A file explorer is made as a terminal program, not as a module. The reasons, in the
order they weighed:

1. The module catalogue does not exist yet (`docs/MODULES.md`, "How a module is added"): a module
   today is a binary a person installs by hand through `/api/command`. A feature that needs that is a
   feature nobody has.
2. The terminal already carries the transport (fetch with the account token, polling), the identity
   and the audit. A module would rebuild the relay path, the `srcdoc` frame and the single-file page
   for the same result.
3. Inside the terminal the program has the trust a shell already has, so file operations need no new
   commands in the installation.
4. Two thirds of the code (tree, keys, index states, wording) moves into a module unchanged if the
   decision is reversed.

**What was built.** One binary, `anytext-explorer`: a two-pane screen with the folder's files, the
state of each file in the search, a preview and the actions of the index. Files come from the disk;
`doc.list`, `doc.text`, `doc.delete`, `doc.restore`, `doc.describe` and `bucket.scan` come from the
installation at `ANYTEXT_SELF` or `127.0.0.1:7787`.

**Checked against the live installation** (25.11.2026, collection `todo`, 40 files in the search):
the folder is listed with its states, the header counts match `doc.list`, the preview reads a text
file from the disk, `F3` reads the indexed text of a document.

**How the screen is tested.** A terminal interface has no screenshot, so tcell's simulation screen
draws the real layout and the test reads the cells back as text (`ui_test.go`,
`TestDumpScreen` with `DUMP=1`). This caught three real defects at once: the cursor landed on "наверх"
instead of the first file, the header was cut in the middle of the path, and the rule of the preview
wrapped onto a second line on a narrow pane.

**Checked on every platform.** The program has no dependency on a pseudo-terminal: it draws on the
terminal it is started in, so it builds for linux, darwin and windows alike. Reaching a *remote*
installation is a different matter, and that is the window's terminal, which exists on linux and
darwin only.

**25.11.2026 — the trace.** Three rounds of a frozen screen were spent on guesses, and the guess
that mattered was outside the program: the window that was freezing was the one served by the site,
and the site's client was the build of 24.11 - every fix lived in the local window only. The lesson is
written into the code: `trace.go` records keys, frames and the duration of every preview, so the next
freeze answers "program or transport" from a file instead of from a conversation.

## Open questions

- **Language of the file names and of the wording.** The screen speaks Russian and English (`-lang`,
  otherwise the locale); the state words come from the interface, the reason codes from the scan.
- **`doc.list` walks the whole collection** on every call, so a folder of a 200 000-file collection is
  slow to open. A folder-scoped listing in the installation would fix it for the explorer, `/docs` and
  the agent at once; until then the explorer passes the folder as the `query` filter.
- **File operations** (rename, copy, move, delete a file, mkdir) are the next slice. They touch the
  disk directly, which is legitimate inside the terminal, and they need confirmations rather than
  permissions: the shell already permits everything.
- **The Windows question.** No terminal there, so no explorer. If that becomes the complaint that
  matters, a module is written on the same code.
