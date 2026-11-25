// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeInstall answers the commands the module asks of an installation, in the shape the installation
// answers them: bucket.list, doc.list, doc.text, media.link and the three document actions.
func fakeInstall(t *testing.T, allowWrite *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if allowWrite != nil && !*allowWrite && req.Command != "bucket.list" &&
			req.Command != "doc.list" && req.Command != "doc.text" && req.Command != "media.link" {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "этой установке не разрешено менять документы"})
			return
		}
		var result any
		switch req.Command {
		case "bucket.list":
			result = []Bucket{{ID: "docs", Name: "Документы", Folders: []string{"/data/docs"}}}
		case "doc.list":
			bucket, _ := req.Args["bucket"].(string)
			result = DocList{
				Counts: map[string]int{StateIndexed: 1, StateSkipped: 1},
				Documents: []Doc{
					{Bucket: bucket, Key: "docs/note.md", Path: "/data/docs/note.md", Name: "note.md",
						Folder: "/data/docs", Kind: "text", State: StateIndexed, Size: 120, HasText: true},
					{Bucket: bucket, Key: "docs/old.xls", Path: "/data/docs/old.xls", Name: "old.xls",
						Folder: "/data/docs", Kind: "spreadsheet", State: StateSkipped, Reason: "unsupported"},
				},
				Total: 2, Shown: 2,
			}
		case "doc.text":
			result = DocText{Doc: "docs/note.md", Name: "note.md", Kind: "text", Text: "привет из индекса", HasText: true}
		case "media.link":
			result = map[string]any{"url": "https://relay.example.com/s/abc/api/thumb?mt=tok123", "expires_in": 600}
		case "doc.delete", "doc.restore", "doc.describe", "bucket.scan":
			result = map[string]any{"ok": true, "scanned": 2, "ingested": 1}
		default:
			http.Error(w, "unknown command "+req.Command, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	}))
}

func newTestApp(t *testing.T) (*app, func()) {
	t.Helper()
	srv := fakeInstall(t, nil)
	return &app{install: newInstall(srv.URL)}, srv.Close
}

func get(t *testing.T, h http.Handler, path string, headers map[string]string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	data, _ := io.ReadAll(rec.Body)
	return rec, string(data)
}

// TestPageIsOneDocument is the rule a module's screen lives under: the frame is filled through the
// relay, and a <link> or a <script src> inside it carries no token, so the person sees bare HTML.
func TestPageIsOneDocument(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	_, body := get(t, a.routes(), "/ui/", nil)
	for _, want := range []string{"<style>", "<script>", "AnyText Explorer", "<base href="} {
		if !strings.Contains(body, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	for _, never := range []string{"<script src=", "<link rel=\"stylesheet\"", "<link href="} {
		if strings.Contains(body, never) {
			t.Errorf("the page loads %q from outside: the relay refuses it", never)
		}
	}
}

// TestBaseFollowsTheMount: the page's relative calls must land on the module root, whether it is
// mounted at /api/module/explorer/ or reached on its own port.
func TestBaseFollowsTheMount(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	_, body := get(t, a.routes(), "/ui/", map[string]string{"X-AnyText-Prefix": "/api/module/explorer"})
	if !strings.Contains(body, `<base href="/api/module/explorer/" />`) {
		t.Errorf("the page does not know where it is mounted:\n%s", firstLines(body, 12))
	}
	_, own := get(t, a.routes(), "/ui/", nil)
	if !strings.Contains(own, `<base href="/" />`) {
		t.Errorf("on its own port the module does not serve from the root")
	}
}

// TestStateIsWhatTheScreenNeeds: the collections, a permit for pictures, and who is looking.
func TestStateIsWhatTheScreenNeeds(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	rec, body := get(t, a.routes(), "/api/state", map[string]string{"X-AnyText-Rights": "read,write"})
	if rec.Code != http.StatusOK {
		t.Fatalf("state answered %d", rec.Code)
	}
	for _, want := range []string{`"collections"`, `"Документы"`, `"media"`, `"tok123"`, `"may_write":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("state has no %s:\n%s", want, body)
		}
	}
}

// TestAReaderMayNotChangeAndIsToldSo: the buttons come from the rights, and the refusal comes from the
// same place - a screen that offers what will be refused is a screen that lies.
func TestAReaderMayNotChangeAndIsToldSo(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	_, body := get(t, a.routes(), "/api/state", map[string]string{"X-AnyText-Rights": "read"})
	if !strings.Contains(body, `"may_write":false`) {
		t.Errorf("a reader is shown the right to write:\n%s", body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/exclude",
		strings.NewReader(`{"bucket":"docs","doc":"docs/note.md"}`))
	req.Header.Set("X-AnyText-Rights", "read")
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a reader excluded a document: %d", rec.Code)
	}
}

// TestFilesAndTextTravelThroughTheInstallation: the module has no files of its own, so both answers
// come from /api/command with the person's token.
func TestFilesAndTextTravelThroughTheInstallation(t *testing.T) {
	var token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("Authorization")
		var req struct {
			Command string `json:"command"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Command {
		case "doc.list":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": DocList{
				Counts:    map[string]int{StateIndexed: 1},
				Documents: []Doc{{Key: "docs/note.md", Name: "note.md", State: StateIndexed}},
			}})
		case "doc.text":
			_ = json.NewEncoder(w).Encode(map[string]any{"result": DocText{Text: "из индекса"}})
		}
	}))
	defer srv.Close()
	a := &app{install: newInstall(srv.URL)}
	headers := map[string]string{"Authorization": "Bearer person-token"}

	_, body := get(t, a.routes(), "/api/files?bucket=docs", headers)
	if !strings.Contains(body, "note.md") {
		t.Errorf("files did not come: %s", body)
	}
	if token != "Bearer person-token" {
		t.Errorf("the person's token did not travel: %q", token)
	}
	_, text := get(t, a.routes(), "/api/text?bucket=docs&doc=docs/note.md", headers)
	if !strings.Contains(text, "из индекса") {
		t.Errorf("text did not come: %s", text)
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
