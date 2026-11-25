// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The screen: one header line, a list of files, a preview pane and two lines of keys. The colours
// mean something and nothing else: four states of the index, in four colours, in the list and in the
// header counts alike.
var (
	colorBar    = tcell.NewHexColor(0x1d3a5f)
	colorDim    = tcell.NewHexColor(0x808a97)
	colorOk     = tcell.NewHexColor(0x4caf50)
	colorWarn   = tcell.NewHexColor(0xd7a02b)
	colorBad    = tcell.NewHexColor(0xc0504d)
	colorTitle  = tcell.NewHexColor(0x7fb2e5)
	colorFolder = tcell.NewHexColor(0x5fa8e0)
)

// previewJob is one render of the right pane, waiting for the cursor to settle.
type previewJob struct {
	entry   Entry
	doc     *Doc
	grouped bool
	seq     int
}

// app is the explorer itself: the folder on the disk, the index state the installation reported, and
// the widgets that draw both.
type app struct {
	inst *Install
	lang string
	t    texts

	// dir is the folder shown; entries are its lines; bucket is the collection the folder belongs to
	// (nil outside every collection); state is the installation's opinion of the files in this
	// folder, by absolute path; counts describe the whole collection.
	dir     string
	buckets []Bucket
	entries []Entry
	state   map[string]*Doc
	counts  map[string]int
	bucket  *Bucket

	online  bool
	busy    string
	msgKind int

	// indexSeq and previewSeq drop the answers of requests a person has already navigated away from:
	// a slow doc.list of a folder left behind must not paint over the folder on screen.
	indexSeq   int
	previewSeq int
	// previewWanted is the line the pane should show, and previewSignal wakes the one worker that
	// renders it: a held arrow key asks for ten panes a second, and a goroutine per keypress means
	// ten full redraws queued behind each other - on a relay that reads as a freeze.
	previewMu     sync.Mutex
	previewWanted *previewJob
	previewSignal chan struct{}
	// indexKey is the document whose indexed text is open in the preview pane (F3). Moving the
	// cursor forgets it.
	indexKey string

	tui     *tview.Application
	pages   *tview.Pages
	header  *tview.TextView
	table   *tview.Table
	preview *tview.TextView
	status  *tview.TextView
	help    *tview.TextView
}

// newApp answers the explorer for one installation.
func newApp(inst *Install, lang, dir string) *app {
	if lang != "en" {
		lang = "ru"
	}
	textsFor := ruTexts
	if lang == "en" {
		textsFor = enTexts
	}
	return &app{
		inst: inst, lang: lang, t: textsFor, dir: dir, state: map[string]*Doc{},
		previewSignal: make(chan struct{}, 1),
	}
}

// tr answers one interface string.
func (a *app) tr(key string) string {
	if v, ok := a.t[key]; ok {
		return v
	}
	if v, ok := ruTexts[key]; ok {
		return v
	}
	return key
}

// setup builds the screen. A nil screen means the real terminal; a test passes a simulation screen
// and then draws the same layout a person sees.
func (a *app) setup(screen tcell.Screen) {
	a.tui = tview.NewApplication()
	if screen != nil {
		a.tui.SetScreen(screen)
	}

	a.header = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.header.SetBackgroundColor(colorBar)

	a.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	a.table.SetBorder(true).SetBorderColor(colorBar).SetTitle(" " + a.tr("files") + " ").SetTitleColor(colorTitle).SetTitleAlign(tview.AlignLeft)
	a.table.SetSelectedStyle(tcell.StyleDefault.Background(colorBar).Foreground(tcell.ColorWhite).Bold(true))
	a.table.SetSelectedFunc(func(row, _ int) { a.open(row) })
	a.table.SetSelectionChangedFunc(func(row, _ int) { a.previewRow(row) })

	a.preview = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	a.preview.SetBorder(true).SetBorderColor(colorBar).SetTitle(" " + a.tr("preview") + " ").SetTitleColor(colorTitle).SetTitleAlign(tview.AlignLeft)

	body := tview.NewFlex().
		AddItem(a.table, 0, 6, true).
		AddItem(a.preview, 0, 5, false)

	a.status = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.help = tview.NewTextView().SetDynamicColors(true).SetTextColor(colorDim)
	a.help.SetText(a.helpText())

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 2, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(a.status, 1, 0, false).
		AddItem(a.help, 2, 0, false)

	a.pages = tview.NewPages().AddPage("main", root, true, true)
	a.tui.SetRoot(a.pages, true)
	a.tui.SetInputCapture(a.onKey)
	go a.previewWorker()
}

