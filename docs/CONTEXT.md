# CONTEXT: anytext-explorer

The state of the project and the decisions behind it. Newest on top; history is appended, not
rewritten.

## 25.11.2026 — the module, and the end of the terminal version

**The decision.** The explorer is a module with a browser screen. The terminal version was dropped the
same day it was first used, for three reasons, in the order they weighed:

1. A terminal cannot show a picture, and a file manager is mostly pictures. Kirill, after an hour:
   "TUI всё же не красивый и неудобный".
2. The frames froze at random, and the fault was never in the explorer: the trace proved a frame drawn
   by the program and never delivered by the terminal's transport. A file manager must not depend on a
   transport being fixed first.
3. A program in `PATH` is not a product: a module is installed by the installation's own registry,
   shown under the puzzle button and reached through the relay like any other screen.

**What was built.** `anytext-explorer` 0.2.0, module id `explorer`, port 7870: one binary, one
self-contained page, no Go dependencies. Left — collections, the filter by state with counts, folders,
search; middle — the files in a list or a grid with thumbnails; right — the preview with the picture,
the text the index holds, and the actions (open, download, exclude/restore, describe).

**What it is built on.** The gallery's two lessons: the page is one document served inline (a `<link>`
in a relayed frame carries no token), and pictures come from the installation's `/api/thumb` and
`/api/file` with a media permit in the address, because a browser cannot put a header on an `<img src>`.
The permit is asked once and kept for ten minutes; a picture that comes back 401 asks for a new one.

**What the module insists on.** It has no files of its own: every listing, every text and every action
goes through `/api/command` with the person's account token, so the installation decides what that
person may do. The screen hides the buttons a reader would be refused, and the refusal itself comes
from the same place (test: `TestAReaderMayNotChangeAndIsToldSo`).

**Checked on the live installation** (25.11.2026): 7 collections, `todo` with 41 files and its counts;
the preview of a picture with the OCR text the index holds; the same page at `/api/module/explorer/ui/`
through the installation's proxy; screenshots of both themes and both views.

## 25.11.2026 — group actions and adding files

Kirill after the first look: "доделать недостающие функции и будет отлично! не хватает групповых
действий".

- **Selection**: a checkbox in every row of the list and of the grid, Shift for a range, and a checkbox
  in the toolbar that takes everything in the current filter (with the "some are chosen" state).
- **One request per action, not one per file**: `POST /api/bulk` runs exclude, restore or describe over
  the whole selection and answers `{done, failed[]}` — one unreadable file must not stop the other
  thirty-nine, and a browser must not send forty requests.
- **`POST /api/zip`**: the chosen files as one archive, streamed. Twenty separate "save as" dialogs is
  not a download. A name clash becomes `note (2).md`, an unreadable file becomes `name.error.txt` in the
  archive rather than a lost archive.
- **Adding files**: a button and drag-and-drop; the module streams the multipart body to the
  installation's `/api/upload` and asks for an incremental scan — it still has no files of its own.
- Checked on the live collection: a zip of three keys (with a duplicate) came back as three entries,
  `note.md` and `Account 0.txt` were excluded and restored (`excluded: 2 → 0`, `indexed: 40 → 40`).
  Tests: bulk, the refusal to a reader, the zip and its name clash, the upload through the installation.

## 25.11.2026 — удалить, переместить, править

"Такая простая функция — удалить, переместить, редактировать (как в /docs)".

Двух вещей в установке не было, и одна нашлась готовой:

- **удаление** уже было: `doc.delete` с `delete_file` (кнопка «Удалить файл» в окне документа);
- **правка** тоже: `doc.save_text` правит слова, которые находит поиск (текст, описание, расшифровку),
  а `doc.save_raw` пишет сам текстовый файл. Модуль показывает обе: «править» у каждого раздела текста
  и «Править файл» для `kind: text`;
- **перемещения не было** — добавлено в ядро: `file.move {bucket, doc, folder, name}`, коммит
  `c73e211` в anytext-gui. Целится оно только внутрь папок коллекции, отказывает раньше первого байта
  (вне коллекции, путь в имени, занятое имя, нет папки) и убирает старый адрес из индекса, чтобы скан
  нашёл файл на новом месте.

Экран: у каждого файла в просмотре — «Переименовать» (одно поле), «Переместить» (папка из списка
коллекции плюс имя), «Удалить» (подтверждение словами: файл уходит с диска, это не отмена). После
перемещения и сохранения файла модуль сам просит инкрементальный скан — человек уже сказал, чего хочет.

Проверено на живой установке во временной коллекции `explorer-test`: переименование и перенос в
подпапку (`черновик.txt` → `sub/договор.txt`), отказ на папку вне коллекции, правка текста в индексе,
правка самого файла, удаление (файл исчез, документ ушёл из списка), затем коллекция удалена.

## What is next

- **Upload** from the screen: it belongs to the window's "+" for now, and a second way to put a file
  into a collection is a second set of rules about where it lands.
- **Rename, move, delete**: they need commands the installation does not have. The module has no files
  of its own on purpose.
- **`doc.list` scoped to a folder**: the whole collection is read at once today. One command in the
  installation would make this screen, `/docs` and the agent faster together.
- **The registry catalogue** (`docs/MODULES.md`, "How a module is added"): until it exists, installing a
  module is a `modules.install` by hand.

## The terminal version (25.11.2026, dropped)

Kept here because the reasons matter, not the code — the code is in the history.

It was a tcell/tview program run inside the window's terminal: two panes, the state of every file in
the search, a preview of the local file. It worked and was checked against the live installation, and
its test read the drawn screen back as text. Two things ended it: a file manager without pictures, and
a transport whose frozen request took the screen with it.

What the exercise proved and is worth remembering:

- the trace file (`~/.cache/anytext-explorer/trace.log`) answers the question a frozen terminal cannot:
  keys without frames is the program, no keys at all is the transport;
- the terminal's poll had no timeout: a request the relay dropped silently was waited on for minutes,
  and the input queue sent one POST per keystroke, which is a queue of overtaking requests for a person
  holding an arrow key. Both are fixed in `anytext-gui` today and matter for `vim` and `mc` there.

### 25.11.2026, продолжение — что показал экран

- **Карточка без картинки** показывала пустой квадрат: эмодзи 📄 не на всех шрифтах есть, а пустой
  квадрат хуже слова. Теперь в углу карточки расширение: `MD`, `PDF`, `DOCX`, `WEB`.
- **Ссылка на страницу с адресом, кончающимся на `.png`**, считалась картинкой и давала рваную иконку:
  у ссылки нет своих байтов. Теперь `isImage` ссылку не пропускает, а картинка, которая не загрузилась
  и после нового разрешения, заменяется на расширение вместо рваной иконки.
