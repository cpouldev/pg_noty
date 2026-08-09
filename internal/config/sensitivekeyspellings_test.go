package config

var sensitiveKeySpellingCases = []sensitiveFallbackCase{
	{
		name:   "a bare key",
		source: "database:\n  url: postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  url: [redacted]\n  schema: noty\n",
	},
	{
		name:   "a double-quoted key",
		source: "database:\n  \"url\": postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  \"url\": [redacted]\n  schema: noty\n",
	},
	{
		name:   "a single-quoted key",
		source: "database:\n  'url': postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  'url': [redacted]\n  schema: noty\n",
	},
	{
		name:   "whitespace before the colon",
		source: "database:\n  url : postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  url : [redacted]\n  schema: noty\n",
	},
	{
		name: "a pasted JSON document",
		source: "{\n" +
			"  \"database\": {\n" +
			"    \"url\": \"postgres://noty:s3cret@db/x\"\n" +
			"  }\n",
		want: "{\n" +
			"  \"database\": {\n" +
			"    \"url\": [redacted]\n" +
			"  [redacted]\n",
	},
	{
		name:   "a value at its key's own indentation",
		source: "url:\npostgres://noty:s3cret@db/x\nversion: 1\n",
		want:   "url:\n[redacted]\nversion: 1\n",
	},
	{
		name:   "a quoted key holding an escape",
		source: "database:\n  \"ur\\u006c\": postgres://noty:s3cret@db/x\n  schema: noty\n",
		want:   "database:\n  [redacted]\n  schema: noty\n",
	},
	{
		name:   "document markers around a sensitive key",
		source: "---\ndatabase:\n  url: postgres://noty:s3cret@db/x\n...\n",
		want:   "---\ndatabase:\n  url: [redacted]\n...\n",
	},
}