// run starts the screen and blocks until the person leaves.
func (a *app) run() error {
	a.setup(nil)
	if err := a.start(); err != nil {
		return err
	}
	return a.tui.Run()
}

// start reads the collections and opens the first folder.
func (a *app) start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	buckets, err := a.inst.Buckets(ctx)
	if err != nil {
		// A folder is still worth showing without the installation: the explorer is a file manager
		// first, and the index state is what it adds on top.
		a.online = false
		a.setMessage(msgError, a.tr("offline"))
	} else {
		a.online = true
		a.buckets = buckets
	}
	return a.openFolderWith(a.dir, "")
}

// openFolderWith reads a folder from the disk and asks the installation about its files. selectName
// puts the cursor on that line - coming up from a folder lands on the folder left behind.
func (a *app) openFolderWith(dir, selectName string) error {
	clean := filepath.Clean(dir)
	if st, err := os.Stat(clean); err != nil || !st.IsDir() {
		return fmt.Errorf("%s: не папка", clean)
	}
	entries, err := readDir(clean)
	if err != nil {
		return err
	}
	a.setMessage(msgNone, "")
	a.preview.Clear()
	a.dir = clean
	a.entries = entries
	a.state = map[string]*Doc{}
	a.counts = nil
	a.indexKey = ""
	a.bucket = collectionOf(a.buckets, clean)
	a.drawList()
	a.selectName(selectName)
	a.drawHeader()
	a.refreshIndex()
	return nil
}

// selectName moves the cursor to the entry with this name, or to the first line.
func (a *app) selectName(name string) {
	rows := a.rows()
	if len(rows) == 0 {
		return
	}
	// "…" first, then the first line that is not "наверх": a folder opened has a file under the
	// cursor, and a person does not have to step over the way back.
	row := 1
	if rows[0].Name == ".." {
		row = 2
	}
	for i, e := range rows {
		if name != "" && e.Name == name {
			row = i + 1
			break
		}
	}
	if row > len(rows) {
		row = len(rows)
	}
	a.table.Select(row, 0)
}

// rows is the list as it is drawn: "наверх" first when there is a parent folder.
func (a *app) rows() []Entry {
	out := make([]Entry, 0, len(a.entries)+1)
	if parent := filepath.Dir(a.dir); parent != a.dir {
		out = append(out, Entry{Name: "..", Path: parent, Dir: true})
	}
	return append(out, a.entries...)
}

