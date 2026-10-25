#!/usr/bin/env python3
"""W4-07: every alert has a runbook, and every runbook link resolves.

Offline, stdlib only (runs as `python3 -I`). Fails (exit 1) when:

  1. a Grafana alert rule in infra/grafana/alerts/*.yaml has no runbook_url, or it
     points at a file that does not exist;
  2. a CloudWatch alarm in infra/terraform that notifies people (its alarm_actions
     include an SNS topic or local.alarm_actions) names no "Runbook: docs/runbooks/<x>.md", or a
     missing one;
  3. a job kind's runbook in backend Go code ("docs/runbooks/<x>.md" in a non-test,
     non-comment line) is missing;
  4. an alert runbook lacks one of the six sections (REQUIRED_SECTIONS) or never
     names the alert it serves (its uid, or the alarm name without the env prefix);
  5. a dashboard uid (alert __dashboardUid__, or a /d/<uid> link in docs/runbooks or
     docs/ops) is not a dashboard in infra/grafana/dashboards;
  6. a relative markdown link in docs/runbooks or docs/ops points at a missing file;
  7. a runbook file is missing from the docs/runbooks/README.md index.

Usage: runbooks_check.py [repo-root]
"""
import json
import os
import re
import sys

REQUIRED_SECTIONS = (
    "What it means",
    "First five minutes",
    "Diagnosis",
    "Mitigation",
    "Escalation",
    "Follow-up",
)

RE_RULE = re.compile(r"^\s*-\s+uid:\s*['\"]?([A-Za-z0-9_-]+)")
RE_RUNBOOK_URL = re.compile(r"^\s*runbook_url:\s*['\"]?([^'\"\s]+)")
RE_DASH_UID = re.compile(r"^\s*__dashboardUid__:\s*['\"]?([^'\"\s]+)")
RE_TF_ALARM = re.compile(r'^resource\s+"aws_cloudwatch_metric_alarm"\s+"([^"]+)"')
RE_TF_NAME = re.compile(r'alarm_name\s*=\s*"\$\{local\.name\}-([^"]+)"')
RE_TF_RUNBOOK = re.compile(r"Runbook:\s*(docs/runbooks/[a-z0-9][a-z0-9-]*\.md)")
RE_GO_RUNBOOK = re.compile(r'"(docs/runbooks/[a-z0-9][a-z0-9-]*\.md)"')
RE_MD_LINK = re.compile(r"\]\(([^)\s]+)\)")
RE_DASH_LINK = re.compile(r"grafana\.todayz\.localhost/d/([A-Za-z0-9_-]+)")
RE_HEADING = re.compile(r"^##\s+(.+?)\s*$")


def grafana_alerts(root):
    """[(file, uid, runbook_url or None, dashboard uid or None)] from the alert YAML."""
    out = []
    adir = os.path.join(root, "infra", "grafana", "alerts")
    for name in sorted(os.listdir(adir)) if os.path.isdir(adir) else []:
        if not name.endswith((".yaml", ".yml")):
            continue
        rel = os.path.join("infra", "grafana", "alerts", name)
        cur = None
        with open(os.path.join(root, rel), encoding="utf-8") as fh:
            for line in fh:
                m = RE_RULE.match(line)
                if m:
                    cur = [rel, m.group(1), None, None]
                    out.append(cur)
                    continue
                if cur is None:
                    continue
                m = RE_RUNBOOK_URL.match(line)
                if m:
                    cur[2] = m.group(1)
                m = RE_DASH_UID.match(line)
                if m:
                    cur[3] = m.group(1)
    return [tuple(a) for a in out]


def terraform_alarms(root):
    """[(file, resource, alarm name, notifies people, runbook or None)]."""
    out = []
    tdir = os.path.join(root, "infra", "terraform")
    for dirpath, dirnames, files in os.walk(tdir):
        dirnames[:] = [d for d in dirnames if not d.startswith(".")]
        for name in sorted(files):
            if not name.endswith(".tf"):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root)
            with open(path, encoding="utf-8") as fh:
                text = fh.read()
            # A resource block ends at the first line that is exactly "}".
            for m in re.finditer(r'^resource\s+"aws_cloudwatch_metric_alarm"[^\n]*\n(.*?)^\}', text, re.M | re.S):
                head = RE_TF_ALARM.match(text[m.start():])
                body = m.group(1)
                nm = RE_TF_NAME.search(body)
                rb = RE_TF_RUNBOOK.search(body)
                actions = re.search(r"alarm_actions\s*=\s*(\[[^\]]*\]|\S+)", body)
                notifies = bool(actions and re.search(r"aws_sns_topic\.|local\.alarm_actions", actions.group(1)))
                out.append((rel, head.group(1), nm.group(1) if nm else head.group(1), notifies, rb.group(1) if rb else None))
    return out


