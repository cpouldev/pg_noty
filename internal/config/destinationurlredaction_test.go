package config

import (
	"strings"
	"testing"
)

func TestCredentialBearingDestinationURLRendersVerbatimWhileActualSecretPathsDoNot(t *testing.T) {
	const (
		databasePassword    = "DATABASE-RENDER-SECRET"
		destinationPassword = "DESTINATION-RENDER-PUBLIC"
		signingSecret       = "SIGNING-RENDER-SECRET"
	)
	document := "version: 1\ndatabase:\n" +
		"  url: postgres://noty:" + databasePassword + "@db.internal/noty\n" +
		"listeners:\n- destination:\n" +
		"    url: https://hook:" + destinationPassword + "@hooks.example.test/order-paid\n" +
		"    signing:\n      secrets: [" + signingSecret + "]\n"

	rendered := renderEveryLineOf(document)
	for _, forbidden := range []string{databasePassword, signingSecret} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("rendered output quoted schema-sensitive text %q:\n%s", forbidden, rendered)
		}
	}
	want := "url: https://hook:" + destinationPassword + "@hooks.example.test/order-paid"
	if !strings.Contains(rendered, want) {
		t.Errorf("rendered output omitted public destination URL %q:\n%s", want, rendered)
	}
	if !strings.Contains(rendered, "postgres://noty:[redacted]@db.internal/noty") {
		t.Errorf("database URL did not retain its useful non-password parts:\n%s", rendered)
	}
}
