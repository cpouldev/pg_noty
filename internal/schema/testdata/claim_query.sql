-- The claim query internal/source issues, held here as the one text both packages read.
--
-- The three shell-style placeholders a reader would expect are already bound to numbered
-- parameters in order of appearance: $1 is the claiming worker, $2 the lease bound, $3 the batch
-- size. The shape is load-bearing -- the LIMIT and the FOR UPDATE SKIP LOCKED below decide how the
-- statement plans, and a stand-in that dropped either would plan differently.
--
-- This is a pin rather than a copy. internal/source/claimquerypin_test.go's
-- TestClaimQueryMatchesTheCommittedFixture compares the text internal/source issues against this
-- file byte for byte, so the two cannot drift apart silently; explain_integration_test.go plans
-- this text and nothing else. If the issued query changes, change it here and re-run both.

UPDATE noty.event_queue q
  SET status='delivering', leased_by=$1, leased_until=now()+$2,
      attempts = attempts + 1
WHERE q.event_id IN (
  SELECT event_id FROM noty.event_queue
   WHERE status='pending' AND next_attempt_at <= now()
   ORDER BY next_attempt_at, event_id
   LIMIT $3 FOR UPDATE SKIP LOCKED)
RETURNING q.event_id;        -- payloads then fetched from noty.events by id
