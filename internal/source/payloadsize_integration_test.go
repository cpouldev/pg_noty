//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// The behavioural half of the payload-cap claim: a row whose payload exceeds the listener's
// payload.max_bytes commits, and the event is recorded complete and byte-identical to the source
// data. Enforcement belongs to internal/delivery -- it moves such an event to dead with a reason --
// so failing the write here would stop the customer's application over a delivery-side policy, and
// truncating it would deliver a webhook that is silently wrong. The DDL's side of the claim is
// payloadsizescan_test.go's.

const (
	// theConfiguredCap is the listener's payload.max_bytes for this case.
	theConfiguredCap = 64
	// theOversizeFactor is how many times over the cap the written value is. It is a factor rather
	// than a length so the two values cannot drift apart into a "cap" the payload merely approaches.
	theOversizeFactor   = 1024
	thePayloadSizeTable = "payload_size_target"
)

// anOversizedValue is a value far past the cap whose every part is distinguishable: a head, a body
// whose repetitions are the bulk, and a tail, so a truncation at either end fails the comparison
// rather than only one of them.
func anOversizedValue() string {
	body := strings.Repeat("payload-body-", theConfiguredCap*theOversizeFactor/len("payload-body-"))
	return "HEAD:" + body + ":TAIL"
}

func TestAnOversizedPayloadCommitsAndIsRecordedInFull(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.`+thePayloadSizeTable+` (id int, blob text)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = thePayloadSizeTable
	request.Listener.Trigger.Payload = config.Payload{Mode: "full", MaxBytes: theConfiguredCap}
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	written := anOversizedValue()
	if len(written) <= theConfiguredCap {
		t.Fatalf(
			"the written value is %d bytes against a cap of %d, so this case does not exceed "+
				"the limit it is named for", len(written), theConfiguredCap,
		)
	}
	mustExecOn(t, pool, `INSERT INTO public.`+thePayloadSizeTable+` VALUES (1, $1)`, written)

	var stored string
	var payloadBytes, events int
	if err := pool.QueryRow(
		t.Context(),
		"SELECT payload->'new'->>'blob', octet_length(payload::text), count(*) OVER () "+
			"FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&stored, &payloadBytes, &events); err != nil {
		t.Fatalf("the oversized write did not commit an event: %v", err)
	}
	// Recorded rather than merely bounded, so a later reader can see the case genuinely exceeded the
	// cap instead of approaching it.
	t.Logf(
		"the recorded payload is %d bytes against a configured max_bytes of %d, %d times over",
		payloadBytes, theConfiguredCap, payloadBytes/theConfiguredCap,
	)
	if events != 1 {
		t.Fatalf("the oversized write committed %d events, want 1", events)
	}
	if stored != written {
		t.Fatalf(
			"the recorded value is %d bytes and the written one is %d; the first difference is "+
				"at byte %d", len(stored), len(written), firstByteDifference(stored, written),
		)
	}
	if queued := queueRowCountIn(t, pool, harnessSchema); queued != 1 {
		t.Fatalf("the oversized write left %d queue rows, want the 1 its event is owed", queued)
	}
}

// firstByteDifference is where two values part, so a truncation reports its cut rather than two
// unreadable blocks. It is only ever asked about values already known to differ, so the shorter
// length it answers for a prefix is the cut and not a not-found sentinel.
func firstByteDifference(stored, written string) int {
	for index := range min(len(stored), len(written)) {
		if stored[index] != written[index] {
			return index
		}
	}
	return min(len(stored), len(written))
}
