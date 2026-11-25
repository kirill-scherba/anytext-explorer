// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The trace answers one question a terminal program cannot answer from a frozen screen: is the
// program stuck, or has the input stopped arriving? A frame that is never drawn and a key that is
// never read look the same to a person; in the file they are two different lines.
//
// It is a small file in the cache folder, rewritten on every start, and it costs a write per key -
// nothing for a program a person drives by hand.

// tracer writes the trace; a nil tracer writes nothing.
type tracer struct {
	mu   sync.Mutex
	file *os.File
	at   time.Time
}

// openTrace starts the trace. An empty path means "do not trace"; a path that cannot be opened is
// not a reason to refuse to work - the explorer is a file manager first.
func openTrace(path string) *tracer {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	t := &tracer{file: f, at: time.Now()}
	t.line("start version=%s pid=%d", version, os.Getpid())
	return t
}

// line writes one line with the time since the start: a freeze is a gap in the numbering.
func (t *tracer) line(format string, args ...any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	elapsed := time.Since(t.at)
	_, _ = fmt.Fprintf(t.file, "%8.3f %s\n", elapsed.Seconds(), fmt.Sprintf(format, args...))
	_ = t.file.Sync()
}

// close ends the trace.
func (t *tracer) close() {
	if t == nil {
		return
	}
	t.line("exit")
	_ = t.file.Close()
}
