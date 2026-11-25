// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Command anytext-explorer is a file explorer for an AnyText installation, drawn in a terminal.
//
// It is not a module: no HTTP server, no port, no registry entry. It is a program a person runs in
// the window's terminal (docs/TERMINAL.md), where the installation lives, so it needs nothing but the
// installation's own address: ANYTEXT_SELF from the terminal's environment, or 127.0.0.1:7787.
//
// The files come from the local disk - inside the terminal that is the trust boundary a shell
// already has - and the installation is asked only what the disk cannot say: whether a file is in the
// search, why another one was skipped, and what text was taken from it.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// version is the explorer's own version.
const version = "0.1.0"

func main() {
	install := flag.String("install", "", "installation address (default: ANYTEXT_SELF or http://127.0.0.1:7787)")
	lang := flag.String("lang", "", "interface language: ru or en (default: from the environment)")
	dir := flag.String("dir", "", "folder to open (default: the current folder)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("anytext-explorer", version)
		return
	}

	base := strings.TrimSpace(*install)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("ANYTEXT_SELF"))
	}
	if base == "" {
		base = "http://127.0.0.1:7787"
	}

	folder := strings.TrimSpace(*dir)
	if folder == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "anytext-explorer:", err)
			os.Exit(1)
		}
		folder = wd
	}

	a := newApp(newInstall(base), detectLang(*lang), folder)
	if err := a.run(); err != nil {
		// A screen is the only way this program talks: when it cannot have one, it says so in one
		// line instead of a stack of escapes on a pipe.
		fmt.Fprintln(os.Stderr, "anytext-explorer:", err)
		os.Exit(1)
	}
}
