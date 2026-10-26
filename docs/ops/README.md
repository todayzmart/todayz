# Ops

How TodayZ is run in production. Short pages, plain steps. Names only: no secret value, URL with
a password, phone number, OTP or order code ever goes in these files.

| Page | When you need it |
|---|---|
| [Severity levels](severity.md) | Deciding how bad an incident is and how fast to answer |
| [On-call basics](on-call.md) | Carrying the pager: what to do when an alert fires, and what never to do |
| [Incident template](incident-template.md) | Opening a SEV1 or SEV2 note |
| [Release checklist](release-checklist.md) | Before and after a push to `main` (it deploys prod, D-210), and rollback |
| [Secret rotation](secret-rotation.md) | Changing a key, by name, and which keys cannot be changed yet |
| [Backup and restore](backup-restore.md) | The local restore drill, a cloud PITR restore, and the cross-region copy |
| [Runbooks](../runbooks/README.md) | One per alert |

Evidence: the restore drill writes `docs/qa/ops/restore-drill-<date>.md`;
`make release-candidate` writes `docs/qa/rc-<date>/`.

Checks: `make check` runs `check-runbooks` (`mk/60-ops.mk`). `make ops-restore-drill` is run by
hand, not by `make check`.
