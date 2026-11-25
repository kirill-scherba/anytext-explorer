// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// ui.go — the module's screen, one self-contained document: its styles and its script travel inside
// the HTML. A module's frame is filled through the relay, and a <link> or a <script src> inside that
// frame carries no account token — the relay refuses it and the person sees bare HTML
// (docs/MODULES.md in anytext-gui, item 4).
package main

import (
	"embed"
	"html/template"
	"net/http"
	"strings"
)

//go:embed ui/index.html ui/app.js ui/style.css
var uiFS embed.FS

var pageTemplate = template.Must(template.ParseFS(uiFS, "ui/index.html"))

var (
	pageStyle  = template.CSS(mustAsset("style.css"))
	pageScript = template.JS(mustAsset("app.js"))
)

// page serves the screen with the one thing the server knows: where the module is mounted.
func (a *app) page(w http.ResponseWriter, r *http.Request) {
	// The installation says where the module is mounted; without the header the module was reached on
	// its own port and serves at the root, and a base of "/" keeps the page's relative calls pointing
	// at the module itself.
	prefix := strings.TrimRight(r.Header.Get("X-AnyText-Prefix"), "/")
	base := prefix + "/"
	if base == "/" || base == "" {
		base = "/"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = pageTemplate.Execute(w, map[string]any{
		"Base":    base,
		"Version": version,
		"Person":  who(r).Email,
		"Style":   pageStyle,
		"Script":  pageScript,
	})
}

// mustAsset reads one file of the screen at start: a missing file is a broken build.
func mustAsset(name string) string {
	data, err := uiFS.ReadFile("ui/" + name)
	if err != nil {
		panic("anytext-explorer: " + err.Error())
	}
	return string(data)
}
