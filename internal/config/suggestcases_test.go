package config

type suggestionCase struct {
	name       string
	written    string
	candidates []string
	want       string
	wantFound  bool
}

func suggestionCases() []suggestionCase {
	return []suggestionCase{
		{
			name: "distance 1", written: "colums", candidates: payloadNames(),
			want: "columns", wantFound: true,
		},
		{
			name: "distance 2", written: "colms", candidates: payloadNames(),
			want: "columns", wantFound: true,
		},
		{
			name:    "distance 3 is beyond the threshold",
			written: "clms", candidates: payloadNames(),
		},
		{
			name:    "a name resembling nothing",
			written: "frobnicate", candidates: payloadNames(),
		},
		{
			// `dedate` is two edits from update and delete; declaration order and
			// lexicographic order disagree, so only the documented tie-break says delete.
			name:    "a tie resolves lexicographically rather than by declaration order",
			written: "dedate", candidates: keyNames(schemaLevels[levelOperations]),
			want: "delete", wantFound: true,
		},
		{
			name: "no candidates at all", written: "anything",
		},
		{
			// Two substitutions in runes but four edits in bytes.
			name:    "a multi-byte name is measured in runes",
			written: "módé", candidates: payloadNames(), want: "mode", wantFound: true,
		},
	}
}
