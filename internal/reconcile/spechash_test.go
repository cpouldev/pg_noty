package reconcile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestStoredListenerSpecContainsNoMarkedSigningSecret(t *testing.T) {
	secret, markers := signingSecretMarkedOnEveryLine()
	stored, err := deriveListenerSpec(listenerForSpec(secret, "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() error = %v", err)
	}

	for _, marker := range markers {
		if bytes.Contains(stored.Bytes, []byte(marker)) {
			t.Errorf("stored spec contains signing-secret marker %q: %s", marker, stored.Bytes)
		}
		if strings.Contains(stored.Hash, marker) {
			t.Errorf("stored hash contains signing-secret marker %q: %s", marker, stored.Hash)
		}
	}
}

func TestRotatingASigningSecretLeavesTheStoredSpecAndItsHashUnchanged(t *testing.T) {
	before, err := deriveListenerSpec(listenerForSpec("first-signing-secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() before rotation error = %v", err)
	}
	after, err := deriveListenerSpec(listenerForSpec("rotated-signing-secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() after rotation error = %v", err)
	}
	if !bytes.Equal(before.Bytes, after.Bytes) {
		t.Errorf(
			"stored spec changed after signing-secret rotation:\n before: %s\n  after: %s",
			before.Bytes,
			after.Bytes,
		)
	}
	if before.Hash != after.Hash {
		t.Errorf("stored hash changed after signing-secret rotation: before %q, after %q", before.Hash, after.Hash)
	}
}

func TestTriggerAffectingChangeChangesTheStoredSpecHash(t *testing.T) {
	before, err := deriveListenerSpec(listenerForSpec("unchanged-secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() before trigger change error = %v", err)
	}
	after, err := deriveListenerSpec(listenerForSpec("unchanged-secret", "NEW.status = 'held'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() after trigger change error = %v", err)
	}
	if before.Hash == after.Hash {
		t.Fatalf("stored hash = %q for distinct trigger definitions", before.Hash)
	}
}

func TestStoredListenerSpecHashesTheSameBytesItReturns(t *testing.T) {
	stored, err := deriveListenerSpec(listenerForSpec("secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() error = %v", err)
	}
	want := sha256.Sum256(stored.Bytes)
	if got := stored.Hash; got != hex.EncodeToString(want[:]) {
		t.Errorf("stored hash = %q, want SHA-256 of returned spec bytes %q", got, hex.EncodeToString(want[:]))
	}
}

func TestEncodingJSONKeepsTriggerStructFieldsInDeclarationOrder(t *testing.T) {
	stored, err := deriveListenerSpec(listenerForSpec("secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() error = %v", err)
	}
	const want = `{"Table":"sales.orders","Operations":[{"Kind":"insert","Columns":null,"When":"NEW.status = 'ready'"},{"Kind":"update","Columns":["status"],"When":""}],"Payload":{"Mode":"full","Columns":null,"Exclude":["secret"],"IncludeOld":false,"MaxBytes":2048}}`
	if got := string(stored.Bytes); got != want {
		t.Errorf("encoding/json TriggerSpec bytes = %s, want declaration-ordered %s", got, want)
	}
}

func TestStoredListenerSpecEncodingIsDeterministicAcrossProcesses(t *testing.T) {
	const outputEnvironment = "PG_NOTY_STEP7_SPEC_SUBPROCESS_OUTPUT"
	if path := os.Getenv(outputEnvironment); path != "" {
		stored, err := deriveListenerSpec(listenerForSpec("subprocess-secret", "NEW.status = 'ready'").Trigger)
		if err != nil {
			t.Fatalf("deriveListenerSpec() in subprocess error = %v", err)
		}
		if err := os.WriteFile(path, stored.Bytes, 0o600); err != nil {
			t.Fatalf("writing subprocess spec = %v", err)
		}
		return
	}

	stored, err := deriveListenerSpec(listenerForSpec("subprocess-secret", "NEW.status = 'ready'").Trigger)
	if err != nil {
		t.Fatalf("deriveListenerSpec() error = %v", err)
	}
	for range 1_000 {
		again, err := deriveListenerSpec(listenerForSpec("subprocess-secret", "NEW.status = 'ready'").Trigger)
		if err != nil {
			t.Fatalf("deriveListenerSpec() repeated error = %v", err)
		}
		if !bytes.Equal(again.Bytes, stored.Bytes) {
			t.Fatalf("in-process encoding differs: first %s, repeated %s", stored.Bytes, again.Bytes)
		}
	}

	outputPath := filepath.Join(t.TempDir(), "subprocess-spec.json")
	command := exec.Command(os.Args[0], "-test.run=^TestStoredListenerSpecEncodingIsDeterministicAcrossProcesses$")
	command.Env = append(os.Environ(), outputEnvironment+"="+outputPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("subprocess encoding failed: %v\n%s", err, output)
	}
	subprocess, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading subprocess spec = %v", err)
	}
	if !bytes.Equal(subprocess, stored.Bytes) {
		t.Errorf("subprocess encoding = %s, want %s", subprocess, stored.Bytes)
	}
}

func listenerForSpec(secret, when string) config.Listener {
	return config.Listener{
		Name:    "orders",
		Enabled: true,
		Trigger: config.TriggerSpec{
			Table: "sales.orders",
			Operations: config.Operations{
				{Kind: "insert", When: when},
				{Kind: "update", Columns: []string{"status"}},
			},
			Payload: config.Payload{Mode: "full", Exclude: []string{"secret"}, MaxBytes: 2048},
		},
		Delivery: config.DeliverySpec{
			Destination: config.Destination{Signing: config.Signing{Secrets: []string{secret}}},
		},
	}
}

func signingSecretMarkedOnEveryLine() (string, []string) {
	markers := []string{
		"step7-signing-secret-line-1",
		"step7-signing-secret-line-2",
		"step7-signing-secret-line-3",
	}
	return strings.Join(
		[]string{
			markers[0] + "=red",
			markers[1] + "=blue",
			markers[2] + "=green",
		}, "\n",
	), markers
}
