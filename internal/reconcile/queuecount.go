package reconcile

import (
	"context"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// The queue boundary is deliberately narrow: this is reconcile's sole event_queue reader.
// internal/delivery owns every other queue read and all queue writes.
type queueCounts struct {
	Pending    int64
	Delivering int64
	Dead       int64
	Live       int64
}

// readQueueCounts obtains one per-listener status aggregate. It intentionally does not expose
// queue rows: delivery claiming and every other queue view belong to internal/delivery.
func readQueueCounts(ctx context.Context, on registryReader, serviceSchema, listener string) (queueCounts, error) {
	queue, err := qualifiedRegistryTable(serviceSchema, schema.TableEventQueue)
	if err != nil {
		return queueCounts{}, err
	}
	rows, err := on.Query(ctx, "SELECT status, count(*) FROM "+queue+" WHERE listener = $1 GROUP BY status", listener)
	if err != nil {
		return queueCounts{}, err
	}
	defer rows.Close()

	var counts queueCounts
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return queueCounts{}, err
		}
		switch status {
		case "pending":
			counts.Pending = count
		case "delivering":
			counts.Delivering = count
		case "dead":
			counts.Dead = count
		}
	}
	if err := rows.Err(); err != nil {
		return queueCounts{}, err
	}
	counts.Live = counts.Pending + counts.Delivering
	return counts, nil
}
