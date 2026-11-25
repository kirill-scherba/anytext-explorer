// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestDumpScreen draws the screen and prints it as text, so that the layout can be looked at without
// a terminal: DUMP=1 go test -run TestDumpScreen -v ./...
//
// It is a tool for whoever works on the screen, not a check: the checks are in ui_test.go.
func TestDumpScreen(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("set DUMP=1 to look at the screen")
	}
	dir := t.TempDir()
	files := map[string]string{
		"заметка.md":          "# Заметка\n\nЭто текст, который видно в файле как он есть.\n",
		"план.txt":            "1. первое\n2. второе\n",
		"прайс.pdf":           "%PDF-1.4 binary",
		"фото.jpg":            "JPEG",
		"запись.opus":         "OggS",
		"документ.docx":       "PK",
		"старый-файл.xls":     "binary",
		"отчёт-за-октябрь.md": "# Отчёт\n\nТекст отчёта.\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "архив"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "входящие"), 0o755); err != nil {
		t.Fatal(err)
	}

	var docs []Doc
	states := map[string]struct {
		state, reason string
		hasText       bool
	}{
		"заметка.md":          {stateIndexed, "", true},
		"план.txt":            {stateIndexed, "", true},
		"прайс.pdf":           {stateIndexed, "", true},
		"фото.jpg":            {stateIndexed, "", true},
		"запись.opus":         {stateSkipped, "audio", false},
		"документ.docx":       {stateSkipped, "no_text", false},
		"старый-файл.xls":     {stateExcluded, "excluded", false},
		"отчёт-за-октябрь.md": {stateUnscanned, "", false},
	}
	for name, st := range states {
		docs = append(docs, Doc{
			Bucket: "тест", Key: "тест/" + name, Path: filepath.Join(dir, name), Name: name,
			Folder: dir, Kind: "text", State: st.state, Reason: st.reason, HasText: st.hasText,
		})
	}

	srv := fakeInstall(t, dir, docs)
	defer srv.Close()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(118, 32)

	a := newApp(newInstall(srv.URL), "ru", dir)
	a.setup(sim)
	if err := a.start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = a.tui.Run() }()
	defer a.tui.QueueUpdateDraw(func() { a.tui.Stop() })

	waitFor(t, sim, a, "в поиске")
	a.tui.QueueUpdateDraw(func() {
		for i, e := range a.rows() {
			if e.Name == "заметка.md" {
				a.table.Select(i+1, 0)
			}
		}
	})
	time.Sleep(150 * time.Millisecond)
	a.tui.QueueUpdateDraw(func() {})
	t.Log("\n" + screenText(sim))
}
