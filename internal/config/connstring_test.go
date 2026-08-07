package config

// This file covers connstring.go's one question -- where in a connection string the passwords sit
// -- through the replacement the redactor makes with the answer, because a span is only observable
// as the text it replaces.

// connectionRedactionCases covers the extent D3 declares for a URL:
// enough to make the secret unreadable, little enough to leave the diagnostic useful. Every
// expectation is the input with one span replaced, so a redactor that blanked the whole
// value fails every row, and a span that is not found at all fails it too -- the text comes
// back unchanged, which is the shape under-redaction takes here.
type connectionRedactionCase struct {
	name string
	text string
	want string
}

var connectionRedactionCases = []connectionRedactionCase{
	{
		name: "uri with a password",
		text: "postgres://noty:s3cret@db.internal:5432/noty",
		want: "postgres://noty:[redacted]@db.internal:5432/noty",
	},
	{
		// Nothing in a password-less URL is a secret, so nothing is replaced: R3's
		// diagnostic about it has to be able to name what it rejected.
		name: "uri without a password",
		text: "postgres://noty@db.internal:5432/noty",
		want: "postgres://noty@db.internal:5432/noty",
	},
	{
		// A password holding the characters that delimit a URL is why the span runs to
		// the last @ rather than to the first delimiter after the scheme.
		name: "uri whose password holds a colon and a slash",
		text: "postgres://noty:a:b/c@db.internal/noty",
		want: "postgres://noty:[redacted]@db.internal/noty",
	},
	{
		name: "quoted uri",
		text: `"postgres://noty:s3cret@db.internal/noty"`,
		want: `"postgres://noty:[redacted]@db.internal/noty"`,
	},
	{
		// A keyword password's span runs to the end of the text, so the pairs written
		// after it go with it. That is the stated cost of the only rule with no
		// under-redaction left in it: an unquoted value cannot be told from a value
		// holding a separator, and the property target falsified both narrower rules
		// (Implementation Note 21). The keywords written *before* the password stay
		// legible, which is what keeps the diagnostic useful.
		name: "keyword and value connection string",
		text: "host=db.internal user=noty password=s3cret dbname=noty",
		want: "host=db.internal user=noty password=[redacted]",
	},
	{
		// A quoted keyword value may hold spaces, so the span could never have ended at
		// one here. This row was the first evidence for the rule the row above now states
		// unconditionally.
		name: "keyword and value connection string with a quoted password",
		text: "host=db.internal password='s3 cret' dbname=noty",
		want: "host=db.internal password=[redacted]",
	},
	{
		// The class the fuzz target found once the oracle could see a partial leak: the
		// password's own bytes continue as something that reads exactly like the next
		// field. Committed as a seed at
		// testdata/fuzz/FuzzRenderedTextNeverQuotesASecret/6d59c70dfe3b1534.
		name: "keyword password whose own bytes look like the next field",
		text: "host=db.internal password=s3 A=cret",
		want: "host=db.internal password=[redacted]",
	},
	{
		// libpq permits whitespace on either side of the `=`, so a keyword matched as
		// the literal `password=` finds no span at all here and replaces nothing. The
		// whitespace itself is kept, because it is not part of the value.
		name: "keyword and value connection string with spaces around the equals",
		text: "host=db.internal password = 's3 cret' dbname=noty",
		want: "host=db.internal password = [redacted]",
	},
	{
		name: "keyword and value connection string with a space before the equals",
		text: "host=db.internal password =s3cret dbname=noty",
		want: "host=db.internal password =[redacted]",
	},
	{
		name: "keyword and value connection string with a space after the equals",
		text: "host=db.internal password= s3cret dbname=noty",
		want: "host=db.internal password= [redacted]",
	},
	{
		// libpq's own `sslpassword` ends in the keyword this scans for. No boundary is
		// required before it, deliberately: the passphrase of a client key is a secret
		// too, and requiring one would leave it rendered.
		name: "keyword whose name ends in the password keyword",
		text: "sslpassword=s3cret dbname=noty",
		want: "sslpassword=[redacted]",
	},
	{
		// Two password-bearing spans in one string, which is what a single-span answer
		// cannot represent: the userinfo password wins and the query keyword's is never
		// looked for.
		name: "uri whose query carries a second password",
		text: "postgres://u:pw@h/db?sslpassword=keypass",
		want: "postgres://u:[redacted]@h/db?sslpassword=[redacted]",
	},
	{
		name: "uri whose query carries the password keyword itself",
		text: "postgres://u:pw@h/db?password=querypass",
		want: "postgres://u:[redacted]@h/db?password=[redacted]",
	},
	{
		// libpq takes the last of a repeated keyword, so the value it actually connects
		// with is precisely the one a first-match-wins scan leaves rendered. One span now
		// covers both, because the first runs past the second -- which is the same
		// guarantee reached by covering rather than by counting.
		name: "a repeated password keyword",
		text: "host=db password=first password=second",
		want: "host=db password=[redacted]",
	},
	{
		// The two locators land on the same bytes, so the spans overlap: the userinfo
		// runs to the `@` and the keyword's value runs to the end of the text. They are
		// merged, because replacing one in turn would move the other's end and stitch a
		// redacted tail onto text that had already shifted.
		name: "a userinfo password spelled as a keyword pair",
		text: "postgres://u:password=secret@h/db",
		want: "postgres://u:[redacted]",
	},
	{
		// AC #24: the bytes on disk are the reference, never what it resolves to, and a
		// diagnostic that hid the reference could not say which variable is at fault.
		name: "bare environment reference",
		text: "${DATABASE_URL}",
		want: "${DATABASE_URL}",
	},
	{
		// A default is written text, so it can hold a secret, and the reference
		// exemption must not extend to it.
		name: "environment reference with a default",
		text: "${DATABASE_URL:-postgres://noty:s3cret@db.internal/noty}",
		want: "${DATABASE_URL:-postgres://noty:[redacted]@db.internal/noty}",
	},
	{
		// The rows above hold a reference where no password is looked for, so they say
		// nothing about the exemption itself. These four put one exactly where a password
		// is found, in both spellings a password has, and pin what "is a reference" means:
		// the interpolation grammar's own name charset (envreference.go), not a second one
		// spelled here. `${1ABC}` and `${a-b}` are references no author can resolve --
		// stage D refuses both -- so they are the written text they look like, and written
		// text where a password belongs is redacted.
		name: "a password written as a bare reference is left as written",
		text: "postgres://u:${PGPASSWORD}@h/db",
		want: "postgres://u:${PGPASSWORD}@h/db",
	},
	{
		name: "a keyword password written as a bare reference is left as written",
		text: "host=db password=${PGPASSWORD}",
		want: "host=db password=${PGPASSWORD}",
	},
	{
		name: "a password whose name starts with a digit is not a reference",
		text: "postgres://u:${1ABC}@h/db",
		want: "postgres://u:[redacted]@h/db",
	},
	{
		name: "a password whose name carries a hyphen is not a reference",
		text: "host=db password=${a-b}",
		want: "host=db password=[redacted]",
	},
}
