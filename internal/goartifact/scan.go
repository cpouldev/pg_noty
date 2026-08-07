// Package goartifact measures Go sources and locates them: which files in a directory are
// production sources, and how many physical lines a file holds.
//
// It is measurement only. Any policy about what those numbers should be stays with the package it
// governs, so a consumer adds its own gate rather than changing anything here.
package goartifact

import "bytes"

// PhysicalLineCount is how many lines a source occupies, whether or not it ends in a newline.
func PhysicalLineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}
