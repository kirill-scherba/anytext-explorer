// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestTextIsShownAsItIs: a file's text is text. A word in brackets is not a colour instruction, and a
// zero byte is a byte - the pane used to mark its own line with a zero byte and replace every one of
// them with a line of dashes, which turned a text file into a megabyte of dashes.
func TestTextIsShownAsItIs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "markup.txt")
	const body = "[red]не красный\x00хвост\nвторая строка\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(100, 24)
	a := newApp(newInstall("http://127.0.0.1:1"), "ru", dir)
	a.setup(sim)

	p := a.previewBody(Entry{Name: "markup.txt", Path: path, Regular: true}, nil, false)
	if p.body != body {
		t.Errorf("the pane changed the text: %q", p.body)
	}
	a.renderPreview(p)
	shown := a.preview.GetText(true)
	if !strings.Contains(shown, "[red]не красный") {
		t.Errorf("the text did not survive the pane:\n%s", shown)
	}
	// Exactly one line of dashes: the rule under the header. A zero byte in the text must not become
	// a rule of its own.
	dashes := 0
	for _, line := range strings.Split(shown, "\n") {
		if len(line) >= 8 && strings.Trim(line, "\u2500") == "" {
			dashes++
		}
	}
	if dashes != 1 {
		t.Errorf("the pane has %d lines of dashes, want the one rule:\n%s", dashes, shown)
	}
}
