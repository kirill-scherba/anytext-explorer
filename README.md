# anytext-explorer — the files of an installation, in a browser

The collections of an AnyText installation with the state of every file in the search — in the search,
skipped and why, excluded, not scanned yet — and a preview that shows a picture, the description a
model wrote, or the text the index holds.

It is a module: a separate process with its own HTTP server on loopback, started and proxied by the
installation at `/api/module/explorer/`. Nothing of it is compiled into AnyText, and the screen reaches
a remote computer through the relay exactly like the window's own pages.

```
person ──► the window or the relay ──► AnyText (proxy /api/module/explorer/)
                                          │
                                          └──► anytext-explorer :7870
                                                 │  /api/command: doc.list, doc.text,
                                                 │  doc.delete, doc.restore, doc.describe, bucket.scan
                                                 └  media.link: the permit for pictures
```

## Why a module and not a program in the terminal

The first version was a full-screen program for the window's terminal (`docs/TERMINAL.md` in
anytext-gui): cheap to build, no distribution, works inside a НУУ container. It was dropped on
25.11.2026, after a day of use:

- **it is not pretty and not convenient.** A terminal cannot show a picture, and a file manager is
  mostly pictures;
- **the frames froze at random** and the reason was never in the explorer: a request of the terminal's
  transport sat unanswered, and the screen stopped while the program kept working. Fixing a transport
  is not the explorer's business;
- **it was a program nobody could install.** A module is installed by the installation's own registry;
  a terminal program has to be in `PATH` of the machine.

The code of that version is in the history (`git log --all`), and the trace file it left
(`~/.cache/anytext-explorer/trace.log`) is what proved the transport was at fault.

## What it needs

- AnyText 0.4.18 or newer: the module gets `ANYTEXT_SELF`, the person's rights, and the account token
  of the frame.
- Nothing else: no second database, no MCP server, no files of its own. The files are the
  installation's, read through `/api/command`; pictures and downloads come from its `/api/thumb` and
  `/api/file` with the media permit.

## Build and install

```bash
CGO_ENABLED=0 go build -o ~/.local/bin/anytext-explorer .

curl -s -X POST http://127.0.0.1:7787/api/command -H 'Content-Type: application/json' -d '{
  "command":"modules.install",
  "args":{"id":"explorer","name":"Проводник","names":{"ru":"Проводник","en":"Files"},
          "version":"0.2.1","addr":"127.0.0.1:7870",
          "command":["'"$HOME"'/.local/bin/anytext-explorer","-addr","127.0.0.1:7870"],
          "auto_start":true,"enabled":true}}'

curl -s http://127.0.0.1:7787/api/module/explorer/health      # {"status":"ok"}
curl -s http://127.0.0.1:7787/api/module/explorer/ui/          # the screen
```

The window shows it under the puzzle button; through the relay it is at
`https://anytext.teonet.app/s/<id>/api/module/explorer/ui/`.

## The screen

```
┌ AnyText Explorer  [todo]                          Обновить  Просканировать ┐
│ поиск…        │ 41 из 41 файлов            имя дата размер состояние  вид  │
│ КОЛЛЕКЦИИ     │ ● в поиске  «В эту папку писать нельзя».md   51 Б  14.11.26 │
│  todo         │ ● в поиске  запрос теплогенератор.pdf       938 КБ 20.11.26 │
│  Галерея      │ ○ пропущено T3_01.11.2026.doc · формат не…  220 КБ 20.11.26 │
│ СОСТОЯНИЕ     │                                                            │
│  ● в поиске 40│                                                            │
│  ○ пропущено 1│                                                            │
│ ПАПКИ         │                                                            │
└───────────────┴────────────────────────────────────────────────────────────┘
```

- **left**: collections, the filter by state (with counts), the folders of the collection, and a search
  by name;
- **middle**: the files — list or grid with thumbnails, sorted by name, date, size or state; a picture
  is shown as a picture;
