package schema

import (
	"regexp"
	"strings"
	"testing"
)

// This file is the one reader the DDL assertions in this package share. It stands in for a SQL
// parser, so every extractor here is paired with a closure assertion in migrationddl_test.go: a
// CREATE or a COMMENT line the extractor does not recognise is reported rather than skipped, and
// the count of recognised statements is reconciled against the count of statements written.
// Without that pairing an object could be created and silently fall outside every assertion below.
//
// The patterns therefore also fix the DDL's layout. That is deliberate: the layout is what lets a
// container-free step assert the shape of SQL it cannot execute.

var (
	createTablePattern     = regexp.MustCompile(`^CREATE TABLE ([a-z_][a-z0-9_]*) \($`)
	createPartitionPattern = regexp.MustCompile(
		`^CREATE TABLE ([a-z_][a-z0-9_]*) PARTITION OF ([a-z_][a-z0-9_]*) DEFAULT;$`)
	createIndexPattern = regexp.MustCompile(
		`^CREATE INDEX ([a-z_][a-z0-9_]*) ON ([a-z_][a-z0-9_]*) \(([^)]*)\)(.*);$`)
	columnPattern = regexp.MustCompile(
		`^    ([a-z_][a-z0-9_]*) ([a-z0-9_]+) (NOT NULL|NULL)(.*),$`)
	commentHeadPattern = regexp.MustCompile(`^COMMENT ON (TABLE|COLUMN|INDEX) (\S+) IS$`)
	commentBodyPattern = regexp.MustCompile(`^    '(.*)';$`)
)

// tableConstraintPrefixes are the lines inside a CREATE TABLE block that declare something other
// than a column. A line inside a block matching neither these nor columnPattern is unrecognised.
var tableConstraintPrefixes = []string{
	"    PRIMARY KEY", "    CHECK ", "    FOREIGN KEY ", "    UNIQUE ", "    CONSTRAINT ",
}

// sqlObject is one object a migration creates, as its DDL declares it.
type sqlObject struct {
	name string
	kind ObjectKind
	// on is the parent for a partition and the indexed table for an index, and is empty for a
	// table.
	on string
}

// sqlColumn is one column declaration inside a CREATE TABLE block.
type sqlColumn struct {
	table   string
	name    string
	sqlType string
	notNull bool
	// rest is whatever follows the nullability, such as GENERATED ALWAYS AS IDENTITY.
	rest string
}

// sqlIndex is one CREATE INDEX statement, with its predicate kept apart from its columns so a
// partial index can be asserted to carry its WHERE clause rather than merely to exist.
type sqlIndex struct {
	name      string
	table     string
	columns   string
	predicate string
}

// createdObjectsIn is every object one migration's DDL creates, and every CREATE line it could not
// read. The second return is the closure: an unread CREATE is a created object outside the
// inventory, which is precisely the drift Step 12 would otherwise be the first to notice.
func createdObjectsIn(sql string) (objects []sqlObject, unread []string) {
	for _, line := range strings.Split(sql, "\n") {
		if !strings.HasPrefix(line, "CREATE ") {
			continue
		}
		object, read := createdObjectOn(line)
		if !read {
			unread = append(unread, line)
			continue
		}
		objects = append(objects, object)
	}
	return objects, unread
}

// createdObjectOn reads one CREATE line. The partition form is tried before the table form because
// both begin CREATE TABLE and only the partition form carries PARTITION OF; the table form's
// pattern anchors on a trailing open parenthesis, so neither can read the other's line, and
// TestThePartitionFormIsNotReadAsAPlainTable holds that apart.
func createdObjectOn(line string) (sqlObject, bool) {
	if found := createPartitionPattern.FindStringSubmatch(line); found != nil {
		return sqlObject{name: found[1], kind: KindPartition, on: found[2]}, true
	}
	if found := createTablePattern.FindStringSubmatch(line); found != nil {
		return sqlObject{name: found[1], kind: KindTable}, true
	}
	if found := createIndexPattern.FindStringSubmatch(line); found != nil {
		return sqlObject{name: found[1], kind: KindIndex, on: found[2]}, true
	}
	return sqlObject{}, false
}

// declaredColumnsIn is every column declared inside a CREATE TABLE block, and every line inside a
// block it could not read.
func declaredColumnsIn(sql string) (columns []sqlColumn, unread []string) {
	table := ""
	for _, line := range strings.Split(sql, "\n") {
		opened := createTablePattern.FindStringSubmatch(line)
		found := columnPattern.FindStringSubmatch(line)
		switch {
		case opened != nil:
			table = opened[1]
		case table == "":
		case strings.HasPrefix(line, ")"):
			table = ""
		case found != nil:
			columns = append(columns, sqlColumn{table: table, name: found[1], sqlType: found[2],
				notNull: found[3] == "NOT NULL", rest: found[4]})
		case !hasAnyPrefix(line, tableConstraintPrefixes):
			unread = append(unread, table+": "+line)
		}
	}
	return columns, unread
}

// declaredIndexesIn is every CREATE INDEX statement, predicate kept apart from columns.
func declaredIndexesIn(sql string) []sqlIndex {
	var indexes []sqlIndex
	for _, line := range strings.Split(sql, "\n") {
		found := createIndexPattern.FindStringSubmatch(line)
		if found == nil {
			continue
		}
		indexes = append(indexes, sqlIndex{name: found[1], table: found[2], columns: found[3],
			predicate: strings.TrimPrefix(found[4], " WHERE ")})
	}
	return indexes
}

// declaredCommentsIn maps a comment's target -- "INDEX event_queue_claim_idx", "COLUMN
// deliveries.response_snippet" -- to its text, and reports every COMMENT header whose body it
// could not read. Step 12 reads these back from obj_description, so a comment written in a shape
// this reader cannot see would be asserted here and absent there.
func declaredCommentsIn(sql string) (comments map[string]string, unread []string) {
	comments = map[string]string{}
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		head := commentHeadPattern.FindStringSubmatch(line)
		if head == nil {
			continue
		}
		body := []string(nil)
		if i+1 < len(lines) {
			body = commentBodyPattern.FindStringSubmatch(lines[i+1])
		}
		if body == nil {
			unread = append(unread, line)
			continue
		}
		comments[head[1]+" "+head[2]] = body[1]
	}
	return comments, unread
}

// migrationSQL is the embedded text of one version, read through the corpus rather than through a
// second file reader.
func migrationSQL(t *testing.T, version int) string {
	t.Helper()

	for _, found := range embeddedCorpusOrFail(t) {
		if found.version == version {
			return found.sql
		}
	}
	t.Fatalf("the embedded corpus holds no migration %d", version)
	return ""
}

// embeddedCorpusOrFail is the shipped corpus, or a failure. It fails rather than returning nothing,
// because every sweep below would otherwise pass vacuously.
func embeddedCorpusOrFail(t *testing.T) []migration {
	t.Helper()

	corpus, err := embeddedCorpus()
	if err != nil {
		t.Fatalf("the embedded corpus does not parse: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("the embedded corpus is empty, so every assertion over it would pass vacuously")
	}
	return corpus
}
