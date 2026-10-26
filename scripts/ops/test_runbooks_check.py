"""Tests for runbooks_check.py: a tiny fake repo per case, each break must be caught."""
import os
import shutil
import tempfile
import unittest

import runbooks_check

ALERT = """apiVersion: 1
groups:
  - orgId: 1
    name: demo
    rules:
      - uid: tz-demo
        title: 'Demo'
        annotations:
          summary: 'x'
          runbook_url: docs/runbooks/demo.md
          __dashboardUid__: todayz-00
"""

ALARMS = """resource "aws_sns_topic" "alarms" {
  name = "x"
}

resource "aws_cloudwatch_metric_alarm" "cpu" {
  alarm_name          = "${local.name}-rds-cpu-high"
  alarm_description   = "CPU. Runbook: docs/runbooks/demo.md."
  alarm_actions       = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "scale_in" {
  alarm_name          = "${local.name}-scale-in"
  alarm_description   = "Scale in."
  alarm_actions       = [aws_autoscaling_policy.worker_in.arn]
}
"""

GO = """package jobs

// Example: Runbook: "docs/runbooks/not-there.md" in a comment is ignored.
const r = "docs/runbooks/demo.md"
"""

RUNBOOK = """# Demo

Alert `tz-demo` and alarm `rds-cpu-high`.

## What it means
## First five minutes
See [dashboard](https://grafana.todayz.localhost/d/todayz-00) and [ops](../ops/severity.md).
## Diagnosis
## Mitigation
## Escalation
## Follow-up
"""


class RunbooksCheckTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.write("infra/grafana/alerts/demo.yaml", ALERT)
        self.write("infra/grafana/dashboards/00.json", '{"uid": "todayz-00"}')
        self.write("infra/terraform/modules/stack/alarms.tf", ALARMS)
        self.write("backend/internal/jobs/demo.go", GO)
        self.write("docs/runbooks/demo.md", RUNBOOK)
        self.write("docs/runbooks/README.md", "- [Demo](demo.md)\n")
        self.write("docs/ops/severity.md", "# Severity\n")

    def tearDown(self):
        shutil.rmtree(self.root)

    def write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(text)

    def errors(self):
        return runbooks_check.check(self.root)[0]

    def assertFails(self, needle):
        errs = self.errors()
        self.assertTrue(any(needle in e for e in errs), "%r not in %r" % (needle, errs))

    def test_clean_repo_passes(self):
        self.assertEqual(self.errors(), [])

    def test_alert_without_runbook_url(self):
        self.write("infra/grafana/alerts/demo.yaml", ALERT.replace("          runbook_url: docs/runbooks/demo.md\n", ""))
        self.assertFails("alert tz-demo has no runbook_url")

    def test_alert_runbook_missing(self):
        self.write("infra/grafana/alerts/demo.yaml", ALERT.replace("demo.md", "gone.md"))
        self.assertFails("runbook_url docs/runbooks/gone.md does not exist")

    def test_alert_dashboard_missing(self):
        self.write("infra/grafana/alerts/demo.yaml", ALERT.replace("todayz-00", "todayz-99"))
        self.assertFails("__dashboardUid__ todayz-99 is not a dashboard")

    def test_alarm_runbook_missing(self):
        self.write("infra/terraform/modules/stack/alarms.tf", ALARMS.replace("demo.md", "rds-cpu.md"))
        self.assertFails("alarm cpu runbook docs/runbooks/rds-cpu.md does not exist")

    def test_paging_alarm_without_runbook(self):
        self.write("infra/terraform/modules/stack/alarms.tf", ALARMS.replace(" Runbook: docs/runbooks/demo.md.", ""))
        self.assertFails("alarm cpu notifies people but names no Runbook")

    def test_scaling_only_alarm_needs_no_runbook(self):
        self.assertFalse(any("scale_in" in e for e in self.errors()))

    def test_job_runbook_missing(self):
        self.write("backend/internal/jobs/demo.go", GO.replace('const r = "docs/runbooks/demo.md"', 'const r = "docs/runbooks/payouts-x.md"'))
        self.assertFails("job runbook docs/runbooks/payouts-x.md does not exist")

    def test_go_test_files_are_ignored(self):
        self.write("backend/internal/jobs/demo_test.go", 'const x = "docs/runbooks/nope.md"\n')
        self.assertEqual(self.errors(), [])

    def test_missing_section(self):
        self.write("docs/runbooks/demo.md", RUNBOOK.replace("## Escalation\n", ""))
        self.assertFails("lacks section '## Escalation'")

    def test_runbook_must_name_its_alert(self):
        self.write("docs/runbooks/demo.md", RUNBOOK.replace("`tz-demo`", "the demo alert"))
        self.assertFails("never names its alert `tz-demo`")

    def test_runbook_must_name_its_alarm(self):
        self.write("docs/runbooks/demo.md", RUNBOOK.replace("`rds-cpu-high`", "the CPU alarm"))
        self.assertFails("never names its alert `rds-cpu-high`")

    def test_dead_dashboard_link(self):
        self.write("docs/runbooks/demo.md", RUNBOOK.replace("/d/todayz-00", "/d/todayz-42"))
        self.assertFails("dashboard link /d/todayz-42 is not a dashboard")

    def test_dead_relative_link(self):
        self.write("docs/runbooks/demo.md", RUNBOOK.replace("../ops/severity.md", "../ops/nope.md"))
        self.assertFails("link ../ops/nope.md does not resolve")

    def test_runbook_missing_from_index(self):
        self.write("docs/runbooks/other.md", "# Other\n")
        self.assertFails("README.md: does not list other.md")


if __name__ == "__main__":
    unittest.main()
