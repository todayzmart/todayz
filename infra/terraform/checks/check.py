#!/usr/bin/env python3
"""Offline policy checks for infra/terraform (W1-10). Standard library only.

  check.py iam [FILE...]     SC-OPS-05: IAM policy documents are checked against an ACTION
                             ALLOW-LIST. Only Describe*, Get*, List* and the named reads of
                             09 section 3.6 may appear in the API role's policy; anything else
                             (rds:ModifyDBInstance, svc:*, "*", NotAction, Deny-free writes)
                             fails. Resource "*" is fine for a read; this is not a
                             wildcard-resource rule.
  check.py worker [FILE...]  the worker policy holds exactly one write, cloudwatch:PutMetricData,
                             and only with the cloudwatch:namespace condition TodayZ/Worker.
  check.py timings [FILE]    SC-GS-10: deploy and health-check timings in modules/stack/timings.tf
                             and their wiring in main.tf.
  check.py all               everything above on the repository files.

Exit 0 when clean, 1 with one line per finding.
"""
import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
STACK = HERE.parent / "modules" / "stack"
API_POLICY = STACK / "policies" / "api-ops-readonly.json"
WORKER_POLICY = STACK / "policies" / "worker-metrics.json"
TIMINGS = STACK / "timings.tf"
STACK_MAIN = STACK / "main.tf"

# 09 section 3.6 / D-97: the reads A-22 makes.
NAMED_READS = {
    "cloudwatch:GetMetricData",
    "cloudwatch:DescribeAlarms",
    "elasticbeanstalk:Describe*",
    "rds:DescribeDBInstances",
    "pi:GetResourceMetrics",
    "pi:DescribeDimensionKeys",
}
# Describe*, Get*, List*: the verb prefix, then letters or a trailing wildcard.
READ_PATTERN = re.compile(r"^[a-z0-9-]+:(Describe|Get|List)[A-Za-z0-9]*\*?$")


def _as_list(v):
    return v if isinstance(v, list) else [v]


def check_readonly_policy(doc, label="policy"):
    """Return findings for a read-only policy document (dict)."""
    out = []
    stmts = _as_list(doc.get("Statement", []))
    if not stmts:
        out.append(f"{label}: no statements")
    seen = set()
    for i, st in enumerate(stmts):
        where = f"{label}: statement {st.get('Sid', i)}"
        if st.get("Effect") != "Allow":
            out.append(f"{where}: Effect must be Allow")
        if "NotAction" in st:
            out.append(f"{where}: NotAction is not allowed")
        if "Action" not in st:
            out.append(f"{where}: no Action")
        for a in _as_list(st.get("Action", [])):
            seen.add(a)
            if a in NAMED_READS or READ_PATTERN.match(a):
                continue
            out.append(f"{where}: action {a!r} is not on the read-only allow-list")
    for missing in sorted(NAMED_READS - seen):
        out.append(f"{label}: named read {missing} is missing")
    return out


def check_worker_policy(doc, label="worker policy"):
    out = []
    stmts = _as_list(doc.get("Statement", []))
    if len(stmts) != 1:
        out.append(f"{label}: expected exactly one statement, found {len(stmts)}")
    for st in stmts:
        where = f"{label}: statement {st.get('Sid', '?')}"
        if st.get("Effect") != "Allow":
            out.append(f"{where}: Effect must be Allow")
        if _as_list(st.get("Action", [])) != ["cloudwatch:PutMetricData"]:
            out.append(f"{where}: the only action allowed is cloudwatch:PutMetricData")
        ns = (st.get("Condition", {}).get("StringEquals", {}) or {}).get("cloudwatch:namespace")
        if ns != "TodayZ/Worker":
            out.append(f"{where}: needs Condition StringEquals cloudwatch:namespace = TodayZ/Worker (found {ns!r})")
        for op in st.get("Condition", {}):
            if op != "StringEquals":
                out.append(f"{where}: unexpected condition operator {op}")
    return out


