package config

import (
	"slices"
	"strings"
)

// This file builds the documents the containment subjects run against, and renders them.
//
// It is separate from the assertions in containment_test.go because two grids now plant through it:
// the layout grid, whose key dimension is the inline spellings sensitiveKeyForms matches, and the
// grammar grid (containmentkeygrammar_test.go), whose key dimension is every spelling YAML permits.
// Both reach the two branches through one pairing below, so neither can quietly test one branch.

type plantedSecret struct {
	// where names the layout and key spelling that wrote this document, so a failure from either
	// subject identifies the case without the caller reassembling it from its own loop variables.
	where     string
	branch    string
	pathAware bool
	text      string
	markers   []string
}

func documentsHiding(secret markedSecret, layout secretLayout, key keySpelling) []plantedSecret {
	where := layout.name + ", a " + key.name + " key"
	body := layout.body(secret.text, key)
	markers := slices.Clone(secret.markers)
	if layout.watchedTail.text != "" {
		if strings.Count(body, watchedTailSlot) != 1 {
			panic("a containment layout with a watched physical tail must write its slot exactly once")
		}
		body = strings.Replace(body, watchedTailSlot, layout.watchedTail.text, 1)
		markers = append(markers, layout.watchedTail.markers...)
	}

	if layout.fallbackOnly {
		return []plantedSecret{plantedOnTheFallback(where, body, markers)}
	}
	return plantedOnBothBranches(where, body, markers)
}

// plantedOnBothBranches is one written declaration read by each of D3's branches: as written it
// parses, and with a second `version` key appended it does not, which is the one difference that
// decides the branch. Both grids pair their cases here rather than each writing the pairing, so a
// grid cannot cover one branch while reading as though it covered both.
func plantedOnBothBranches(where, body string, markers []string) []plantedSecret {
	written := "version: 1\n" + body

	return []plantedSecret{
		{where: where, branch: "path-aware", pathAware: true, text: written, markers: markers},
		{where: where, branch: "key-scoped fallback", pathAware: false,
			text: written + "version: 2\n", markers: markers},
	}
}

// plantedOnTheFallback is the one branch a declaration the parser cannot read ever reaches.
func plantedOnTheFallback(where, body string, markers []string) plantedSecret {
	return plantedSecret{
		where: where, branch: "key-scoped fallback", pathAware: false,
		text: "version: 1\n" + body, markers: markers,
	}
}

func renderEveryLineOf(document string) string {
	text := newSource("listeners.yaml", []byte(document))
	diags := make(Errors, 0, len(text.lines))
	for line := range text.lines {
		number := line + 1
		diags = append(diags, Error{File: text.Name(), Line: number, Col: 1, Msg: "message"})
	}

	warnings := make(Warnings, len(diags))
	for i, diag := range diags {
		warnings[i] = Warning(diag)
	}

	source := []byte(document)
	return diags.Render(source) + warnings.Render(source)
}
