package config

type sensitiveFallbackCase struct {
	name   string
	source string
	want   string
}

var sensitiveValueShapeCases = []sensitiveFallbackCase{
	{
		name:   "a sensitive key on its own line",
		source: "database:\n  url: postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  url: [redacted]\n  schema: noty\n",
	},
	{
		name:   "a sensitive key written on a sequence-item line",
		source: "database:\n- url: postgres://noty:s3cret@db/x\nversion: 1\n",
		want:   "database:\n- url: [redacted]\nversion: 1\n",
	},
	{
		name:   "a value written on the lines below a sensitive key",
		source: "database:\n  url:\n    postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  url:\n    [redacted]\n  schema: noty\n",
	},
	{
		name:   "a block scalar and the sibling item after it",
		source: "secrets:\n- |\n  first-s3cret\n- second-s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n  [redacted]\n[redacted]\nversion: 1\n",
	},
	{
		name:   "sequence items at the key's own indentation",
		source: "secrets:\n- one-s3cret\n- two-s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n[redacted]\nversion: 1\n",
	},
	{
		name:   "a comment and a blank line between items",
		source: "secrets:\n# a comment\n\n- s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n\n[redacted]\nversion: 1\n",
	},
	{
		name:   "a block scalar body that looks like a comment",
		source: "secrets:\n- |\n  #first-s3cret\n- second-s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n  [redacted]\n[redacted]\nversion: 1\n",
	},
	{
		name:   "a comment at a sensitive key's own indentation",
		source: "secrets:\n#s3cret\n- item-s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n[redacted]\nversion: 1\n",
	},
	{
		name:   "document markers inside a sensitive key's block",
		source: "secrets:\n- one-s3cret\n---\n- two-s3cret\n...\n- three-s3cret\nversion: 1\n",
		want:   "secrets:\n[redacted]\n---\n[redacted]\n...\n[redacted]\nversion: 1\n",
	},
	{
		name:   "a flow mapping holding a sensitive key",
		source: "database: {url: postgres://noty:s3cret@db/x}\nversion: 1\n",
		want:   "database: {url: [redacted]\nversion: 1\n",
	},
	{
		name:   "a flow sequence beside a sensitive key",
		source: "      secrets: [a-s3cret, b-s3cret]\n      other: kept\n",
		want:   "      secrets: [redacted]\n      other: kept\n",
	},
	{
		name:   "a public key of the same name as a sensitive one",
		source: "destination:\n  url: ftp://host/x\n  method: POST\n",
		want:   "destination:\n  url: [redacted]\n  method: POST\n",
	},
	{
		name: "keys whose names end in a sensitive key name",
		source: "database:\n  listen_url: postgres://noty:s3cret@replica/x\n" +
			"  not_url: kept\n",
		want: "database:\n  listen_url: [redacted]\n  not_url: kept\n",
	},
	{
		name:   "a document holding no sensitive key at all",
		source: "version: 1\nlisteners: [one, two\n",
		want:   "version: 1\nlisteners: [one, two\n",
	},
}
