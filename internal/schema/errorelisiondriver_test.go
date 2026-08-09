package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file asserts the elision against an error the driver produced, which is the case ADR-11
// exists for: eliding a password from an error we constructed proves the routine works and says
// nothing about the path the leak actually takes.
//
// The DSN below carries a password twice, and the two halves are measured to be treated differently
// by the driver: it redacts the one in the URI's userinfo and leaves the one written as a parameter
// exactly as given. The parameter half is therefore the falsifiable half -- a row over the userinfo
// half alone would pass against a finishing point that did nothing.
const (
	userinfoPassword  = "userinfopw"
	parameterPassword = "parameterpw"
	driverRefusedDSN  = "postgres://noty:" + userinfoPassword + "@db.internal:5432/notydb" +
		"?password=" + parameterPassword + "&sslmode=this-is-not-an-sslmode"
)

// driverProducedError is an error the driver itself built around a DSN, obtained without a server:
// an unusable sslmode is refused while the connection string is being parsed.
func driverProducedError(t *testing.T) error {
	t.Helper()

	_, err := pgx.ParseConfig(driverRefusedDSN)
	if err == nil {
		t.Fatal("the driver accepted an unusable sslmode, so this file has no driver-produced error to elide")
	}
	return err
}

// TestTheDriversOwnRedactionStillLeavesAPasswordBehind pins the measurement the finishing point
// exists to answer. Measured on github.com/jackc/pgx/v5 v5.10.0: the userinfo password is replaced
// and a password written as a query parameter is rendered verbatim. If a later version closes that
// gap this fails by name, and the reader is told the justification changed rather than left with a
// redundant elision.
func TestTheDriversOwnRedactionStillLeavesAPasswordBehind(t *testing.T) {
	raw := driverProducedError(t).Error()

	if !strings.Contains(raw, parameterPassword) {
		t.Errorf("the driver's own error %q no longer carries a parameter password; re-derive the "+
			"finishing point's justification, which rests on it doing so", raw)
	}
	if strings.Contains(raw, userinfoPassword) {
		t.Logf("the driver no longer redacts the userinfo password either: %q", raw)
	}
}

// TestADriverProducedErrorLosesEveryPasswordOnTheWayOut is the criterion itself: the real leak path,
// both spellings, through the one finishing point.
func TestADriverProducedErrorLosesEveryPasswordOnTheWayOut(t *testing.T) {
	left := finished(driverProducedError(t), userinfoPassword, parameterPassword)

	for _, secret := range []string{userinfoPassword, parameterPassword} {
		if strings.Contains(left.Error(), secret) {
			t.Errorf("the finished driver error still reads %q, which holds %q", left, secret)
		}
	}
	for _, kept := range []string{"db.internal:5432", "notydb", "sslmode"} {
		if !strings.Contains(left.Error(), kept) {
			t.Errorf("the finished driver error %q no longer names %q", left, kept)
		}
	}
}

// TestTheFinishedErrorCannotBeUnwrappedBackToTheDriversCopyOfTheDSN closes the half a message-level
// elision leaves open.
//
// Measured on v5.10.0: *pgconn.ParseConfigError carries the connection string in an exported
// ConnString field, unredacted, whatever its Error method renders. An error chain that stayed
// unwrappable to it would hand a caller everything the message had just removed, so the finishing
// point keeps only this package's own sentinel and drops the driver's error.
func TestTheFinishedErrorCannotBeUnwrappedBackToTheDriversCopyOfTheDSN(t *testing.T) {
	raw := driverProducedError(t)

	var carried *pgconn.ParseConfigError
	if !errors.As(raw, &carried) {
		t.Fatalf("the driver's error is a %T, not the type this severing is written against", raw)
	}
	if !strings.Contains(carried.ConnString, userinfoPassword) {
		t.Fatalf("ConnString reads %q and no longer carries the password, so this test would pass "+
			"against a finishing point that severed nothing", carried.ConnString)
	}

	var reached *pgconn.ParseConfigError
	if errors.As(finished(raw, userinfoPassword, parameterPassword), &reached) {
		t.Errorf("the finished error unwraps back to the driver's, whose ConnString reads %q",
			reached.ConnString)
	}
}
