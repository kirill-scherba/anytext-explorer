// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Install is the AnyText installation this module works for. Everything the screen shows comes from
// here: the collections, the files and their state, the text the search holds of them.
type Install struct {
	base   string
	client *http.Client
}

// newInstall answers a client for the installation at base.
func newInstall(base string) *Install {
	return &Install{
		base: strings.TrimRight(strings.TrimSpace(base), "/"),
		// A collection of two hundred thousand files answers doc.list slowly, and a scan answers when
		// it is done: a short timeout would report a working installation as dead.
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Bucket is a collection of the installation with the folders it indexes.
type Bucket struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Folders     []string `json:"folders"`
	Builtin     string   `json:"builtin"`
}

// Doc is one file as the installation reports it (doc.list).
type Doc struct {
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Folder         string `json:"folder"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	Reason         string `json:"reason,omitempty"`
	Size           int64  `json:"size,omitempty"`
	ModTime        string `json:"mod_time,omitempty"`
	Description    string `json:"description,omitempty"`
	HasText        bool   `json:"has_text,omitempty"`
	HasDescription bool   `json:"has_description,omitempty"`
	HasAudio       bool   `json:"has_audio,omitempty"`
}

// DocList is the answer of doc.list.
type DocList struct {
	Counts    map[string]int `json:"counts"`
	Documents []Doc          `json:"documents"`
	Total     int            `json:"total"`
	Shown     int            `json:"shown"`
}

// DocText is the answer of doc.text: the texts the search holds for one document.
type DocText struct {
	Doc            string `json:"doc"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Text           string `json:"text"`
	Description    string `json:"description"`
	Audio          string `json:"audio"`
	HasText        bool   `json:"has_text"`
	HasDescription bool   `json:"has_description"`
	HasAudio       bool   `json:"has_audio"`
	Truncated      bool   `json:"truncated"`
	Edited         bool   `json:"edited"`
	File           string `json:"file"`
	Folder         string `json:"folder"`
}

// Document states, as doc.list names them.
const (
	StateIndexed   = "indexed"
	StateSkipped   = "skipped"
	StateExcluded  = "excluded"
	StateUnscanned = "unscanned"
)

// command posts one command on behalf of a person and decodes its result into out.
//
// The person's account token travels with the request: the installation decides what that person may
// do, and a module that dropped the token would work as the owner for everybody
// (docs/MODULES.md, "What a module sees").
func (in *Install) command(ctx context.Context, token, cmd string, args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"command": cmd, "args": args})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.base+"/api/command", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := in.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", in.base, err)
	}
	defer resp.Body.Close()
	// Forty thousand characters of a document, times four buffers: the answer to doc.text is large by
	// design and must not be cut at a megabyte.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	var answer struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return fmt.Errorf("%s: %s", cmd, snippet(raw))
	}
	if answer.Error != "" {
		return errors.New(answer.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", cmd, http.StatusText(resp.StatusCode))
	}
	if out == nil || len(answer.Result) == 0 || string(answer.Result) == "null" {
		return nil
	}
	return json.Unmarshal(answer.Result, out)
}

