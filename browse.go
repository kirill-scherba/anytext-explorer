// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one line of the list: a folder or a file, with what the installation says about it when
// the file lives inside a collection.
type Entry struct {
	Name    string
	Path    string
	Dir     bool
	Size    int64
	ModTime time.Time
	// Regular says the entry is an ordinary file: a pipe, a socket or a device is not read, because
	// opening one can block until a writer appears - and that freezes the pane for good.
	Regular bool
	// Doc is the installation's opinion of this file: in the search, skipped and why, which text was
	// taken. It is nil for a folder, for a file outside every collection, and while the index is
	// still being read.
	Doc *Doc
}

// readDir lists one folder the way a file manager shows it: folders first, then files, each group by
// name and without regard to case.
func readDir(dir string) ([]Entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		path := filepath.Join(dir, it.Name())
		info, err := it.Info()
		if err != nil {
			// A file that vanished between the listing and the stat is not a reason to show an
			// empty folder: the row is kept with the name alone.
			out = append(out, Entry{Name: it.Name(), Path: path, Dir: it.IsDir()})
			continue
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			// A link is shown by what it points at: a link to a folder is worth entering, and a link
			// to nothing is neither a folder nor a file to read.
			if st, err := os.Stat(path); err == nil {
				mode, info = st.Mode(), st
			}
		}
		out = append(out, Entry{
			Name:    it.Name(),
			Path:    path,
			Dir:     mode.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Regular: mode.IsRegular(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// collectionOf answers the collection whose folder holds path. The deepest folder wins: a collection
// added inside another collection's folder would otherwise take files it does not own.
func collectionOf(buckets []Bucket, path string) *Bucket {
	clean := filepath.Clean(path)
	var best *Bucket
	bestLen := -1
	for i := range buckets {
		for _, folder := range buckets[i].Folders {
			f := filepath.Clean(folder)
			if !inside(f, clean) {
				continue
			}
			if len(f) > bestLen {
				best, bestLen = &buckets[i], len(f)
			}
		}
	}
	return best
}

// inside reports whether path is folder itself or lies below it.
func inside(folder, path string) bool {
	if path == folder {
		return true
	}
	return strings.HasPrefix(path, folder+string(filepath.Separator))
}

// docsByPath turns the answer of doc.list into a map the list can ask one file at a time.
func docsByPath(list *DocList) map[string]*Doc {
	out := make(map[string]*Doc, len(list.Documents))
	for i := range list.Documents {
		d := list.Documents[i]
		out[filepath.Clean(d.Path)] = &d
	}
	return out
}

// textKinds are the extensions whose file can be shown as it is. Everything else is either binary
// or has its text in the index only: a PDF's text is extracted, not read from the file.
var textKinds = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true,
	".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".conf": true, ".log": true,
	".go": true, ".js": true, ".ts": true, ".vue": true, ".py": true, ".pl": true, ".sh": true,
	".c": true, ".h": true, ".cpp": true, ".rs": true, ".java": true, ".kt": true, ".sql": true,
	".html": true, ".htm": true, ".css": true, ".xml": true, ".srt": true, ".vtt": true,
}

// isText reports whether the file can be read and shown as text. The name decides, as it does for
// the scan: reading a file to guess its kind costs more than the guess is worth.
func isText(path string) bool {
	return textKinds[strings.ToLower(filepath.Ext(path))]
}

// localText reads the beginning of a text file. The limit is in bytes and generous: the preview is a
// glance at the file, not a pager, and the pager has its own key.
func localText(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// humanSize writes a size the way a list of files wants it: short, three significant characters.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " Б"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	value := float64(n) / float64(div)
	if value < 10 {
		// One decimal for a size below ten: "9.4 МБ" says more than "9 МБ".
		return fmt.Sprintf("%.1f %s", value, units[exp])
	}
	return fmt.Sprintf("%.0f %s", value, units[exp])
}
