// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPreviewOfAPipeDoesNotBlock is the defect of 25.11.2026: the cursor landed on a named pipe called
// something.txt, the pane opened it for a preview, and opening a pipe waits for a writer that never
// comes. The worker stayed blocked and the pane stopped following the cursor for the rest of the
// session.
func TestPreviewOfAPipeDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	pipe := filepath.Join(dir, "c.txt")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("no named pipes here: %v", err)
	}
	entries, err := readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("readDir found %d entries, want 1", len(entries))
	}
	if entries[0].Regular {
		t.Fatal("a named pipe is not an ordinary file")
	}
	a := newApp(newInstall("http://127.0.0.1:1"), "ru", dir)
	done := make(chan string, 1)
	go func() { done <- a.previewBody(entries[0], nil, false) }()
	select {
	case text := <-done:
		if !strings.Contains(text, a.tr("notRegular")) {
			t.Errorf("the pane does not say why it does not read the pipe:\n%s", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the preview of a named pipe blocked")
	}
}