// Collections lists the collections of the installation.
func (in *Install) Collections(ctx context.Context, token string) ([]Bucket, error) {
	var out []Bucket
	if err := in.command(ctx, token, "bucket.list", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Files answers every file of one collection with its state. One request per collection: the screen
// then walks the tree it already holds, and a click costs nothing.
func (in *Install) Files(ctx context.Context, token, bucket string) (*DocList, error) {
	var out DocList
	if err := in.command(ctx, token, "doc.list", map[string]any{
		"bucket": bucket, "limit": 200000,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Text answers what the search holds for one document.
func (in *Install) Text(ctx context.Context, token, bucket, doc string) (*DocText, error) {
	var out DocText
	if err := in.command(ctx, token, "doc.text", map[string]any{"bucket": bucket, "doc": doc}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Exclude takes a document out of the search and keeps the file where it is.
func (in *Install) Exclude(ctx context.Context, token, bucket, doc string) error {
	return in.command(ctx, token, "doc.delete", map[string]any{
		"bucket": bucket, "doc": doc, "delete_file": false,
	}, nil)
}

// Restore puts an excluded document back into the search.
func (in *Install) Restore(ctx context.Context, token, bucket, doc string) error {
	return in.command(ctx, token, "doc.restore", map[string]any{"bucket": bucket, "doc": doc}, nil)
}

// Describe asks the vision model what a picture shows.
func (in *Install) Describe(ctx context.Context, token, bucket, doc string) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := in.command(ctx, token, "doc.describe", map[string]any{"bucket": bucket, "doc": doc}, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// Scan brings the collection's index up to date. "changed" reads only what the scan has not seen or
// what has been rewritten since; "all" rebuilds the collection.
func (in *Install) Scan(ctx context.Context, token, bucket, mode string) (map[string]any, error) {
	if mode == "" {
		mode = "changed"
	}
	var out map[string]any
	if err := in.command(ctx, token, "bucket.scan", map[string]any{"bucket": bucket, "mode": mode}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// File answers the bytes of one document: the installation's own /api/file, asked with the person's
// token. The module never reads a disk, so a download is a copy of a copy.
func (in *Install) File(ctx context.Context, token, bucket, doc string) (io.ReadCloser, error) {
	u := in.base + "/api/file?" + url.Values{"bucket": {bucket}, "doc": {doc}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := in.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("%s: %s", doc, snippet(raw))
	}
	return resp.Body, nil
}

// Upload puts one file into a collection through the installation's own endpoint, so the file lands
// where the collection expects it and not where this program guesses. The body is streamed: a file of
// two hundred megabytes must not be kept in memory twice.
func (in *Install) Upload(ctx context.Context, token, bucket, name string, data io.Reader) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		_ = mw.WriteField("bucket", bucket)
		part, err := mw.CreateFormFile("file", name)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, data); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = mw.Close()
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.base+"/api/upload", pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := in.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var answer struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	if answer.Error != "" {
		return errors.New(answer.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload: %s", snippet(raw))
	}
	return nil
}

// Permission is an "<img src>" address needs a permit that the browser can carry in the query: a
// browser cannot put an Authorization header on a picture, and the relay refuses an address without
// one (anytext-gallery, 23.11.2026). One permit serves the whole screen for ten minutes.
type Permission struct {
	Base      string `json:"base"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// permits keeps the permit of this installation, shared by every request the module answers.
type permits struct {
	mu    sync.Mutex
	base  string
	token string
	until time.Time
}

// Permit answers the address pictures and downloads live under, and a fresh permit when the kept one
// is old. The document is only the door: the installation checks that it exists before the relay is
// asked.
func (in *Install) Permit(ctx context.Context, token, bucket, doc string, cache *permits, max int) (Permission, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.token != "" && time.Now().Before(cache.until) {
		return Permission{Base: cache.base, Token: cache.token, ExpiresIn: int(time.Until(cache.until).Seconds())}, true
	}
	if strings.TrimSpace(doc) == "" {
		return Permission{}, false
	}
	var answer struct {
		URL       string `json:"url"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := in.command(ctx, token, "media.link", map[string]any{
		"bucket": bucket, "doc": doc, "max": max,
	}, &answer); err != nil || answer.URL == "" {
		return Permission{}, false
	}
	parsed, err := url.Parse(answer.URL)
	if err != nil {
		return Permission{}, false
	}
	permit := parsed.Query().Get("mt")
	if permit == "" {
		return Permission{}, false
	}
	// The base is the address without the route: ".../s/<id>" for a relayed installation.
	cut := strings.Index(answer.URL, "/api/")
	if cut <= 0 {
		return Permission{}, false
	}
	ttl := answer.ExpiresIn
	if ttl <= 0 {
		ttl = 600
	}
	// Kept a little shorter than it lives: an address built at the last second is expired by the time
	// the picture is asked for.
	cache.base, cache.token = answer.URL[:cut], permit
	cache.until = time.Now().Add(time.Duration(ttl)*time.Second - 45*time.Second)
	return Permission{Base: cache.base, Token: permit, ExpiresIn: ttl}, true
}

// snippet shortens an answer for an error message: a screen has room for one line.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "пустой ответ"
	}
	return s
}
