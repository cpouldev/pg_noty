package config

import "slices"

func signingSecretBoundariesAcceptance() acceptanceSpec {
	const emptyPath = "$.listeners[0].destination.signing.secrets"
	return wholeAcceptance(R38, "R38_ok_signing_secret_boundaries",
		[]acceptanceSubject{
			sequenceSubject("signing.secrets.empty", emptyPath, nil,
				replaceText("        secrets: []\n",
					"        secrets: [\"${SIGNING_SECRET_OLD}\"]\n")),
			scalarSubject("signing.secrets.one",
				"$.listeners[1].destination.signing.secrets[0]",
				"${SIGNING_SECRET}", `"${SIGNING_SECRET_OLD}"`),
		},
		[]acceptanceObservation{
			configObservation("signing.secrets.empty", "the empty signing-secret boundary",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						len(c.Listeners[0].Delivery.Destination.Signing.Secrets) == 0
				}),
			configObservation("signing.secrets.one", "the one-secret boundary",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						slices.Equal(c.Listeners[1].Delivery.Destination.Signing.Secrets,
							[]string{corpusVariables["SIGNING_SECRET"]})
				}),
		})
}
