package config

// multibyteRuneClasses are the UTF-8 encoding lengths and the awkward code points a byte-wise scan
// must not split or misclassify: two, three and four byte encodings, a combining mark that carries
// no width of its own, the highest legal rune, and the replacement rune a decoder substitutes for
// invalid input.
//
// One table, because both scans that consume raw line bytes must survive the same classes and a
// second copy is a second definition free to drift: the flow-close state machine reading what follows
// a container's closer, and the unreadable-key scan reading what follows a key's colon.
var multibyteRuneClasses = []struct {
	name string
	text string
}{
	{name: "two byte letter", text: "é"},
	{name: "combining mark", text: "́"},
	{name: "three byte separator", text: " "},
	{name: "three byte ideograph", text: "界"},
	{name: "four byte symbol", text: "\U0001f642"},
	{name: "maximum rune", text: "\U0010ffff"},
	{name: "replacement rune", text: "�"},
}