def go_runbooks(root):
    """[(file:line, runbook)] from non-test, non-comment backend Go lines."""
    out = []
    bdir = os.path.join(root, "backend")
    for dirpath, dirnames, files in os.walk(bdir):
        dirnames[:] = [d for d in dirnames if d not in ("gen", "node_modules", "vendor") and not d.startswith(".")]
        for name in sorted(files):
            if not name.endswith(".go") or name.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, name)
            with open(path, encoding="utf-8") as fh:
                for i, line in enumerate(fh, 1):
                    if line.lstrip().startswith("//"):
                        continue
                    for m in RE_GO_RUNBOOK.finditer(line):
                        out.append(("%s:%d" % (os.path.relpath(path, root), i), m.group(1)))
    return out


def dashboard_uids(root):
    uids = set()
    ddir = os.path.join(root, "infra", "grafana", "dashboards")
    for name in os.listdir(ddir) if os.path.isdir(ddir) else []:
        if name.endswith(".json"):
            with open(os.path.join(ddir, name), encoding="utf-8") as fh:
                uid = json.load(fh).get("uid")
            if uid:
                uids.add(uid)
    return uids


def sections(text):
    return {m.group(1) for m in (RE_HEADING.match(l) for l in text.splitlines()) if m}


def md_files(root, *dirs):
    for d in dirs:
        full = os.path.join(root, d)
        for name in sorted(os.listdir(full)) if os.path.isdir(full) else []:
            if name.endswith(".md"):
                yield os.path.join(d, name)


def check(root):
    errors = []
    # alert runbook -> names it must mention
    alert_runbooks = {}

    def runbook_exists(rb):
        return rb.startswith("docs/runbooks/") and os.path.isfile(os.path.join(root, rb))

    alerts = grafana_alerts(root)
    dashes = dashboard_uids(root)
    for rel, uid, rb, dash in alerts:
        if not rb:
            errors.append("%s: alert %s has no runbook_url" % (rel, uid))
        elif not runbook_exists(rb):
            errors.append("%s: alert %s runbook_url %s does not exist" % (rel, uid, rb))
        else:
            alert_runbooks.setdefault(rb, set()).add(uid)
        if dash and dash not in dashes:
            errors.append("%s: alert %s __dashboardUid__ %s is not a dashboard" % (rel, uid, dash))

    alarms = terraform_alarms(root)
    for rel, res, name, notifies, rb in alarms:
        if not rb:
            if notifies:
                errors.append("%s: alarm %s notifies people but names no Runbook: docs/runbooks/<x>.md" % (rel, res))
        elif not runbook_exists(rb):
            errors.append("%s: alarm %s runbook %s does not exist" % (rel, res, rb))
        else:
            alert_runbooks.setdefault(rb, set()).add(name)

    for where, rb in go_runbooks(root):
        if not runbook_exists(rb):
            errors.append("%s: job runbook %s does not exist" % (where, rb))

    for rb, names in sorted(alert_runbooks.items()):
        with open(os.path.join(root, rb), encoding="utf-8") as fh:
            text = fh.read()
        have = sections(text)
        for s in REQUIRED_SECTIONS:
            if s not in have:
                errors.append("%s: alert runbook lacks section '## %s'" % (rb, s))
        for n in sorted(names):
            if "`%s`" % n not in text:
                errors.append("%s: never names its alert `%s`" % (rb, n))

    for rel in md_files(root, "docs/runbooks", "docs/ops"):
        path = os.path.join(root, rel)
        with open(path, encoding="utf-8") as fh:
            text = fh.read()
        for m in RE_DASH_LINK.finditer(text):
            if m.group(1) not in dashes:
                errors.append("%s: dashboard link /d/%s is not a dashboard" % (rel, m.group(1)))
        for m in RE_MD_LINK.finditer(text):
            target = m.group(1).split("#", 1)[0]
            if not target or re.match(r"^[a-z][a-z0-9+.-]*:", target):
                continue
            if not os.path.exists(os.path.normpath(os.path.join(os.path.dirname(path), target))):
                errors.append("%s: link %s does not resolve" % (rel, m.group(1)))

    index = os.path.join(root, "docs", "runbooks", "README.md")
    if not os.path.isfile(index):
        errors.append("docs/runbooks/README.md: the runbook index is missing")
    else:
        with open(index, encoding="utf-8") as fh:
            text = fh.read()
        for rel in md_files(root, "docs/runbooks"):
            base = os.path.basename(rel)
            if base != "README.md" and "(%s)" % base not in text:
                errors.append("docs/runbooks/README.md: does not list %s" % base)

    return errors, len(alerts), len([a for a in alarms if a[4] or a[3]]), len(alert_runbooks)


def main(argv):
    root = os.path.abspath(argv[1] if len(argv) > 1 else os.path.join(os.path.dirname(__file__), "..", ".."))
    errors, n_alerts, n_alarms, n_runbooks = check(root)
    for e in errors:
        print("FAIL: " + e)
    if errors:
        print("runbooks: %d problem(s)" % len(errors))
        return 1
    print("ok: runbooks (%d Grafana alerts, %d CloudWatch alarms, %d alert runbooks)" % (n_alerts, n_alarms, n_runbooks))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
