// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// app is the module: one installation, one permit shared by the screen, and the person's token for
// every request that reaches the files.
type app struct {
	install *Install
	permits permits
}

// routes is the whole API of the screen. Paths are relative to the module: the installation mounts it
// at /api/module/explorer/, and the page's <base> makes "api/files" mean the same at the machine, in
// the relayed window and on the module's own port.
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /api/state", a.state)
	mux.HandleFunc("GET /api/files", a.files)
	mux.HandleFunc("GET /api/text", a.text)
	mux.HandleFunc("GET /api/signed", a.signed)
	mux.HandleFunc("POST /api/scan", a.scan)
	mux.HandleFunc("POST /api/exclude", a.exclude)
	mux.HandleFunc("POST /api/restore", a.restore)
	mux.HandleFunc("POST /api/describe", a.describe)
	// The screen itself: /ui/ is where the installation points the puzzle button.
	mux.HandleFunc("GET /ui/", a.page)
	mux.HandleFunc("GET /", a.page)
	return mux
}

// person is who is asking: the installation puts it into headers, and only the installation can reach
// this port.
type person struct {
	Email    string   `json:"email"`
	Rights   []string `json:"rights"`
	MayWrite bool     `json:"may_write"`
}

// who answers the person of one request. A read-only person is shown the screen without the buttons
// that would only be refused later.
func who(r *http.Request) person {
	rights := strings.TrimSpace(r.Header.Get("X-AnyText-Rights"))
	p := person{Email: r.Header.Get("X-AnyText-Person")}
	if rights != "" {
		p.Rights = strings.Split(rights, ",")
	}
	// Nobody at the machine means the owner, and the owner may do everything.
	p.MayWrite = rights == "" || strings.Contains(rights, "write")
	return p
}

// token is the account token the screen's requests carry: the installation hands it to the frame, and
// the module passes it on so that the person's rights travel with every command.
func token(r *http.Request) string {
	t := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(t) > 7 && strings.EqualFold(t[:7], "bearer ") {
		return strings.TrimSpace(t[7:])
	}
	return ""
}

// health answers the installation's supervisor: a module that does not answer is not shown.
func (a *app) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version})
}

// state is what the screen needs before it draws anything: the collections, the permit for pictures,
// who is looking and what that person may do.
func (a *app) state(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	collections, err := a.install.Collections(ctx, token(r))
	if err != nil {
		writeError(w, err)
		return
	}
	// The permit is asked for with the first collection's first file: the installation wants to know
	// that the person asking for a picture can see something at all.
	media := Permission{}
	if doc := firstFile(ctx, a.install, token(r), collections); doc != "" {
		media, _ = a.install.Permit(ctx, token(r), collections[0].ID, doc, &a.permits, 400)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     version,
		"collections": collections,
		"media":       media,
		"person":      who(r),
	})
}

// firstFile answers the key of some file of the first collection that has one: a permit needs a
// document, and any document of the collection will do.
func firstFile(ctx context.Context, in *Install, token string, collections []Bucket) string {
	for _, b := range collections {
		list, err := in.Files(ctx, token, b.ID)
		if err != nil || len(list.Documents) == 0 {
			continue
		}
		return list.Documents[0].Key
	}
	return ""
}

// files is every file of one collection with its state: the screen builds the folder tree itself, so
// a click in the tree costs no request.
func (a *app) files(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужна коллекция"})
		return
	}
	list, err := a.install.Files(r.Context(), token(r), bucket)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// text is what the search holds of one document: the extracted text, the description of a picture,
// the transcript of a recording.
func (a *app) text(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	doc := q.Get("doc")
	if doc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужен документ"})
		return
	}
	txt, err := a.install.Text(r.Context(), token(r), q.Get("bucket"), doc)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, txt)
}

// signed answers a fresh permit: the screen asks for one when a picture that used to load comes back
// 401, which is what an expired permit looks like.
func (a *app) signed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a.permits.mu.Lock()
	a.permits.token = ""
	a.permits.mu.Unlock()
	media, ok := a.install.Permit(r.Context(), token(r), q.Get("bucket"), q.Get("doc"), &a.permits, 400)
	if !ok {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "разрешение на картинки не получено"})
		return
	}
	writeJSON(w, http.StatusOK, media)
}

// scan indexes the new and changed files of a collection.
func (a *app) scan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket string `json:"bucket"`
		Mode   string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Bucket == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужна коллекция"})
		return
	}
	// A scan of a large collection takes minutes: the screen is told when it is done, and nothing
	// waits for it here.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Minute)
	defer cancel()
	res, err := a.install.Scan(ctx, token(r), req.Bucket, req.Mode)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// docAction is the shape of the three actions that name one document.
type docAction struct {
	Bucket string `json:"bucket"`
	Doc    string `json:"doc"`
}

func readAction(w http.ResponseWriter, r *http.Request) (docAction, bool) {
	var req docAction
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужен документ"})
		return req, false
	}
	if !who(r).MayWrite {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "этой установке не разрешено менять документы"})
		return req, false
	}
	return req, true
}

// exclude takes a document out of the search and keeps the file where it is.
func (a *app) exclude(w http.ResponseWriter, r *http.Request) {
	req, ok := readAction(w, r)
	if !ok {
		return
	}
	if err := a.install.Exclude(r.Context(), token(r), req.Bucket, req.Doc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// restore puts an excluded document back into the search.
func (a *app) restore(w http.ResponseWriter, r *http.Request) {
	req, ok := readAction(w, r)
	if !ok {
		return
	}
	if err := a.install.Restore(r.Context(), token(r), req.Bucket, req.Doc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// describe asks the vision model what a picture shows.
func (a *app) describe(w http.ResponseWriter, r *http.Request) {
	req, ok := readAction(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	text, err := a.install.Describe(ctx, token(r), req.Bucket, req.Doc)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": text})
}

// writeJSON answers one JSON object.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers a refusal in words a person can read: the installation explains what went wrong
// better than a status code does.
func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
}