- **right**: the preview — the picture, what the file is, and the text the index holds. The actions:
  open, download, edit the file (a text file) or the words the search holds, exclude from the search or
  put it back, describe a picture with the model, **rename**, **move**, **delete**.

**Group actions.** A checkbox in every row, Shift for a range, and a checkbox in the toolbar that takes
everything in the current filter. With a selection the bar appears: *Скачать zip*, *Описать моделью*,
*Убрать из поиска*, *Вернуть в поиск*, *Снять*. One request runs the action over the whole selection
and answers what failed, so one unreadable file does not stop the rest.

**Adding files.** *Добавить файлы* in the bar, or drop them onto the list: the module streams them to
the installation's own `/api/upload` — it has no files of its own — and then asks for an incremental
scan, so the new files are in the search when the screen refreshes.

| Mark | State | What it means |
|---|---|---|
| `●` green | в поиске | the search finds it: text, description or transcript is in the index |
| `○` amber | пропущено | the scan looked at it and took nothing; the reason is next to the name |
| `⊘` red | убрано вами | excluded by a person; the file is where it is |
| `·` grey | не сканировано | no scan has reached it yet |

The address understands two choices: `?theme=dark|light` and `?view=list|grid`, and `?doc=<key>` opens
that file — which is also how a person shares what he found.

## The API of the screen

| Request | What it does |
|---|---|
| `GET /api/state` | the collections, the permit for pictures, who is looking and what that person may do |
| `GET /api/files?bucket=` | every file of one collection with its state — one request, then the screen walks the tree it holds |
| `GET /api/text?bucket=&doc=` | the text the search holds: extracted text, description, transcript |
| `GET /api/signed?bucket=&doc=` | a fresh permit for pictures (asked when a picture comes back 401) |
| `POST /api/scan` | index the new and changed files of a collection |
| `POST /api/exclude` / `POST /api/restore` | out of the search / back into it |
| `POST /api/describe` | describe a picture with the vision model |
| `POST /api/bulk` | one action over many documents: `exclude`, `restore`, `describe` |
| `POST /api/zip` | the chosen documents as one archive (a name clash becomes `note (2).md`) |
| `POST /api/upload?bucket=` | multipart files, forwarded to the installation's `/api/upload` |
| `GET /api/raw?bucket=&doc=` | the text of a text file as it is on the disk (the editor shows the file, not the index) |
| `POST /api/save` | `kind: file` writes the file (`doc.save_raw`), any other kind corrects the words the search holds (`doc.save_text`) |
| `POST /api/move` | rename and move as one wish (`file.move`): the target must be a folder of the collection |
| `POST /api/delete` | the document and the file (`doc.delete` with `delete_file`) |
| `GET /health` | the supervisor's question |

Every request carries the person's account token to `/api/command`: the installation decides what that
person may do, and the screen only hides the buttons that would be refused.

## What it does not do yet

- **the whole collection is read at once** (`doc.list` with a high limit). A collection of two hundred
  thousand files answers slowly; a folder-scoped listing in the installation would fix it for the
  screen, `/docs` and the agent at once.

## Tests

```bash
go test ./...
```

The tests use a fake installation and check the three rules that broke before: the page is one
self-contained document, its base follows the mount, and a reader is neither shown nor given the right
to change documents.

## Building for Windows: do not strip this binary

`go build -trimpath -ldflags "-s -w"` makes the Windows build of this module light up Defender's
machine-learning verdict `Trojan:Win32/Bearfoos.A!ml`, and the file is deleted while it is still being
written — an installation reports "не удалось скачать ни по одному адресу" and cannot tell why. Measured
on 26.11.2026 by scanning four builds of the same source: `-trimpath` alone clean, `-s -w` alone clean,
both together flagged. `scripts/publish.py` of matrica-modules-dist builds this module without `-s -w`
(`"strip": false` in `modules.json`); the binary is 15 MB instead of 10, and Defender leaves it alone.
