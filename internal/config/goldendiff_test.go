package config

import (
	"strconv"
	"strings"
)

// This file is how a golden mismatch is reported, and nothing else.
//
// It is the path every future golden failure is read through, so it is a subject in its own right
// rather than a detail of the harness: a wrong diff misreports the whole corpus at once, and a reader
// looking for the corpus assertions should not have to walk past a subsequence algorithm to reach
// them. golden_test.go owns the corpus and the write gate; this owns the report.

// goldenDiffContext is how many identical lines are shown on either side of a difference. Two
// is the convention's own context depth, which is what a reader of a rendered block already
// has in their eye.
const goldenDiffContext = 2

// goldenDiff reports the differing lines of a mismatch with a little context around each, and
// counts the identical lines it skipped between them. A corpus-wide format change is then
// diagnosable in one run rather than one fixture at a time, and stays readable once a golden
// grows past a screen.
//
// A differing line is quoted, so a difference that is only whitespace is visible; an unchanged
// context line is not, because quoting it would make the hunk harder to read than the block.
func goldenDiff(want, got string) string {
	script := alignedLines(strings.Split(want, "\n"), strings.Split(got, "\n"))

	var report strings.Builder
	skipped := 0
	for at, edit := range script {
		if edit.op == linesMatch && !withinContextOfADifference(script, at) {
			skipped++
			continue
		}
		writeSkipped(&report, skipped)
		skipped = 0

		report.WriteString(edit.reported() + "\n")
	}
	writeSkipped(&report, skipped)
	return report.String()
}

// withinContextOfADifference reports whether this step of the alignment sits within the context
// window of a difference, which is what lets the lines between two hunks be counted rather than
// printed.
func withinContextOfADifference(script []goldenEdit, at int) bool {
	for near := max(at-goldenDiffContext, 0); near <= min(at+goldenDiffContext, len(script)-1); near++ {
		if script[near].op != linesMatch {
			return true
		}
	}
	return false
}

// goldenEdit is one step of the alignment between a golden and the output compared against it.
type goldenEdit struct {
	op   alignment
	text string
}

// alignment is what one step of the alignment found.
type alignment uint8

const (
	linesMatch alignment = iota
	lineOnlyInTheGolden
	lineOnlyInTheOutput
)

func (e goldenEdit) reported() string {
	switch e.op {
	case lineOnlyInTheGolden:
		return "- " + strconv.Quote(e.text)
	case lineOnlyInTheOutput:
		return "+ " + strconv.Quote(e.text)
	default:
		return "  " + e.text
	}
}

// alignedLines pairs the golden's lines with the output's by longest common subsequence, so that
// one inserted or deleted line is reported as one difference.
//
// Aligning by line index instead would report every line after an insertion as changed, which is
// the reading a corpus-wide format change produces -- the case this diff exists to diagnose, and
// so the case it must not read worst on. The goldens are tens of lines long, so the quadratic
// table costs nothing worth measuring.
func alignedLines(want, got []string) []goldenEdit {
	common := commonSubsequenceLengths(want, got)

	var script []goldenEdit
	i, j := 0, 0
	for i < len(want) && j < len(got) {
		switch {
		case want[i] == got[j]:
			script = append(script, goldenEdit{op: linesMatch, text: want[i]})
			i, j = i+1, j+1
		// A tie drops the golden's line first, so a changed line reads as `-` then `+`.
		case common[i+1][j] >= common[i][j+1]:
			script = append(script, goldenEdit{op: lineOnlyInTheGolden, text: want[i]})
			i++
		default:
			script = append(script, goldenEdit{op: lineOnlyInTheOutput, text: got[j]})
			j++
		}
	}

	for ; i < len(want); i++ {
		script = append(script, goldenEdit{op: lineOnlyInTheGolden, text: want[i]})
	}
	for ; j < len(got); j++ {
		script = append(script, goldenEdit{op: lineOnlyInTheOutput, text: got[j]})
	}
	return script
}

// commonSubsequenceLengths[i][j] is how many lines the tails want[i:] and got[j:] have in common,
// which is what lets alignedLines choose the step that keeps the most lines aligned.
func commonSubsequenceLengths(want, got []string) [][]int {
	common := make([][]int, len(want)+1)
	for i := range common {
		common[i] = make([]int, len(got)+1)
	}

	for i := len(want) - 1; i >= 0; i-- {
		for j := len(got) - 1; j >= 0; j-- {
			if want[i] == got[j] {
				common[i][j] = common[i+1][j+1] + 1
				continue
			}
			common[i][j] = max(common[i+1][j], common[i][j+1])
		}
	}
	return common
}

func writeSkipped(report *strings.Builder, skipped int) {
	if skipped == 0 {
		return
	}
	report.WriteString("  ... " + strconv.Itoa(skipped) + " identical lines\n")
}
