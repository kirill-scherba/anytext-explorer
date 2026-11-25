// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import "testing"

// TestDetectLang: the flag wins, the environment decides otherwise, and a locale that says nothing
// about a language leaves the screen in Russian.
func TestDetectLang(t *testing.T) {
	cases := []struct{ flag, env, want string }{
		{"en", "ru_RU.UTF-8", "en"},
		{"ru", "en_US.UTF-8", "ru"},
		{"", "ru_RU.UTF-8", "ru"},
		{"", "en_US.UTF-8", "en"},
		{"", "C", "ru"},
		{"", "POSIX", "ru"},
	}
	for _, c := range cases {
		t.Setenv("LC_ALL", c.env)
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", c.env)
		if got := detectLang(c.flag); got != c.want {
			t.Errorf("detectLang(%q) with %s = %q, want %q", c.flag, c.env, got, c.want)
		}
	}
}

// TestStateTextSaysWhy: a file that is skipped is not skipped "for no reason" when the scan recorded
// one, and the words come from the interface, not from the code.
func TestStateTextSaysWhy(t *testing.T) {
	tr := func(key string) string { return ruTexts[key] }
	doc := &Doc{State: stateSkipped, Reason: "audio"}
	want := "пропущено — запись: текст добавляет расшифровка"
	if got := stateText(tr, doc); got != want {
		t.Errorf("stateText = %q, want %q", got, want)
	}
	if got := stateText(tr, &Doc{State: stateIndexed}); got != "в поиске" {
		t.Errorf("stateText of an indexed document = %q", got)
	}
}
