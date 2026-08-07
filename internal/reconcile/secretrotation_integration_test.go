//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds both halves of the secret-rotation rule. It is separated from
// secrets_integration_test.go's leak assertions only by the 200-line bound; its fixture is the same
// marked configuration.

// TestRotatingASigningSecretProposesNothingAndATriggerChangeProposesAReplacement is that rule.
//
// listeners.spec holds Listener.Trigger and nothing else, so rotating a delivery
// secret must move neither spec_hash nor the plan. The second half is what stops that from being
// vacuous: a hash covering nothing would also leave the plan clean on rotation, so a
// trigger-affecting change to the same listener is asserted to propose a replacement in the same
// test.
func TestRotatingASigningSecretProposesNothingAndATriggerChangeProposesAReplacement(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	planted := preparedCredentialTarget(t, pool, "credential_rotation")
	if _, err := Apply(t.Context(), pool, planted.cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	applied := registrySpecHash(t, pool, planted.listener.Name)

	rotated := planted.cfg
	rotated.Listeners = append([]config.Listener(nil), planted.cfg.Listeners...)
	rotated.Listeners[0].Delivery.Destination.Signing.Secrets = []string{
		credentialMarkedOnEveryLine("rotated", "a wholly new secret\nspanning two lines").text,
	}

	result, err := Apply(t.Context(), pool, rotated, Approval{Approved: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Plan.Actions) != 0 || result.Statements != 0 || result.Verdict != VerdictClean {
		t.Errorf(
			"rotating a signing secret planned %+v with %d statements and verdict %q, want a "+
				"clean plan issuing nothing; listeners.spec holds Listener.Trigger alone",
			result.Plan.Actions, result.Statements, result.Verdict,
		)
	}
	if rotatedHash := registrySpecHash(t, pool, planted.listener.Name); rotatedHash != applied {
		t.Errorf("spec_hash moved from %q to %q on a delivery-only change", applied, rotatedHash)
	}

	assertATriggerChangeStillProposesAReplacement(t, pool, rotated, applied)
}

// assertATriggerChangeStillProposesAReplacement is that rule's other side, in the same test, so
// the unchanged hash above is shown to cover something rather than nothing.
func assertATriggerChangeStillProposesAReplacement(
	t *testing.T,
	pool *pgxpool.Pool,
	base config.Config,
	applied string,
) {
	t.Helper()

	changed := base
	changed.Listeners = append([]config.Listener(nil), base.Listeners...)
	changed.Listeners[0].Trigger.Payload.Mode = "keys_only"

	plan, err := Plan(t.Context(), pool, changed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionReplace {
		t.Fatalf(
			"a trigger-affecting change planned %+v, want one replace; without this the "+
				"unchanged hash above would also hold for a hash covering nothing", plan.Actions,
		)
	}
	if _, err := Apply(
		t.Context(),
		pool,
		changed,
		Approval{Approved: true, DestructionPermitted: true},
		Options{},
	); err != nil {
		t.Fatal(err)
	}
	if changedHash := registrySpecHash(t, pool, base.Listeners[0].Name); changedHash == applied {
		t.Errorf("spec_hash stayed %q across a trigger-affecting change, so it covers nothing", applied)
	}
}

func registrySpecHash(t *testing.T, pool *pgxpool.Pool, listener string) string {
	t.Helper()

	reading, err := readRegistry(t.Context(), pool, harnessSchema, listener)
	if err != nil {
		t.Fatalf("read registry for %q: %v", listener, err)
	}
	if reading.Listener.SpecHash == "" {
		t.Fatalf("listener %q has no recorded spec_hash, so comparing it proves nothing", listener)
	}
	return reading.Listener.SpecHash
}
