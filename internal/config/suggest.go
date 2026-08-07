package config

// This file is the did-you-mean rule and nothing else: which declared name a misspelled one most
// likely meant, or that none of them is close enough to name. Where the candidates come from and
// what a diagnostic does with the answer are shapecheck.go's.
//
// CONFORMANCE with skill Pattern 8 (did-you-mean). The distance is hand-rolled with a threshold of
// 2, which is the precedent the CLI framework already approved for this project sets and which the
// Description states as the rule; no distance library is added, because this package introduces
// no third-party dependency at all.
//
// One mechanism of that pattern is substituted, and the substitution is the whole reason to state
// the conformance rather than assume it: Pattern 8's snippet indexes the two names **by byte**,
// and this table walks them **by rune**. A key an author wrote with a multi-byte character would
// otherwise sit two or three byte-edits from its neighbour for a single mistyped letter, so the
// suggestion that exists to help them is the one they would not get. The package counts columns in
// runes for the same reason (ADR-4), so the two agree about what a character is.

// suggestionThreshold is how far a written name may be from a declared one and still be worth
// naming. Beyond it a suggestion is more likely to mislead than to help, so the diagnostic states
// only that the key is unknown.
const suggestionThreshold = 2

// closestName is the candidate a written name most likely meant, and whether any candidate is
// close enough to say so. Presence is its own result, so an empty candidate list cannot be
// mistaken for a suggestion of the empty name.
//
// Ties are broken lexicographically, which is what keeps a golden file stable: two candidates the
// same distance away would otherwise be chosen by the order the schema table happened to list
// them in, and a key added to that table would silently rewrite an unrelated rendered hint. No two
// names the contract declares at one level are close enough to tie today -- the nearest pair,
// `name` and `table`, are three apart -- so the rule is written for the contract's next key rather
// than for its current ones.
func closestName(written string, candidates []string) (string, bool) {
	closest, nearest := "", suggestionThreshold+1

	for _, candidate := range candidates {
		distance := editDistance(written, candidate)

		if distance < nearest || (distance == nearest && candidate < closest) {
			closest, nearest = candidate, distance
		}
	}

	return closest, nearest <= suggestionThreshold
}

// editDistance is the Levenshtein distance between two names, counted in runes: the fewest
// insertions, deletions and substitutions that turn one into the other.
//
// One row of the table is held rather than the whole grid, because each row depends only on the
// one above it. `previous` carries the value the cell diagonally above held before this row
// overwrote it, which is what the substitution case needs.
func editDistance(a, b string) int {
	from, to := []rune(a), []rune(b)

	// Row zero: turning the empty prefix of `from` into each prefix of `to` costs one insertion
	// per rune.
	row := make([]int, len(to)+1)
	for j := range row {
		row[j] = j
	}

	for i := 1; i <= len(from); i++ {
		diagonal := row[0]
		// Column zero: turning this prefix of `from` into the empty prefix of `to` costs one
		// deletion per rune.
		row[0] = i

		for j := 1; j <= len(to); j++ {
			above := row[j]

			substitution := diagonal
			if from[i-1] != to[j-1] {
				substitution++
			}
			row[j] = min(above+1, row[j-1]+1, substitution)

			diagonal = above
		}
	}
	return row[len(to)]
}