// selected answers the entry under the cursor.
func (a *app) selected() *Entry {
	rows := a.rows()
	row, _ := a.table.GetSelection()
	idx := row - 1
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

// drawList fills the table: a header row that stays put, then one line per entry.
func (a *app) drawList() {
	a.table.Clear()
	// The column of states holds one mark: the words are in the header counts and in the preview,
	// and a name that fits is worth more than a word repeated on every line.
	for col, name := range []string{"", a.tr("colSize"), a.tr("colName")} {
		cell := tview.NewTableCell(name).SetTextColor(colorDim).SetSelectable(false)
		if col == 1 {
			cell.SetAlign(tview.AlignRight)
		}
		a.table.SetCell(0, col, cell)
	}
	for i, e := range a.rows() {
		a.table.SetCell(i+1, 0, a.stateCell(e))
		a.table.SetCell(i+1, 1, sizeCell(e))
		a.table.SetCell(i+1, 2, nameCell(e))
	}
	if len(a.entries) == 0 {
		a.table.SetCell(len(a.rows())+1, 2, tview.NewTableCell("("+a.tr("emptyFolder")+")").SetTextColor(colorDim))
	}
}

// stateCell is the column a file manager does not have and this one exists for: what the search
// holds of this file. A folder has no state, and neither has a file outside every collection.
func (a *app) stateCell(e Entry) *tview.TableCell {
	cell := tview.NewTableCell("").SetExpansion(0)
	if e.Dir {
		return cell
	}
	doc := a.docOf(e.Path)
	if doc == nil {
		return cell.SetText("·").SetTextColor(colorDim)
	}
	mark, color := "●", colorOk
	switch doc.State {
	case stateSkipped:
		mark, color = "○", colorWarn
	case stateExcluded:
		mark, color = "⊘", colorBad
	case stateUnscanned:
		mark, color = "·", colorDim
	}
	return cell.SetText(mark).SetTextColor(color)
}

// sizeCell writes the size, and a dash for a folder.
func sizeCell(e Entry) *tview.TableCell {
	cell := tview.NewTableCell("").SetAlign(tview.AlignRight).SetExpansion(0).SetTextColor(colorDim)
	if e.Dir {
		return cell
	}
	return cell.SetText(humanSize(e.Size))
}

// nameCell is the name, with the folders in their own colour and a slash, as a shell shows them.
func nameCell(e Entry) *tview.TableCell {
	cell := tview.NewTableCell("").SetExpansion(1)
	if e.Name == ".." {
		return cell.SetText("..").SetTextColor(colorDim)
	}
	if e.Dir {
		return cell.SetText(e.Name + "/").SetTextColor(colorFolder)
	}
	return cell.SetText(e.Name)
}

// docOf answers the installation's record of a file, if it has one.
func (a *app) docOf(path string) *Doc {
	doc, ok := a.state[filepath.Clean(path)]
	if !ok {
		return nil
	}
	return doc
}

// drawHeader draws where we are, which collection owns the folder and what the collection holds.
func (a *app) drawHeader() {
	// Two lines: where we are, then whose collection it is and what the collection holds. One line
	// with all of it is cut in the middle of the path on a screen of eighty columns.
	var b strings.Builder
	b.WriteString(" [::b]" + a.tr("title") + "[-:-:-]  [::b]" + tview.Escape(a.fit(a.dir)) + "[-:-:-]\n ")
	if a.bucket != nil {
		b.WriteString("[::d]" + a.tr("collection") + ":[-:-:-] " + tview.Escape(a.bucket.Name))
	} else {
		b.WriteString("[::d]" + a.tr("noCollection") + "[-:-:-]")
	}
	if a.counts != nil {
		b.WriteString(fmt.Sprintf("   [#4caf50]●[-] %s [#d7a02b]○[-] %s [#c0504d]⊘[-] %s [::d]· %s[-:-:-]",
			a.count(a.counts[stateIndexed], "countIndexed"),
			a.count(a.counts[stateSkipped], "countSkipped"),
			a.count(a.counts[stateExcluded], "countExcluded"),
			a.count(a.counts[stateUnscanned], "countUnscanned")))
	}
	if a.busy != "" {
		b.WriteString("  [#d7a02b]" + tview.Escape(a.busy) + "[-]")
	}
	a.header.SetText(b.String())
}

// fit cuts a path from the left when the screen is narrower than the path: the last folder of a long
// path is what a person is looking at, and it must stay visible.
func (a *app) fit(text string) string {
	// The width of the header is known only after the first draw; before it the path is shown whole
	// and the widget cuts it, which is what a person sees for a fraction of a second.
	width := 0
	if _, _, w, _ := a.header.GetInnerRect(); w > 0 {
		width = w
	}
	// The header shares its line with the title of the program.
	room := width - len(a.tr("title")) - 3
	runes := []rune(text)
	if width <= 0 || len(runes) <= room || room < 8 {
		return text
	}
	return "…" + string(runes[len(runes)-room+1:])
}

// helpText is the line of keys under the screen: navigation and the view above, the actions of the
// index below - two lines fit a screen of eighty columns, one does not.
func (a *app) helpText() string {
	return a.tr("helpMain") + "\n" + a.tr("helpIndex")
}

// count writes one number with the word it counts: "40 в поиске".
func (a *app) count(n int, key string) string {
	return fmt.Sprintf("%d %s", n, a.tr(key))
}

// The kinds of the status line. A line that says "reading the index state" must not wipe the answer
// of a scan that has just finished, and an answer must not outlive the folder a person has left.
const (
	msgNone = iota
	msgBusy
	msgResult
	msgError
)

// setMessage writes the line under the list: what is running, what happened, or what went wrong.
func (a *app) setMessage(kind int, text string) {
	a.msgKind = kind
	if text == "" {
		a.status.SetText("")
		return
	}
	switch kind {
	case msgError:
		a.status.SetText(" [#c0504d]" + tview.Escape(text) + "[-]")
	case msgResult:
		a.status.SetText(" [::b]" + tview.Escape(text) + "[-:-:-]")
	default:
		a.status.SetText(" [::d]" + tview.Escape(text) + "[-:-:-]")
	}
}

// quiet reports whether the status line may be taken by a background request: a result or an error
// of a finished action stays until the person does something.
func (a *app) quiet() bool {
	return a.msgKind == msgNone || a.msgKind == msgBusy
}

// refreshIndex asks the installation about the files of the current folder, in the background: the
// folder itself is already on screen, and doc.list of a large collection takes its time.
func (a *app) refreshIndex() {
	if !a.online || a.bucket == nil {
		a.drawHeader()
		return
	}
	dir, bucketID := a.dir, a.bucket.ID
	a.indexSeq++
	seq := a.indexSeq
	if a.quiet() {
		a.setMessage(msgBusy, a.tr("loading"))
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		list, err := a.inst.Docs(ctx, bucketID, dir, 5000)
		a.tui.QueueUpdateDraw(func() {
			if seq != a.indexSeq {
				return
			}
			if err != nil {
				a.setMessage(msgError, err.Error())
				return
			}
			a.state = docsByPath(list)
			a.counts = list.Counts
			if a.msgKind == msgBusy {
				a.setMessage(msgNone, "")
			}
			a.drawList()
			a.drawHeader()
			a.previewRow(a.selectedRow())
		})
	}()
}

// selectedRow answers the row of the table under the cursor, 0 when there is none.
func (a *app) selectedRow() int {
	row, _ := a.table.GetSelection()
	if row < 1 {
		return 0
	}
	return row
}

// open does what Enter does: a folder is entered, a file is looked at.
func (a *app) open(row int) {
	rows := a.rows()
	idx := row - 1
	if idx < 0 || idx >= len(rows) {
		return
	}
	e := rows[idx]
	if e.Dir {
		left := ""
		if e.Name == ".." {
			left = filepath.Base(a.dir)
		}
		if err := a.openFolderWith(e.Path, left); err != nil {
			a.setMessage(msgError, err.Error())
		}
		return
	}
	a.showIndexedText()
}

// previewRow asks for the pane of the line under the cursor. The work happens in previewWorker: the
// event loop only places the request, so a held key never queues redraws behind itself.
func (a *app) previewRow(row int) {
	rows := a.rows()
	idx := row - 1
	if idx < 0 || idx >= len(rows) {
		// The cursor is between rows (a table refilled under it): the pane keeps what it shows.
		// Blanking it in the middle of a move looks like a freeze of its own.
		return
	}
	e := rows[idx]
	a.indexKey = ""
	a.previewSeq++
	a.previewMu.Lock()
	a.previewWanted = &previewJob{
		entry: e, doc: a.docOf(e.Path), grouped: a.online && a.bucket != nil, seq: a.previewSeq,
	}
	a.previewMu.Unlock()
	select {
	case a.previewSignal <- struct{}{}:
	default:
	}
}

// previewDebounce is how long the worker waits for the cursor to settle: shorter than a key repeat,
// so a held key shows the pane of where it stopped, not of every line it passed.
const previewDebounce = 60 * time.Millisecond

// previewWorker renders the pane once per settled cursor position.
func (a *app) previewWorker() {
	for range a.previewSignal {
		time.Sleep(previewDebounce)
		// Another request came while we waited: it replaces this one, there is nothing to draw twice.
		select {
		case <-a.previewSignal:
		default:
		}
		a.previewMu.Lock()
		job := a.previewWanted
		a.previewWanted = nil
		a.previewMu.Unlock()
		if job == nil {
			continue
		}
		text := a.renderWithTimeout(job)
		a.tui.QueueUpdateDraw(func() {
			if job.seq != a.previewSeq {
				return
			}
			a.renderPreview(text)
		})
	}
}

// pane is what the preview shows: a header made of words the program writes (markup and all) and the
// body, which is a file's text and nothing else.
//
// The body is kept apart on purpose. It is escaped when it is drawn, so a text that holds "[red]" is
// a text that holds "[red]" and not a colour instruction; and the line between the header and the
// text is built from the width of the pane, which is known only when the pane is drawn.
type pane struct {
	head []string
	body string
}

// renderPreview fills the pane: the header, a line as wide as the pane, then the text.
func (a *app) renderPreview(p pane) {
	width := 40
	if _, _, w, _ := a.preview.GetInnerRect(); w > 0 {
		width = w - 1
	}
	lines := append([]string{}, p.head...)
	if p.body != "" {
		lines = append(lines, strings.Repeat("─", width), tview.Escape(p.body))
	}
	a.preview.SetText(strings.Join(lines, "\n"))
	a.preview.ScrollToBeginning()
}

// previewBody is the preview: what the file is, what the installation thinks of it, and as much of
// its text as fits the glance.
func (a *app) previewBody(e Entry, doc *Doc, grouped bool) pane {
	title := e.Name
	if e.Name == ".." {
		title = a.tr("above")
	}
	head := []string{"[::b]" + tview.Escape(title) + "[-:-:-]"}
	if e.Dir {
		head = append(head, "[::d]"+a.tr("folder")+"[-:-:-]")
		switch n := countChildren(e.Path); {
		case n == -childCountLimit:
			head = append(head, "[::d]"+a.tr("inFolder")+": "+fmt.Sprintf(a.tr("itemsMany"), childCountLimit)+"[-:-:-]")
		case n >= 0:
			head = append(head, "[::d]"+a.tr("inFolder")+": "+fmt.Sprintf(a.tr("items"), n)+"[-:-:-]")
		}
		return pane{head: head}
	}
	if !e.Regular {
		// A pipe, a socket, a device: opening one waits for a writer that may never come.
		head = append(head, "[::d]"+a.tr("notRegular")+"[-:-:-]")
		return pane{head: head}
	}
	head = append(head, fmt.Sprintf("[::d]%s[-:-:-] · %s · %s %s",
		tview.Escape(kindOf(e.Path)), humanSize(e.Size), a.tr("metaModified"), e.ModTime.Format("02.01.06 15:04")))
	if doc != nil {
		head = append(head, a.tr("metaState")+": "+stateText(a.tr, doc))
		if doc.Description != "" {
			head = append(head, "[::d]"+a.tr("description")+":[-:-:-] "+tview.Escape(doc.Description))
		}
	} else if grouped {
		head = append(head, "[::d]"+a.tr("outsideKnown")+"[-:-:-]")
	}
	body, hint := a.previewText(e, doc)
	if hint != "" {
		head = append(head, hint)
	}
	return pane{head: head, body: body}
}

// previewText picks the body for the pane: the file itself when it is text, and a line in the header
// explaining where the text lives when it is not.
func (a *app) previewText(e Entry, doc *Doc) (body, hint string) {
	if isText(e.Path) && e.Regular {
		text, err := localText(e.Path, 64<<10)
		if err == nil {
			return clipLines(text, 500), ""
		}
		return "", "[#c0504d]" + tview.Escape(err.Error()) + "[-]"
	}
	if doc != nil && (doc.HasText || doc.HasDescription) {
		return "", "[::d]" + a.tr("inIndex") + "[-:-:-]"
	}
	return "", "[::d]" + a.tr("noPreview") + "[-:-:-]"
}

// showIndexedText puts what the search holds for the file into the preview pane: the extracted text,
// the description of a picture, the transcript of a recording.
func (a *app) showIndexedText() {
	e := a.selected()
	if e == nil || e.Dir {
		return
	}
	doc := a.docOf(e.Path)
	if doc == nil {
		if !a.online || a.bucket == nil {
			a.setMessage(msgError, a.tr("offline"))
			return
		}
		a.setMessage(msgError, a.tr("needCollection"))
		return
	}
	a.indexKey = e.Path
	a.previewSeq++
	seq := a.previewSeq
	a.setMessage(msgBusy, a.tr("asking"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		txt, err := a.inst.Text(ctx, doc.Bucket, doc.Key)
		a.tui.QueueUpdateDraw(func() {
			if seq != a.previewSeq {
				return
			}
			if err != nil {
				a.setMessage(msgError, err.Error())
				return
			}
			a.setMessage(msgNone, "")
			a.renderPreview(indexedBody(a.tr, *e, doc, txt))
		})
	}()
}

// indexedBody is the preview of the indexed text: the same frame the glance has, and the text itself
// under it.
func indexedBody(tr func(string) string, e Entry, doc *Doc, txt *DocText) pane {
	head := []string{
		"[::b]" + tview.Escape(e.Name) + "[-:-:-]",
		"[::d]" + tr("indexText") + " · " + stateText(tr, doc) + "[-:-:-]",
	}
	body := ""
	if txt != nil {
		if txt.Kind != "" {
			head = append(head, "[::d]"+tview.Escape(txt.Kind)+"[-:-:-]")
		}
		if txt.Truncated {
			head = append(head, "[#d7a02b]"+tr("truncated")+"[-]")
		}
		if txt.Description != "" {
			head = append(head, "[::b]"+tr("description")+"[-:-:-]", tview.Escape(txt.Description))
		}
		if txt.Audio != "" {
			head = append(head, "[::b]"+tr("transcript")+"[-:-:-]")
			body = clipLines(txt.Audio, 300)
		}
		if txt.Text != "" {
			body = clipLines(txt.Text, 2000)
		}
		if txt.Text == "" && txt.Description == "" && txt.Audio == "" {
			head = append(head, "[::d]"+tr("noPreview")+"[-:-:-]")
		}
	}
	return pane{head: head, body: body}
}

// previewTimeout is how long a file may take to describe itself. A folder on a network that stopped
// answering is a reason to say so, not a reason to lose the pane until the program is restarted.
const previewTimeout = 3 * time.Second

// renderWithTimeout builds the pane, but never waits longer than previewTimeout. A request that does
// not answer is left behind: its goroutine ends when the read ends, and the pane says what happened.
func (a *app) renderWithTimeout(job *previewJob) pane {
	done := make(chan pane, 1)
	go func() { done <- a.previewBody(job.entry, job.doc, job.grouped) }()
	select {
	case p := <-done:
		return p
	case <-time.After(previewTimeout):
		return pane{head: []string{"[#c0504d]" + tview.Escape(a.tr("slowFile")) + "[-]"}}
	}
}

// ─── actions ────────────────────────────────────────────────────────────────

// reloadDir re-reads the folder and keeps the cursor where a person left it.
func (a *app) reloadDir() {
	var name string
	if e := a.selected(); e != nil && e.Name != ".." {
		name = e.Name
	}
	if err := a.openFolderWith(a.dir, name); err != nil {
		a.setMessage(msgError, err.Error())
	}
}

// scan asks the installation to index the new and changed files of the collection.
func (a *app) scan() {
	bucket := a.bucket
	if bucket == nil {
		a.setMessage(msgError, a.tr("needCollection"))
		return
	}
	a.runLong(a.tr("scanning"), func(ctx context.Context) (string, error) {
		res, err := a.inst.Scan(ctx, bucket.ID, "changed")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(a.tr("scanResult"),
			num(res["scanned"]), num(res["ingested"]), num(res["unchanged"]), num(res["skipped"])), nil
	})
}

// exclude takes the file out of the search and leaves it where it is.
func (a *app) exclude() {
	e := a.selected()
	if e == nil || e.Dir {
		return
	}
	doc := a.docOf(e.Path)
	if doc == nil {
		a.setMessage(msgError, a.tr("needCollection"))
		return
	}
	if doc.State == stateExcluded {
		a.runLong(a.tr("asking"), func(ctx context.Context) (string, error) {
			if err := a.inst.Restore(ctx, doc.Bucket, doc.Key); err != nil {
				return "", err
			}
			return fmt.Sprintf(a.tr("restored"), e.Name), nil
		})
		return
	}
	a.modal(fmt.Sprintf(a.tr("confirmExclude"), tview.Escape(e.Name)), func() {
		a.runLong(a.tr("asking"), func(ctx context.Context) (string, error) {
			if err := a.inst.Exclude(ctx, doc.Bucket, doc.Key); err != nil {
				return "", err
			}
			return fmt.Sprintf(a.tr("excluded"), e.Name), nil
		})
	})
}

// describe asks the vision model what a picture shows and puts the answer into the index.
func (a *app) describe() {
	e := a.selected()
	if e == nil || e.Dir {
		return
	}
	doc := a.docOf(e.Path)
	if doc == nil {
		a.setMessage(msgError, a.tr("needCollection"))
		return
	}
	a.runLong(a.tr("describing"), func(ctx context.Context) (string, error) {
		if _, err := a.inst.Describe(ctx, doc.Bucket, doc.Key); err != nil {
			return "", err
		}
		return fmt.Sprintf(a.tr("described"), e.Name), nil
	})
}

// runLong runs one request in the background and reports how it ended. The screen keeps working:
// a scan of a large collection takes minutes, and a frozen window during it would look like a hang.
func (a *app) runLong(label string, fn func(context.Context) (string, error)) {
	if a.busy != "" {
		return
	}
	a.busy = label
	a.drawHeader()
	a.setMessage(msgBusy, label)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		text, err := fn(ctx)
		a.tui.QueueUpdateDraw(func() {
			a.busy = ""
			a.drawHeader()
			a.reloadDir()
			if err != nil {
				a.setMessage(msgError, err.Error())
			} else {
				a.setMessage(msgResult, text)
			}
		})
	}()
}

// modal asks a question with two buttons: the answer runs yes, or nothing happens.
func (a *app) modal(question string, yes func()) {
	m := tview.NewModal().
		SetText(question).
		AddButtons([]string{a.tr("yes"), a.tr("no")}).
		SetDoneFunc(func(index int, _ string) {
			a.pages.RemovePage("confirm")
			a.tui.SetFocus(a.table)
			if index == 0 {
				yes()
			}
		})
	a.pages.AddPage("confirm", m, false, true)
	a.tui.SetFocus(m)
}

// ─── keys ───────────────────────────────────────────────────────────────────

// onKey is the whole keyboard of the explorer. Keys it does not use go to the widget that has the
// focus: the list moves with the arrows by itself, and the preview scrolls by itself.
func (a *app) onKey(ev *tcell.EventKey) *tcell.EventKey {
	if a.pages.HasPage("confirm") {
		return ev
	}
	switch ev.Key() {
	case tcell.KeyCtrlC:
		a.tui.Stop()
		return nil
	case tcell.KeyF3:
		a.showIndexedText()
		return nil
	case tcell.KeyF5:
		a.reloadDir()
		return nil
	case tcell.KeyF7:
		if a.bucket == nil {
			a.setMessage(msgError, a.tr("needCollection"))
			return nil
		}
		bucket := a.bucket
		a.modal(fmt.Sprintf(a.tr("confirmScan"), tview.Escape(bucket.Name)), a.scan)
		return nil
	case tcell.KeyF8:
		a.exclude()
		return nil
	case tcell.KeyF9:
		a.describe()
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyEscape:
		if a.tui.GetFocus() == a.preview {
			a.toggleFocus()
			return nil
		}
		a.goUp()
		return nil
	case tcell.KeyTab:
		a.toggleFocus()
		return nil
	}
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'q' {
		a.tui.Stop()
		return nil
	}
	return ev
}

// goUp leaves the folder for its parent and puts the cursor back on the folder left behind.
func (a *app) goUp() {
	parent := filepath.Dir(a.dir)
	if parent == a.dir {
		return
	}
	if err := a.openFolderWith(parent, filepath.Base(a.dir)); err != nil {
		a.setMessage(msgError, err.Error())
	}
}

// toggleFocus moves the keyboard between the list and the text: a long text is read with the arrows.
func (a *app) toggleFocus() {
	if a.tui.GetFocus() == a.preview {
		a.tui.SetFocus(a.table)
		a.help.SetText(a.helpText())
		return
	}
	a.tui.SetFocus(a.preview)
	a.help.SetText(a.tr("helpPreview"))
}

// ─── small helpers ──────────────────────────────────────────────────────────

// stateText is the state of a document in words, with the reason of a skip when there is one.
func stateText(tr func(string) string, doc *Doc) string {
	if doc == nil {
		return ""
	}
	label := tr("stateIndexed")
	switch doc.State {
	case stateSkipped:
		label = tr("stateSkipped")
	case stateExcluded:
		label = tr("stateExcluded")
	case stateUnscanned:
		label = tr("stateUnscanned")
	}
	if doc.Reason != "" && doc.State != stateExcluded {
		label += " — " + reasonText(tr, doc.Reason)
	}
	return label
}

// reasonText turns the scan's code into words, as the window does. The wording lives in the
// interface, not in the answer of the installation.
func reasonText(tr func(string) string, reason string) string {
	switch reason {
	case "unsupported":
		return tr("reasonUnsupported")
	case "audio":
		return tr("reasonAudio")
	case "no_text":
		return tr("reasonNoText")
	case "excluded":
		return tr("reasonExcluded")
	}
	return tr("reasonOther")
}

// kindOf names a file by its extension: the same word the window uses next to the file.
func kindOf(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return "файл"
	}
	return ext
}

// countChildren answers how many lines a folder holds; -1 when it cannot be read. It stops after
// childCountLimit names: a folder of two hundred thousand files is counted as "many", not read whole.
const childCountLimit = 1000

func countChildren(dir string) int {
	f, err := os.Open(dir)
	if err != nil {
		return -1
	}
	defer f.Close()
	names, _ := f.Readdirnames(childCountLimit + 1)
	if len(names) > childCountLimit {
		return -childCountLimit
	}
	return len(names)
}

// clipLines cuts a text to a number of lines: the pane is a glance, and a key opens the whole text.
func clipLines(text string, lines int) string {
	if lines <= 0 {
		return text
	}
	parts := strings.SplitN(text, "\n", lines+1)
	if len(parts) <= lines {
		return text
	}
	return strings.Join(parts[:lines], "\n")
}

// num reads a number out of an untyped answer of the installation.
func num(v any) string {
	switch n := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", n)
	case int:
		return fmt.Sprintf("%d", n)
	case nil:
		return "0"
	}
	return fmt.Sprint(v)
}
