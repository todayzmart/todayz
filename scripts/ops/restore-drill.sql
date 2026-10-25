-- Reconcile pass for the restore drill (W4-07). Read only. Prints one line per
-- fact: section <tab> key <tab> value. restore-drill.sh runs it on the source
-- (inside the dump's snapshot) and on the restored copy, then compares.
-- Never selects a value from a *_enc or secret column: tables are checksummed
-- whole (md5 of each row's text), so no row content leaves the database.

-- rows: exact row count and an order-free checksum per leaf table.
SELECT 'rows', n.nspname || '.' || c.relname,
       (xpath('/row/n/text()', x))[1]::text || ' ' || (xpath('/row/h/text()', x))[1]::text
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL query_to_xml(format(
  'SELECT count(*) AS n, coalesce(md5(string_agg(md5(t::text), %L ORDER BY md5(t::text))), %L) AS h FROM %I.%I t',
  '', 'empty', n.nspname, c.relname), false, true, '') AS x
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY 2;

-- ledger: the invariants of the ledger_reconcile job (credits.sql Reconcile). Each must be 0.
SELECT 'ledger', k, v::text FROM (
  SELECT
    (SELECT count(*) FROM (SELECT transaction_id FROM ledger_entries
       GROUP BY transaction_id HAVING sum(amount) <> 0) x) AS unbalanced,
    (SELECT count(*) FROM account_balances
       WHERE kind IN ('customer_wallet', 'customer_held') AND balance < 0) AS negative,
    (SELECT count(*) FROM (
       SELECT a.owner_id, sum(b.balance) AS bal FROM ledger_accounts a
       JOIN account_balances b ON b.account_id = a.id
       WHERE a.kind IN ('customer_wallet', 'customer_held') GROUP BY a.owner_id) w
     FULL JOIN (SELECT customer_id, sum(remaining + held) AS n FROM credit_lots GROUP BY customer_id) l
       ON l.customer_id = w.owner_id
     WHERE coalesce(w.bal, 0) <> coalesce(l.n, 0)) AS lots_vs_balances,
    (SELECT count(*) FROM credit_holds h
     LEFT JOIN (SELECT hold_id, sum(held_delta) AS n FROM credit_lot_consumptions
       WHERE hold_id IS NOT NULL GROUP BY hold_id) c ON c.hold_id = h.id
     WHERE coalesce(c.n, 0) <> CASE WHEN h.status = 'held' THEN h.amount - h.released ELSE 0 END) AS holds,
    (SELECT count(*) FROM (
       SELECT a.owner_id, b.balance FROM ledger_accounts a JOIN account_balances b ON b.account_id = a.id
       WHERE a.kind = 'customer_held') w
     FULL JOIN (SELECT customer_id, sum(amount - released) AS n FROM credit_holds
       WHERE status = 'held' GROUP BY customer_id) h ON h.customer_id = w.owner_id
     WHERE coalesce(w.balance, 0) <> coalesce(h.n, 0)) AS held_vs_holds,
    (SELECT count(*) FROM account_balances b
     LEFT JOIN (SELECT account_id, sum(amount) AS s FROM ledger_entries GROUP BY account_id) e
       ON e.account_id = b.account_id
     WHERE b.balance <> coalesce(e.s, 0)) AS cache_drift,
    (SELECT count(*) FROM credit_lots l
     LEFT JOIN (SELECT lot_id, sum(remaining_delta) AS r, sum(held_delta) AS h
       FROM credit_lot_consumptions GROUP BY lot_id) c ON c.lot_id = l.id
     WHERE l.remaining <> l.original + coalesce(c.r, 0) OR l.held <> coalesce(c.h, 0)) AS lot_drift,
    (SELECT coalesce(sum(amount), 0) FROM ledger_entries) AS entries_sum
) r CROSS JOIN LATERAL (VALUES
  ('unbalanced', unbalanced), ('negative', negative), ('lots_vs_balances', lots_vs_balances),
  ('holds', holds), ('held_vs_holds', held_vs_holds), ('cache_drift', cache_drift),
  ('lot_drift', lot_drift), ('entries_sum', entries_sum)) AS t(k, v);

-- money: credit sums that must survive the restore unchanged.
SELECT 'money', 'balance.' || kind, sum(balance)::text FROM account_balances GROUP BY kind
UNION ALL SELECT 'money', 'ledger_entries.amount', coalesce(sum(amount), 0)::text FROM ledger_entries
UNION ALL SELECT 'money', 'ledger_entries.abs', coalesce(sum(abs(amount)), 0)::text FROM ledger_entries
UNION ALL SELECT 'money', 'credit_lots.original', coalesce(sum(original), 0)::text FROM credit_lots
UNION ALL SELECT 'money', 'credit_lots.remaining', coalesce(sum(remaining), 0)::text FROM credit_lots
UNION ALL SELECT 'money', 'credit_lots.held', coalesce(sum(held), 0)::text FROM credit_lots
UNION ALL SELECT 'money', 'credit_holds.amount', coalesce(sum(amount), 0)::text FROM credit_holds
UNION ALL SELECT 'money', 'credit_holds.captured', coalesce(sum(captured), 0)::text FROM credit_holds
UNION ALL SELECT 'money', 'credit_holds.released', coalesce(sum(released), 0)::text FROM credit_holds
UNION ALL SELECT 'money', 'payouts.amount', coalesce(sum(amount), 0)::text FROM payouts
UNION ALL SELECT 'money', 'payout_lines.amount', coalesce(sum(amount), 0)::text FROM payout_lines
UNION ALL SELECT 'money', 'payout_statement_lines.amount', coalesce(sum(amount), 0)::text FROM payout_statement_lines
UNION ALL SELECT 'money', 'admin_adjustment_requests.amount', coalesce(sum(amount), 0)::text FROM admin_adjustment_requests
ORDER BY 2;

-- partitions: children, default partition, and the furthest range bound per parent.
SELECT 'partitions', n.nspname || '.' || p.relname,
       count(c.oid)::text || ' children'
       || CASE WHEN bool_or(pg_get_expr(c.relpartbound, c.oid) = 'DEFAULT') THEN ', default' ELSE ', no default' END
       || ', up to ' || coalesce(max(substring(pg_get_expr(c.relpartbound, c.oid) FROM $$TO \('([0-9-]+)$$)), 'none')
FROM pg_partitioned_table pt
JOIN pg_class p ON p.oid = pt.partrelid
JOIN pg_namespace n ON n.oid = p.relnamespace
LEFT JOIN pg_inherits i ON i.inhparent = p.oid
LEFT JOIN pg_class c ON c.oid = i.inhrelid
GROUP BY n.nspname, p.relname
ORDER BY 2;

-- migrations: goose and River versions.
SELECT 'migrations', 'goose.max_version', max(version_id)::text FROM goose_db_version WHERE is_applied
UNION ALL SELECT 'migrations', 'goose.applied', count(*)::text FROM goose_db_version WHERE is_applied AND version_id > 0
UNION ALL SELECT 'migrations', 'river.max_version', coalesce(max(version), 0)::text FROM river_migration;

-- schema: object counts, so a missing index, trigger, function or extension shows.
SELECT 'schema', 'indexes', count(*)::text FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
UNION ALL SELECT 'schema', 'triggers', count(*)::text FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace WHERE NOT t.tgisinternal AND n.nspname NOT IN ('pg_catalog', 'information_schema')
UNION ALL SELECT 'schema', 'functions', count(*)::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname = 'public'
UNION ALL SELECT 'schema', 'constraints', count(*)::text FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
  WHERE n.nspname = 'public'
UNION ALL SELECT 'schema', 'extensions', string_agg(extname || ' ' || extversion, ', ' ORDER BY extname) FROM pg_extension
UNION ALL SELECT 'schema', 'sequences', count(*)::text FROM pg_sequences WHERE schemaname = 'public';
