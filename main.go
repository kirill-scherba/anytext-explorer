// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Command anytext-explorer is the file explorer module of AnyText: the collections of an
// installation with the state of every file in the search, in a browser.
//
// It is a module (docs/MODULES.md in anytext-gui): a separate process with its own HTTP server on
// loopback, started and proxied by the installation at /api/module/explorer/. Nothing of it is
// compiled into AnyText, and the screens reach a remote computer through the relay like the window's
// own pages - the relay knows nothing about this program.
//
// The files themselves belong to the installation: they are listed and read through /api/command
// (doc.list, doc.text, doc.delete, doc.restore, doc.describe, bucket.scan), and the pictures and
// downloads come from the installation's own /api/thumb and /api/file. The module never touches the
// disk: one door, one set of rights, one place where the line between a person and the files is
// drawn (docs/MODULES.md, "A module is given the person's identity").
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// version is the module's own version.
const version = "0.2.2"

func main() {
	addr := flag.String("addr", "127.0.0.1:7870", "address to listen on (loopback only)")
	install := flag.String("install", "", "installation address (default: ANYTEXT_SELF or http://127.0.0.1:7787)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("anytext-explorer", version)
		return
	}

	base := strings.TrimRight(strings.TrimSpace(*install), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("ANYTEXT_SELF")), "/")
	}
	if base == "" {
		base = "http://127.0.0.1:7787"
	}

	a := &app{install: newInstall(base)}
	log.Printf("anytext-explorer %s: %s, installation %s", version, *addr, base)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
