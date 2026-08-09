package delivery

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSignKnownVectorAndHeaderRotation(t *testing.T) {
	secret := []byte("secret")
	body := []byte("body")
	if got := Sign(secret, 1700000000, body); got != "42ac6f0448c1d9c3e1e82b9726248f58fef84afffcbad5188246e96070e0ea46" {
		t.Fatalf("known digest = %s", got)
	}
	rotation := []struct {
		name    string
		secrets [][]byte
		want    string
	}{
		{"one", [][]byte{[]byte("old")}, "v1=1357aaf3e8320b9f5f3f255ed236a05276ecd37cebc7a1eaa8051b63908d3793"},
		{"two", [][]byte{[]byte("old"), secret}, "v1=1357aaf3e8320b9f5f3f255ed236a05276ecd37cebc7a1eaa8051b63908d3793,v1=42ac6f0448c1d9c3e1e82b9726248f58fef84afffcbad5188246e96070e0ea46"},
		{"three", [][]byte{[]byte("old"), secret, []byte("new")}, "v1=1357aaf3e8320b9f5f3f255ed236a05276ecd37cebc7a1eaa8051b63908d3793,v1=42ac6f0448c1d9c3e1e82b9726248f58fef84afffcbad5188246e96070e0ea46,v1=de1665c0f1b73c0e41d8ddbf36f95bbddfa2ebb359160d4b02de302279e3fff7"},
	}
	for _, tc := range rotation {
		t.Run(tc.name, func(t *testing.T) {
			if got := SignAll(tc.secrets, 1700000000, body); got != tc.want || strings.Contains(got, ", ") {
				t.Fatalf("rotation header = %q, want %q", got, tc.want)
			}
		})
	}
	header := SignAll(rotation[2].secrets, 1700000000, body)
	for _, secret := range rotation[2].secrets {
		if !VerifyHeader(secret, 1700000000, body, header) {
			t.Fatalf("rotated secret %q did not verify", secret)
		}
	}
	if VerifyHeader([]byte("retired"), 1700000000, body, header) {
		t.Fatal("retired secret verified")
	}
	if !VerifyHeader(secret, 1700000000, body, header) {
		t.Fatal("configured middle secret did not verify")
	}
	if VerifyHeader(secret, 1700000001, body, header) || VerifyHeader(secret, 1700000000, []byte("bodY"), header) {
		t.Fatal("signature verified with a changed signed input")
	}
}

func TestSignNearMissesSeparatorOrderAndHexCase(t *testing.T) {
	got := Sign([]byte("secret"), 1700000000, []byte("body"))
	for _, wrong := range []string{
		"d9d0665fd7151faa31f859ed65c8c5a5713d65c20d15a8369e22180e0d40ef2d", // no separator
		"1c24c0126ab19ec70ef655d33466508c8928db9eba7da2242b98e97fc025b81f", // reversed parts
		strings.ToUpper(got),
	} {
		if got == wrong {
			t.Fatalf("near-miss digest unexpectedly matched: %s", wrong)
		}
	}
	if got != strings.ToLower(got) {
		t.Fatalf("digest is not lowercase hex: %s", got)
	}
}

func TestTwoAttemptsKeepBodyAndRotateTimestampSignature(t *testing.T) {
	body := []byte(`{"id":123,"data":{"new":1}}`)
	first, second := Sign([]byte("secret"), 100, body), Sign([]byte("secret"), 101, body)
	if first == second {
		t.Fatal("per-attempt signatures did not change")
	}
	if !Verify([]byte("secret"), 100, body, first) || !Verify([]byte("secret"), 101, body, second) {
		t.Fatal("a signature did not verify against its own timestamp")
	}
	if Verify([]byte("secret"), 101, body, first) || Verify([]byte("secret"), 100, body, second) {
		t.Fatal("a signature verified against the other attempt timestamp")
	}
}

func TestSignerCopiesSecretsAndKeepsStoragePrivate(t *testing.T) {
	secrets := []string{"old", "new"}
	signer := NewSigner(secrets)
	secrets[0] = "changed"
	if got := signer.Header(1, []byte("body")); !strings.HasPrefix(got, "v1=") || strings.Contains(got, "changed") {
		t.Fatalf("signer did not defensively copy configuration: %q", got)
	}
}

func TestSignerFormattingRedactsSecretMarkers(t *testing.T) {
	const marker = "SIGNING-SECRET-MARKER"
	signer := NewSigner([]string{marker})
	for _, format := range []string{"%v", "%#v", "%s"} {
		if rendered := fmt.Sprintf(format, signer); strings.Contains(rendered, marker) {
			t.Fatalf("format %s exposed secret: %q", format, rendered)
		}
	}
}

func TestSignerLogValueMatchesStringUnderBothHandlers(t *testing.T) {
	signer := NewSigner([]string{"SIGNING-SECRET-MARKER"})
	want := signer.String()
	for _, format := range []string{"text", "json"} {
		var output bytes.Buffer
		var handler slog.Handler
		if format == "json" {
			handler = slog.NewJSONHandler(&output, nil)
		} else {
			handler = slog.NewTextHandler(&output, nil)
		}
		slog.New(handler).Info("signer", "value", signer)
		if strings.Contains(output.String(), "SIGNING-SECRET-MARKER") || !strings.Contains(output.String(), want) {
			t.Fatalf("%s handler output = %q, want redacted %q", format, output.String(), want)
		}
	}
}

func TestSignatureVerificationUsesConstantTimeHMACEqual(t *testing.T) {
	contents, err := os.ReadFile("signing.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "hmac.Equal(") {
		t.Fatal("signing implementation does not use hmac.Equal")
	}
}

func TestVerifyRejectsMalformedAndNearMissDigests(t *testing.T) {
	secret, body := []byte("rotation-secret"), []byte(`{"id":1}`)
	digest := Sign(secret, 99, body)
	if !Verify(secret, 99, body, digest) {
		t.Fatal("valid digest did not verify")
	}
	near := digest[:len(digest)-1] + "0"
	if Verify(secret, 99, body, near) || Verify(secret, 99, body, digest+"0") || Verify(secret, 99, body, "not-hex") {
		t.Fatal("malformed or near-miss digest verified")
	}
	if Verify(secret, 99, body, strings.ToUpper(digest)) {
		t.Fatal("uppercase digest verified")
	}
	if VerifyHeader(secret, 99, body, "v2="+digest) || VerifyHeader(secret, 99, body, "v1=not-hex,bad") {
		t.Fatal("unknown/malformed header term verified")
	}
	if VerifyHeader(secret, 99, body, "v1="+digest+",bad") {
		t.Fatal("valid signature plus unknown term verified")
	}
	other := Sign([]byte("different"), 99, body)
	if !VerifyHeader(secret, 99, body, "v1="+other+",v1="+digest) {
		t.Fatal("valid rotated signatures did not permit a matching term")
	}
}
