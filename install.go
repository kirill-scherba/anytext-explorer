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
	"net/http"
	"strings"
	"time"
)

// Install is the AnyText installation this explorer works with.
//
// The files are read from the local disk: inside the terminal that is the same trust boundary the
// shell already has (docs/TERMINAL.md). The installation is asked only what the disk cannot say -
// whether a file is in the search, why another one was skipped, what text was taken from it.
type Install struct {
	base   string
	client *http.Client
}

// newInstall answers a client for the installation at base (http://127.0.0.1:7787 by default).
func newInstall(base string) *Install {
	return &Install{
		base: strings.TrimRight(strings.TrimSpace(base), "/"),
		// A collection of two hundred thousand files answers doc.list slowly, and a scan answers
		// when it is done: a short timeout would report a working installation as dead.
		client: &http.Client{Timeout: 5 * time.Minute},
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
	Reason         string `json:"reason"`
	Size           int64  `json:"size"`
	ModTime        string `json:"mod_time"`
	Description    string `json:"description"`
	HasText        bool   `json:"has_text"`
	HasDescription bool   `json:"has_description"`
	HasAudio       bool   `json:"has_audio"`
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
	Chunks         int    `json:"chunks"`
}

// Document states, as doc.list names them.
const (
	stateIndexed   = "indexed"
	stateSkipped   = "skipped"
	stateExcluded  = "excluded"
	stateUnscanned = "unscanned"
)

// command posts one command and decodes its result into out. A command that the installation
// refuses with an explanation in words comes back as an error with those words.
func (in *Install) command(ctx context.Context, cmd string, args map[string]any, out any) error {
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
	resp, err := in.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", in.base, err)
	}
	defer resp.Body.Close()
	// Forty thousand characters of a document, times four buffers (text, description, audio,
	// metadata): the answer to doc.text is large by design and must not be cut at a megabyte.
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
	if out == nil {
		return nil
	}
	if len(answer.Result) == 0 || string(answer.Result) == "null" {
		return nil
	}
	return json.Unmarshal(answer.Result, out)
}

// WhoAmI checks that the installation answers at all.
func (in *Install) WhoAmI(ctx context.Context) (string, error) {
	var me struct {
		Role  string   `json:"role"`
		Email string   `json:"email"`
		Perms []string `json:"perms"`
	}
	if err := in.command(ctx, "app.whoami", nil, &me); err != nil {
		return "", err
	}
	if me.Role == "" {
		return "owner", nil
	}
	return me.Role, nil
}

// Buckets lists the collections of the installation.
func (in *Install) Buckets(ctx context.Context) ([]Bucket, error) {
	var out []Bucket
	if err := in.command(ctx, "bucket.list", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Docs answers the files of one collection whose path contains query. An empty bucket means every
// collection: the installation walks all of them, which is why the explorer asks about one folder
// of one collection at a time.
func (in *Install) Docs(ctx context.Context, bucket, query string, limit int) (*DocList, error) {
	args := map[string]any{"query": query}
	if bucket != "" {
		args["bucket"] = bucket
	}
	if limit > 0 {
		args["limit"] = limit
	}
	var out DocList
	if err := in.command(ctx, "doc.list", args, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Text answers what the search holds for one document: the extracted text, the description of a
// picture, the transcript of a recording.
func (in *Install) Text(ctx context.Context, bucket, key string) (*DocText, error) {
	var out DocText
	if err := in.command(ctx, "doc.text", map[string]any{"bucket": bucket, "doc": key}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Exclude takes a document out of the search and keeps the file where it is.
func (in *Install) Exclude(ctx context.Context, bucket, key string) error {
	return in.command(ctx, "doc.delete", map[string]any{
		"bucket": bucket, "doc": key, "delete_file": false,
	}, nil)
}

// Restore puts an excluded document back into the search.
func (in *Install) Restore(ctx context.Context, bucket, key string) error {
	return in.command(ctx, "doc.restore", map[string]any{"bucket": bucket, "doc": key}, nil)
}

// Describe asks the vision model what a picture shows.
func (in *Install) Describe(ctx context.Context, bucket, key string) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	if err := in.command(ctx, "doc.describe", map[string]any{"bucket": bucket, "doc": key}, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

// Scan brings the collection's index up to date with the folders. "changed" reads only what the
// scan has not seen or what has been rewritten since; "all" rebuilds the collection.
func (in *Install) Scan(ctx context.Context, bucket, mode string) (map[string]any, error) {
	if mode == "" {
		mode = "changed"
	}
	var out map[string]any
	if err := in.command(ctx, "bucket.scan", map[string]any{"bucket": bucket, "mode": mode}, &out); err != nil {
		return nil, err
	}
	return out, nil
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
