# W4-07: ops readiness. See docs/ops/README.md.
#
#   check-runbooks   (offline) every Grafana alert, paging CloudWatch alarm and job kind has a
#                    runbook that exists; alert runbooks have the six sections and name their
#                    alert; dashboard uids and relative links in docs/runbooks and docs/ops resolve
#   ops-restore-drill  (not in make check) pg_dump the local DB (port 5433), restore it into a
#                    scratch database, reconcile, and write docs/qa/ops/restore-drill-<date>.md.
#                    Needs the local Postgres up (make dev, or the stack's postgres container).

CHECK_STEPS += check-runbooks

.PHONY: check-runbooks ops-restore-drill

check-runbooks:
	@cd $(ROOT) && shellcheck -x scripts/ops/*.sh
	@cd $(ROOT)/scripts/ops && python3 -I -m unittest discover -s . -p 'test_*.py'
	@python3 -I $(ROOT)/scripts/ops/runbooks_check.py $(ROOT)

ops-restore-drill:
	@$(ROOT)/scripts/ops/restore-drill.sh
