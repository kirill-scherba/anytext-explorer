// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// fakeInstall answers the two commands the explorer needs, in the shape the installation answers
// them: bucket.list and doc.list. Everything else is a test's business, not the screen's.
func fakeInstall(t *testing.T, folder string, docs []Doc) *httptest.Server {
	t.Helper()
	buckets := []Bucket{{ID: "тест", Name: "Тест", Folders: []string{folder}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		query, _ := req.Args["query"].(string)
		var result any
		switch req.Command {
		case "app.whoami":
			result = map[string]any{"role": "owner"}
		case "bucket.list":
			result = buckets
		case "doc.list":
			// The installation filters by a substring of the path, and so does the fake.
			out := DocList{Documents: []Doc{}, Counts: map[string]int{}}
			for _, d := range docs {
				if query != "" && !strings.Contains(strings.ToLower(d.Path), strings.ToLower(query)) {
					continue
				}
				out.Documents = append(out.Documents, d)
				out.Counts[d.State]++
			}
			out.Total, out.Shown = len(out.Documents), len(out.Documents)
			result = out
		case "doc.text":
			result = DocText{Doc: "тест/заметка.md", Kind: "text", Text: "# Заметка\nпривет из индекса"}
		default:
			http.Error(w, "unknown command", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	}))
}

// screenText is what a person would read on the terminal: the cells as lines of text.
func screenText(sim tcell.SimulationScreen) string {
	cells, w, h := sim.GetContents()
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 {
				b.WriteRune(' ')
				continue
			}
			b.WriteString(string(c.Runes))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// waitFor draws until the screen says what the test waits for: the explorer loads the index in the
// background, and a test that reads the screen at once reads it half-drawn.
func waitFor(t *testing.T, sim tcell.SimulationScreen, a *app, want string) string {
	t.Helper()
	var last string
	for i := 0; i < 100; i++ {
		// A queued update is a barrier: it runs after everything queued before it, and blocks until
		// the event loop has done so.
		a.tui.QueueUpdateDraw(func() {})
		last = screenText(sim)
		if strings.Contains(last, want) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the screen never showed %q; it shows:\n%s", want, last)
	return last
}

// TestScreenShowsFilesAndIndexState is the one test that matters for a terminal interface: the
// layout is drawn, the files are there, and the state of the index is next to them.
func TestScreenShowsFilesAndIndexState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "заметка.md"), []byte("# Заметка\nпривет\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "фото.jpg"), []byte("JPEG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "архив"), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := fakeInstall(t, dir, []Doc{
		{Bucket: "тест", Key: "тест/заметка.md", Path: filepath.Join(dir, "заметка.md"),
			Name: "заметка.md", Folder: dir, Kind: "text", State: stateIndexed, HasText: true},
		{Bucket: "тест", Key: "тест/фото.jpg", Path: filepath.Join(dir, "фото.jpg"),
			Name: "фото.jpg", Folder: dir, Kind: "image", State: stateSkipped, Reason: "unsupported"},
	})
	defer srv.Close()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(110, 30)

	a := newApp(newInstall(srv.URL), "ru", dir)
	a.setup(sim)
	if err := a.start(); err != nil {
		t.Fatal(err)
	}
	// The event loop is what draws: the same run a person's terminal has, only on a screen of cells.
	go func() { _ = a.tui.Run() }()
	defer a.tui.QueueUpdateDraw(func() { a.tui.Stop() })

	// The files are on screen before the installation answers: wait for the state, then put the
	// cursor on the file whose preview the test reads.
	waitFor(t, sim, a, "в поиске")
	a.tui.QueueUpdateDraw(func() {
		for i, e := range a.rows() {
			if e.Name == "заметка.md" {
				a.table.Select(i+1, 0)
			}
		}
	})
	text := waitFor(t, sim, a, "Заметка")
	for _, want := range []string{
		"anytext explorer",
		"заметка.md",
		"фото.jpg",
		"архив/",
		"в поиске",  // the state of заметка.md
		"пропущено", // the state of фото.jpg
		"коллекция", // the header says which collection owns the folder
		"Тест",      // and its name
		"Заметка",   // the preview of the selected file, read from the disk
		"F3 текст",  // the keys are on screen
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the screen does not show %q:\n%s", want, text)
		}
	}
}

// TestShowIndexedTextReadsTheInstallation checks the one thing the disk cannot answer: what text the
// search holds for a file.
func TestShowIndexedTextReadsTheInstallation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "заметка.md")
	if err := os.WriteFile(path, []byte("# Заметка\nпривет\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeInstall(t, dir, []Doc{{
		Bucket: "тест", Key: "тест/заметка.md", Path: path, Name: "заметка.md",
		Folder: dir, Kind: "text", State: stateIndexed, HasText: true,
	}})
	defer srv.Close()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(100, 24)

	a := newApp(newInstall(srv.URL), "ru", dir)
	a.setup(sim)
	if err := a.start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = a.tui.Run() }()
	defer a.tui.QueueUpdateDraw(func() { a.tui.Stop() })

	waitFor(t, sim, a, "заметка.md")
	a.tui.QueueUpdateDraw(a.showIndexedText)
	text := waitFor(t, sim, a, "привет из индекса")
	if !strings.Contains(text, "текст из индекса") {
		t.Errorf("the pane does not say where the text came from:\n%s", text)
	}
}

// TestReadDirPutsFoldersFirst keeps the order a person expects from a file manager.
func TestReadDirPutsFoldersFirst(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"б.txt", "А.txt", "я.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "папка"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name)
	}
	want := []string{"папка", "А.txt", "б.txt", "я.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("readDir order %v, want %v", got, want)
	}
}

// TestCollectionOfTakesTheDeepestFolder: a collection added inside another one's folder must not
// take the files of the collection that owns them.
func TestCollectionOfTakesTheDeepestFolder(t *testing.T) {
	buckets := []Bucket{
		{ID: "outer", Folders: []string{"/data/docs"}},
		{ID: "inner", Folders: []string{"/data/docs/private"}},
	}
	if b := collectionOf(buckets, "/data/docs/report.txt"); b == nil || b.ID != "outer" {
		t.Errorf("a file in the outer folder belongs to %v, want outer", b)
	}
	if b := collectionOf(buckets, "/data/docs/private/secret.txt"); b == nil || b.ID != "inner" {
		t.Errorf("a file in the inner folder belongs to %v, want inner", b)
	}
	if b := collectionOf(buckets, "/data/elsewhere/report.txt"); b != nil {
		t.Errorf("a file outside every collection belongs to %v, want nothing", b)
	}
}

// TestHumanSize keeps the column narrow: three characters and a unit.
func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:           "0 Б",
		999:         "999 Б",
		1024:        "1.0 КБ",
		9_400:       "9.2 КБ",
		1_500_000:   "1.4 МБ",
		182_000_000: "174 МБ",
	}
	for size, want := range cases {
		if got := humanSize(size); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", size, got, want)
		}
	}
}
