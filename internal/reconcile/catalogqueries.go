package reconcile

// Catalog query constants are catalog.go's query half.
const (
	resolveTargetQuery = `
SELECT relation.oid, namespace.nspname, relation.relname,
       COALESCE(array_agg(attribute.attname ORDER BY attribute.attnum)
         FILTER (WHERE attribute.attnum > 0 AND NOT attribute.attisdropped), ARRAY[]::text[]),
       COALESCE(array_agg(attribute.attname ORDER BY primary_key.position)
         FILTER (WHERE primary_key.position IS NOT NULL), ARRAY[]::text[])
FROM pg_catalog.pg_class AS relation
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
LEFT JOIN pg_catalog.pg_attribute AS attribute ON attribute.attrelid = relation.oid
LEFT JOIN LATERAL (
  SELECT key_columns.ordinality AS position
  FROM pg_catalog.pg_index AS index
  CROSS JOIN unnest(index.indkey) WITH ORDINALITY AS key_columns(attnum, ordinality)
  WHERE index.indrelid = attribute.attrelid AND index.indisprimary
    AND key_columns.attnum = attribute.attnum
) AS primary_key ON true
WHERE relation.oid = COALESCE($1::oid, pg_catalog.to_regclass($2))
  -- 'r' is an ordinary table and 'p' a partitioned one. PostgreSQL has carried row triggers on a
  -- partitioned parent since 11, propagating them to every partition, so admitting only 'r' told an
  -- operator their table "does not exist" for a table plainly there. Views, materialised views and
  -- foreign tables stay excluded: a row trigger on one of those is a different construct.
  AND relation.relkind IN ('r', 'p')
GROUP BY relation.oid, namespace.nspname, relation.relname`

	readTriggerQuery = `
SELECT trigger.oid, trigger.tgfoid, obj_description(trigger.oid, 'pg_trigger'),
       pg_get_triggerdef(trigger.oid)
FROM pg_catalog.pg_trigger AS trigger
WHERE trigger.tgrelid = $1 AND trigger.tgname = $2`

	readFunctionQuery = `
SELECT function.oid, obj_description(function.oid, 'pg_proc'), pg_get_functiondef(function.oid)
FROM pg_catalog.pg_proc AS function
WHERE function.oid = $1`

	readLocksQuery = `
SELECT lock.mode, lock.granted
FROM pg_catalog.pg_locks AS lock
WHERE lock.relation = $1 AND lock.pid = pg_catalog.pg_backend_pid()
ORDER BY lock.granted DESC, lock.mode`

	schemaExistsQuery = `
SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1)`

	advisoryLockHolderQuery = `
SELECT activity.pid::text
FROM pg_catalog.pg_locks AS locks
JOIN pg_catalog.pg_stat_activity AS activity ON activity.pid = locks.pid
WHERE locks.locktype = 'advisory' AND locks.granted AND locks.classid = $1::oid
  AND locks.objid = $2::oid AND locks.objsubid = 1
LIMIT 1`

	blockingBackendQuery = `
SELECT activity.pid::text
FROM unnest(pg_catalog.pg_blocking_pids($1)) AS blocker(pid)
JOIN pg_catalog.pg_stat_activity AS activity ON activity.pid = blocker.pid
LIMIT 1`
)
