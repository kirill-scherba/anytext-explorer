// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"strings"
)

// The interface speaks the two languages the product speaks. The wording lives here and nowhere
// else, for the same reason the window keeps its own in one file: a screen with half of its words in
// English is worse than a screen in one language a person does not know.

type texts map[string]string

var ruTexts = texts{
	"title":        "anytext explorer",
	"files":        "файлы",
	"preview":      "просмотр",
	"collection":   "коллекция",
	"noCollection": "вне коллекций",
	"offline":      "установка не отвечает — состояние индекса неизвестно",
	"above":        "наверх",
	"folder":       "папка",
	"inFolder":     "в папке",
	"items":        "%d элементов",
	"itemsMany":    "больше %d элементов",
	"notRegular":   "это не обычный файл (ссылка, сокет или устройство) — не читаю",
	"slowFile":     "файл не отвечает: папка на медленном диске или файл занят",

	"stateIndexed":   "в поиске",
	"stateSkipped":   "пропущено",
	"stateExcluded":  "убрано вами",
	"stateUnscanned": "не сканировалось",

	"reasonUnsupported": "формат не поддерживается",
	"reasonAudio":       "запись: текст добавляет расшифровка",
	"reasonNoText":      "текста не найдено",
	"reasonExcluded":    "исключён вами",
	"reasonOther":       "без причины",

	"colState": "состояние",
	"colSize":  "размер",
	"colName":  "имя",

	"metaModified": "изменён",
	"metaKind":     "тип",
	"metaState":    "состояние",
	"metaSize":     "размер",
	"noPreview":    "нет текстового превью",
	"inIndex":      "текст есть только в индексе: F3",
	"indexText":    "текст из индекса",
	"fileText":     "файл как он есть",
	"description":  "описание",
	"transcript":   "расшифровка",
	"truncated":    "текст показан не весь",

	"helpMain":    "Enter открыть · BkSp вверх · Tab панель · F3 текст из индекса · F5 обновить",
	"helpIndex":   "F7 просканировать · F8 убрать из поиска / вернуть · F9 описать моделью · q выход",
	"helpPreview": "↑↓ листать · Tab назад к списку · q выход",

	"countIndexed":   "в поиске",
	"countSkipped":   "пропущено",
	"countExcluded":  "убрано",
	"countUnscanned": "не скан.",

	"loading":        "читаю состояние индекса…",
	"scanning":       "индексирую коллекцию…",
	"describing":     "описываю картинку моделью…",
	"asking":         "спрашиваю установку…",
	"scanResult":     "просканировано %s · добавлено %s · без изменений %s · не распознано %s",
	"excluded":       "%s убран из поиска, файл на месте",
	"restored":       "%s возвращён в поиск",
	"described":      "%s описан моделью",
	"emptyFolder":    "папка пуста",
	"nothingHere":    "здесь нечего открывать",
	"outsideKnown":   "файл вне коллекций — установка о нём ничего не знает",
	"needCollection": "действие работает только для файлов внутри коллекции",
	"confirmExclude": "Убрать из поиска?\n\n%s\n\nФайл останется на месте, но перестанет находиться поиском.",
	"confirmScan":    "Проиндексировать новое и изменённое в коллекции «%s»?",
	"yes":            "Да",
	"no":             "Нет",
}

var enTexts = texts{
	"title":        "anytext explorer",
	"files":        "files",
	"preview":      "preview",
	"collection":   "collection",
	"noCollection": "outside collections",
	"offline":      "installation does not answer — index state unknown",
	"above":        "up",
	"folder":       "folder",
	"inFolder":     "in the folder",
	"items":        "%d items",
	"itemsMany":    "more than %d items",
	"notRegular":   "this is not an ordinary file (a link, a socket or a device) — not read",
	"slowFile":     "the file does not answer: a folder on a slow disk, or the file is busy",

	"stateIndexed":   "in search",
	"stateSkipped":   "skipped",
	"stateExcluded":  "excluded",
	"stateUnscanned": "unscanned",

	"reasonUnsupported": "format not supported",
	"reasonAudio":       "recording: the transcript adds the text",
	"reasonNoText":      "no text found",
	"reasonExcluded":    "excluded by you",
	"reasonOther":       "no reason recorded",

	"colState": "state",
	"colSize":  "size",
	"colName":  "name",

	"metaModified": "modified",
	"metaKind":     "kind",
	"metaState":    "state",
	"metaSize":     "size",
	"noPreview":    "no text preview",
	"inIndex":      "the text is in the index only: F3",
	"indexText":    "text from the index",
	"fileText":     "the file as it is",
	"description":  "description",
	"transcript":   "transcript",
	"truncated":    "the text is shown in part",

	"helpMain":    "Enter open · BkSp up · Tab pane · F3 text from index · F5 refresh",
	"helpIndex":   "F7 scan · F8 exclude / restore · F9 describe · q quit",
	"helpPreview": "↑↓ scroll · Tab back to the list · q quit",

	"countIndexed":   "in search",
	"countSkipped":   "skipped",
	"countExcluded":  "excluded",
	"countUnscanned": "unscanned",

	"loading":        "reading the index state…",
	"scanning":       "indexing the collection…",
	"describing":     "describing the picture…",
	"asking":         "asking the installation…",
	"scanResult":     "scanned %s · added %s · unchanged %s · not recognised %s",
	"excluded":       "%s excluded from the search, the file stays",
	"restored":       "%s is back in the search",
	"described":      "%s described by the model",
	"emptyFolder":    "the folder is empty",
	"nothingHere":    "nothing to open here",
	"outsideKnown":   "the file is outside collections — the installation knows nothing about it",
	"needCollection": "this works only for files inside a collection",
	"confirmExclude": "Exclude from the search?\n\n%s\n\nThe file stays where it is and stops being found.",
	"confirmScan":    "Index the new and changed files of collection \"%s\"?",
	"yes":            "Yes",
	"no":             "No",
}

// detectLang answers the interface language: the flag when it is given, the environment otherwise,
// and Russian when neither says anything - it is the product's first language.
func detectLang(flag string) string {
	if v := normalLang(flag); v == "ru" || v == "en" {
		return v
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := normalLang(os.Getenv(name)); v == "ru" || v == "en" {
			return v
		}
	}
	// "C", "POSIX" and an empty environment say nothing about a language: the screen stays in the
	// language the product speaks first rather than pretending to be English.
	return "ru"
}

// normalLang accepts "ru", "en" and the locale forms ("ru_RU.UTF-8", "en_US").
func normalLang(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case strings.HasPrefix(v, "ru"):
		return "ru"
	case strings.HasPrefix(v, "en"):
		return "en"
	}
	return ""
}
