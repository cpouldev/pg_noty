package schema

import (
	"errors"
	"strings"
	"testing"
)

// The two DSN spellings libpq accepts, written against one password so a row can name what survived.
// A routine written for one leaves the other intact, which is why both are rows rather than one.
const (
	thePassword = "s3cr3t"
	uriDSN      = "postgres://noty:" + thePassword + "@db.internal:5432/notydb?sslmode=require"
	keywordDSN  = "host=db.internal user=noty password=" + thePassword + " dbname=notydb"
)

func TestEveryDSNSpellingLosesItsPasswordOnTheWayOut(t *testing.T) {
	for _, tc := range []struct {
		name, written string
		secrets       []string
		wantGone      []string
	}{
		{name: "the URI form", written: "connect: " + uriDSN, secrets: []string{thePassword},
			wantGone: []string{thePassword}},
		{name: "the keyword form", written: "connect: " + keywordDSN, secrets: []string{thePassword},
			wantGone: []string{thePassword}},

		// libpq takes the last of a repeated keyword, so an earlier one is a second credential
		// rather than a stale copy: both have to go.
		{
			name:     "two different passwords in one keyword string",
			written:  "connect: host=h user=u password=first password=second",
			secrets:  []string{"first", "second"},
			wantGone: []string{"first", "second"},
		},
		{
			name:     "one password written twice",
			written:  "connect: " + uriDSN + " retrying " + uriDSN,
			secrets:  []string{thePassword},
			wantGone: []string{thePassword},
		},
		{
			name:     "a URI carrying a password in its userinfo and again as a parameter",
			written:  "connect: postgres://noty:first@db.internal/notydb?password=second",
			secrets:  []string{"first", "second"},
			wantGone: []string{"first", "second"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left := finished(errors.New(tc.written), tc.secrets...)

			for _, secret := range tc.wantGone {
				if strings.Contains(left.Error(), secret) {
					t.Errorf("the finished error still reads %q, which holds %q", left, secret)
				}
			}
		})
	}
}

// TestAnElidedErrorStaysDiagnosable is the other side of the redaction: an error naming nothing is
// as useless as one naming the password is dangerous. Everything but the password survives.
func TestAnElidedErrorStaysDiagnosable(t *testing.T) {
	left := finished(errors.New("connect: "+uriDSN), thePassword).Error()

	for _, kept := range []string{"postgres://", "noty", "db.internal:5432", "notydb", "sslmode=require"} {
		if !strings.Contains(left, kept) {
			t.Errorf("the finished error %q no longer names %q, so it cannot be acted on", left, kept)
		}
	}
	if want := "postgres://noty:" + elidedPassword + "@db.internal:5432/notydb?sslmode=require"; !strings.Contains(left, want) {
		t.Errorf("the finished error reads %q, want it to read %q", left, want)
	}
}

// TestTheFinishingPointAnswersEveryShapeItIsHanded covers the inputs a caller hands it that are not
// an error carrying a DSN, each of which has its own answer.
func TestTheFinishingPointAnswersEveryShapeItIsHanded(t *testing.T) {
	if finished(nil, thePassword) != nil {
		t.Error("finished(nil) is not nil, so every success path would start returning an error")
	}

	// An unknown password is not a licence to rewrite the message: replacing the empty string would
	// put the placeholder between every rune of it.
	if left := finished(errors.New("connect: "+uriDSN), ""); left.Error() != "connect: "+uriDSN {
		t.Errorf("an empty secret rewrote the message to %q", left)
	}
	if left := finished(errors.New("connect: " + uriDSN)); left.Error() != "connect: "+uriDSN {
		t.Errorf("no secret at all rewrote the message to %q", left)
	}

	// Over-redaction is the safe direction and is deliberate: a password that happens to spell a
	// host takes the host with it rather than surviving.
	if left := finished(errors.New("connect to db.internal failed"), "db.internal"); strings.Contains(left.Error(), "db.internal") {
		t.Errorf("a secret spelling a host survived in %q", left)
	}
}

// TestASentinelSurvivesTheFinishingPoint is what makes the finishing point safe to put in front of
// every return: the six later steps assert against a specific sentinel, and an error that lost its
// sentinel on the way out would make those assertions fail for the wrong reason.
func TestASentinelSurvivesTheFinishingPoint(t *testing.T) {
	for _, tc := range sentinelDetail {
		t.Run(tc.name, func(t *testing.T) {
			assertMatchesOnly(t, finished(tc.built, thePassword), tc.sentinel)
		})
	}

	if left := finished(errors.New("connect: "+uriDSN), thePassword); sentinelMatching(left) != nil {
		t.Errorf("the finished form of an error matching no sentinel matches %v", sentinelMatching(left))
	}
}