def parse_locals(text):
    """name = number lines of timings.tf."""
    return {m.group(1): int(m.group(2)) for m in re.finditer(r"^\s*([a-z0-9_]+)\s*=\s*(\d+)\s*$", text, re.M)}


EXPECTED = {
    "deregistration_delay_seconds": 75,
    "health_check_interval_seconds": 5,
    "health_check_timeout_seconds": 4,
    "unhealthy_threshold": 2,
    "healthy_threshold": 2,
    "api_prestop_seconds": 15,
    "api_drain_seconds": 75,
    "api_stop_timeout_seconds": 120,
    "worker_stop_timeout_seconds": 180,
    "worker_drain_grace_seconds": 90,
    "worker_hard_cancel_seconds": 30,
}
# main.tf must pass these locals to the api environment (and not hard-code its own).
WIRING = [
    "health_check_interval_seconds = local.health_check_interval_seconds",
    "health_check_timeout_seconds  = local.health_check_timeout_seconds",
    "unhealthy_threshold           = local.unhealthy_threshold",
    "healthy_threshold             = local.healthy_threshold",
    "deregistration_delay_seconds  = local.deregistration_delay_seconds",
    'SHUTDOWN_PRESTOP               = "${local.api_prestop_seconds}s"',
    'SHUTDOWN_DRAIN                 = "${local.api_drain_seconds}s"',
    "tostring(local.api_stop_timeout_seconds)",
    "tostring(local.worker_stop_timeout_seconds)",
]


def check_timings(timings_text, main_text=None):
    out = []
    v = parse_locals(timings_text)
    for k, want in EXPECTED.items():
        if k not in v:
            out.append(f"timings.tf: {k} is missing")
        elif v[k] != want:
            out.append(f"timings.tf: {k} is {v[k]}, expected {want}")
    if out:
        return out
    # Relations the plan relies on.
    if v["worker_drain_grace_seconds"] + v["worker_hard_cancel_seconds"] >= v["worker_stop_timeout_seconds"]:
        out.append("timings.tf: DrainGrace + hard-cancel window must be below the worker stop timeout")
    if v["api_drain_seconds"] <= 60:
        out.append("timings.tf: api drain must exceed WriteTimeout (60 s)")
    if v["api_prestop_seconds"] + v["api_drain_seconds"] >= v["api_stop_timeout_seconds"]:
        out.append("timings.tf: pre-stop + drain must be below the api stop timeout")
    if v["deregistration_delay_seconds"] < v["api_drain_seconds"]:
        out.append("timings.tf: deregistration delay must cover the api drain")
    if v["health_check_timeout_seconds"] >= v["health_check_interval_seconds"]:
        out.append("timings.tf: health-check timeout must be below the interval")
    if v["health_check_interval_seconds"] * v["unhealthy_threshold"] > v["api_prestop_seconds"]:
        out.append("timings.tf: a draining instance must leave rotation inside the pre-stop (interval x unhealthy threshold)")
    if main_text is not None:
        for w in WIRING:
            if w not in main_text:
                out.append(f"modules/stack/main.tf: missing wiring {w!r}")
    return out


def _load(path):
    return json.loads(Path(path).read_text())


def run(argv):
    cmd = argv[1] if len(argv) > 1 else "all"
    files = argv[2:]
    findings = []
    if cmd in ("iam", "all"):
        for f in files if cmd == "iam" and files else [API_POLICY]:
            findings += check_readonly_policy(_load(f), Path(f).name)
    if cmd in ("worker", "all"):
        for f in files if cmd == "worker" and files else [WORKER_POLICY]:
            findings += check_worker_policy(_load(f), Path(f).name)
    if cmd in ("timings", "all"):
        t = Path(files[0]) if cmd == "timings" and files else TIMINGS
        main = STACK_MAIN.read_text() if t == TIMINGS else None
        findings += check_timings(t.read_text(), main)
    if cmd not in ("iam", "worker", "timings", "all"):
        print(__doc__)
        return 2
    for line in findings:
        print("FAIL", line)
    if not findings:
        print(f"infra checks ({cmd}): ok")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(run(sys.argv))
