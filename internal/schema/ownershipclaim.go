package schema

import "context"

// This file is the two statements one object's ownership marker is read and written by.
// ownershipmarker.go is the other half -- the format itself, which after this split reaches no
// handle at all. The seam is where the 200-line budget this package's own gate enforces put it, and
// it is the one that changes for different reasons: that file changes when the marker's shape does,
// this one when what a caller may do with a refusal changes.
//
// Nothing here finishes an error the *server* raised, and that is ddlrefusal.go's warning rather
// than an omission: finished severs the chain to *pgconn.PgError deliberately (ADR-11), so a refusal
// finished here reaches its caller carrying no SQLSTATE at all. Every call site needs that
// condition, and each of them finishes what it was handed:
//
//   - Steps 11, 14 and 15 call through boundedTx, which classifies a lock timeout on the COMMENT
//     into ErrLockTimeout. A marker finished here would leave a contended create answering the
//     driver's own sentence instead.
//   - Step 13's step 4 classifies 42501 into ErrPrivilege for criterion 17, which is the whole of
//     ADR-3's DBA-pre-creates path: only an owner may comment on a schema, so a boot granted CREATE
//     and USAGE on a schema somebody else owns meets this refusal and no other.

// markerOn is what the catalog says about one object's ownership marker.
//
// It answers the zero markerReading alongside an error, whose state is none of the four, so a
// caller that ignored the error cannot read a failed observation as a state the catalog reported.
func markerOn(ctx context.Context, db catalogReader, object markedObject) (markerReading, error) {
	var written *string
	if err := db.QueryRow(ctx, object.readQuery, object.target).Scan(&written); err != nil {
		return markerReading{}, err
	}

	text, present := "", written != nil
	if present {
		text = *written
	}
	return markerReadingOf(object.form, text, present, object.instance), nil
}

// claimMarker writes this instance's ownership marker onto one object, in two statements because
// COMMENT ON accepts no bind parameter: the server renders the statement through format, and then
// it is executed.
//
// Both errors are handed back exactly as they arrived, for the reason this file's header gives.
func claimMarker(ctx context.Context, db catalogWriter, object markedObject) error {
	var statement string
	err := db.QueryRow(ctx, renderStatementQuery,
		object.commentOn, object.target, object.form.text(object.instance)).Scan(&statement)
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, statement)
	return err
}
