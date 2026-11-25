// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
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
	mux.HandleFunc("POST /api/bulk", a.bulk)
	mux.HandleFunc("POST /api/zip", a.zip)
	mux.HandleFunc("POST /api/upload", a.upload)
	mux.HandleFunc("POST /api/move", a.move)
	mux.HandleFunc("POST /api/delete", a.delete)
	mux.HandleFunc("POST /api/save", a.save)
	mux.HandleFunc("GET /api/raw", a.raw)
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

// bulk runs one action over many documents: a person who marked forty files should not be made to
// click forty times, and a browser should not send forty requests.
func (a *app) bulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string   `json:"action"`
		Bucket string   `json:"bucket"`
		Docs   []string `json:"docs"`
		Folder string   `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Docs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужны документы"})
		return
	}
	if !who(r).MayWrite {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "этой установке не разрешено менять документы"})
		return
	}
	// A bulk of pictures described by a model takes minutes: the request is answered when it is done,
	// and nothing here gives up on it first.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Minute)
	defer cancel()
	token := token(r)

	done, failed := 0, []map[string]string{}
	for _, doc := range req.Docs {
		var err error
		switch req.Action {
		case "exclude":
			err = a.install.Exclude(ctx, token, req.Bucket, doc)
		case "restore":
			err = a.install.Restore(ctx, token, req.Bucket, doc)
		case "describe":
			_, err = a.install.Describe(ctx, token, req.Bucket, doc)
		case "move":
			// A group move keeps the names: forty files renamed to one name is not a wish anyone has.
			if strings.TrimSpace(req.Folder) == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужна папка"})
				return
			}
			_, err = a.install.Move(ctx, token, req.Bucket, doc, req.Folder, "")
		case "delete":
			err = a.install.Delete(ctx, token, req.Bucket, doc)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неизвестное действие: " + req.Action})
			return
		}
		if err != nil {
			failed = append(failed, map[string]string{"doc": doc, "error": err.Error()})
			continue
		}
		done++
	}
	writeJSON(w, http.StatusOK, map[string]any{"done": done, "failed": failed})
}

// zip answers the chosen documents as one archive. A person marking twenty files wants them together,
// and twenty separate dialogs of "save as" is not a download, it is a chore.
func (a *app) zip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket string   `json:"bucket"`
		Docs   []string `json:"docs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Docs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужны документы"})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Minute)
	defer cancel()
	token := token(r)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="anytext-`+req.Bucket+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	zw := zip.NewWriter(w)
	defer zw.Close()

	used := map[string]int{}
	for _, doc := range req.Docs {
		body, err := a.install.File(ctx, token, req.Bucket, doc)
		if err != nil {
			// One unreadable file must not lose the archive: it is listed in the archive as a note.
			entry, _ := zw.Create(zipName(doc, used) + ".error.txt")
			_, _ = io.WriteString(entry, err.Error())
			continue
		}
		entry, err := zw.Create(zipName(doc, used))
		if err != nil {
			body.Close()
			return
		}
		_, _ = io.Copy(entry, body)
		body.Close()
	}
}

// zipName answers the name of one document inside the archive: the file's own name, and a counter when
// two folders hold a file with the same name.
func zipName(key string, used map[string]int) string {
	name := filepath.Base(key)
	if name == "." || name == "/" || name == "" {
		name = "file"
	}
	used[name]++
	if n := used[name]; n > 1 {
		ext := filepath.Ext(name)
		name = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(n) + ")" + ext
	}
	return name
}

// upload puts files into a collection through the installation's own endpoint. The module is not a
// second way to write files: it asks the installation, which knows where the collection keeps them.
func (a *app) upload(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужна коллекция"})
		return
	}
	if !who(r).MayWrite {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "этой установке не разрешено добавлять файлы"})
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Minute)
	defer cancel()
	token := token(r)

	saved, failed := []string{}, []map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		if err := a.install.Upload(ctx, token, bucket, name, part); err != nil {
			failed = append(failed, map[string]string{"name": name, "error": err.Error()})
		} else {
			saved = append(saved, name)
		}
		part.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": saved, "failed": failed})
}

// move renames or moves the file of one document. The folder must be one of the collection's: a file
// that leaves them is a file the index no longer owns.
func (a *app) move(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket string `json:"bucket"`
		Doc    string `json:"doc"`
		Folder string `json:"folder"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужен документ"})
		return
	}
	if !who(r).MayWrite {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "этой установке не разрешено менять документы"})
		return
	}
	res, err := a.install.Move(r.Context(), token(r), req.Bucket, req.Doc, req.Folder, req.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// delete removes the file itself, with the document. The screen asks first; this is the door.
func (a *app) delete(w http.ResponseWriter, r *http.Request) {
	req, ok := readAction(w, r)
	if !ok {
		return
	}
	if err := a.install.Delete(r.Context(), token(r), req.Bucket, req.Doc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// save keeps a person's words: the correction of a text the search holds (kind: text, description,
// audio), or the text file itself (kind: file).
func (a *app) save(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bucket string `json:"bucket"`
		Doc    string `json:"doc"`
		Kind   string `json:"kind"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужен документ"})
		return
	}
	if !who(r).MayWrite {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "этой установке не разрешено менять документы"})
		return
	}
	var err error
	if req.Kind == "file" {
		err = a.install.SaveRaw(r.Context(), token(r), req.Bucket, req.Doc, req.Text)
	} else {
		err = a.install.SaveText(r.Context(), token(r), req.Bucket, req.Doc, req.Kind, req.Text)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// raw answers the text of a text file as it is on the disk: the editor shows the file, not the index.
func (a *app) raw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	doc := q.Get("doc")
	if doc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "нужен документ"})
		return
	}
	body, err := a.install.File(r.Context(), token(r), q.Get("bucket"), doc)
	if err != nil {
		writeError(w, err)
		return
	}
	defer body.Close()
	// A text file, not an archive: an editor that reads four megabytes of anything is a browser tab
	// nobody asked for.
	data, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": string(data)})
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
