// Command generate writes the deterministic 200-listener performance specimen.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
)

const listenerCount = 200

var output = flag.String("output", "", "write the generated YAML to this path instead of stdout")

const documentHeader = `# Generated deterministically by testdata/gen/generate.go; do not edit.
version: 1
instance: performance
database:
  url: ${DATABASE_URL}
  schema: noty
  listen_url: ${DATABASE_LISTEN_URL}
worker:
  concurrency: 16
  batch_size: 100
  poll_interval: 10s
  lease_timeout: 5m
  drain_timeout: 30s
retention:
  keep: 168h
  partition_interval: 24h
  precreate: 168h
defaults:
  timeout: 5s
  retry:
    max_attempts: 5
    backoff: exponential
    initial_interval: 10s
    max_interval: 1h
    jitter: true
  headers:
    User-Agent: pgnoty/performance
listeners:
`

const listenerTemplate = `  - name: listener_%03d
    enabled: true
    table: public.events_%03d
    timeout: 10s
    concurrency: 4
    operations:
      insert: {}
      update:
        columns: [status, total]
        when: "OLD.status <> 'paid' AND NEW.status = 'paid'"
      delete: {}
    payload:
      mode: columns
      columns: [id, total, customer_id]
      include_old: true
      max_bytes: 262144
    destination:
      url: ${ORDER_WEBHOOK_URL}
      method: POST
      headers:
        X-Tenant: acme
      signing:
        secrets: ["${SIGNING_SECRET}"]
    retry:
      max_attempts: 10
`

func main() {
	flag.Parse()
	document := generatedDocument()
	if *output == "" {
		_, _ = os.Stdout.Write(document)
		return
	}
	if err := os.WriteFile(*output, document, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generatedDocument() []byte {
	var document bytes.Buffer
	document.WriteString(documentHeader)
	for index := 0; index < listenerCount; index++ {
		fmt.Fprintf(&document, listenerTemplate, index, index)
	}
	return document.Bytes()
}
