"""Schemathesis checks for the TodayZ contract (W0-04; 04 §1.4, 08 §3.8, SC-PL-02).

run.sh loads this module through SCHEMATHESIS_HOOKS=hooks. The one check,
todayz_problem, looks at every answer with a status of 400 or more:

  - it is application/problem+json, unless the operation declares that status
    with application/json itself (/readyz answers 503 with plain JSON);
  - its "code" is registered in backend/api/problems/<domain>.yaml, with the
    status the registry gives it;
  - "status" repeats the HTTP status and "type" is https://todayz.in/errors/<code>.

problem_errors and load_registry are plain functions so test_hooks.py covers
them without a server; the check itself just reports what they return.
"""

from __future__ import annotations

import json
import os
import pathlib
from typing import Any

import yaml

PROBLEM_TYPE = "https://todayz.in/errors/"
PROBLEM_MEDIA_TYPE = "application/problem+json"


def load_registry(problems_dir: str | os.PathLike[str]) -> dict[str, int]:
    """Problem code -> status from every *.yaml in the registry directory."""
    registry: dict[str, int] = {}
    files = sorted(pathlib.Path(problems_dir).glob("*.yaml"))
    if not files:
        raise ValueError(f"no problem files in {problems_dir}")
    for path in files:
        with path.open(encoding="utf-8") as f:
            doc = yaml.safe_load(f) or {}
        for entry in doc.get("problems") or []:
            code, status = entry.get("code"), entry.get("status")
            if not isinstance(code, str) or not isinstance(status, int):
                raise ValueError(f"{path.name}: a problem needs a string code and an int status: {entry!r}")
            if code in registry:
                raise ValueError(f"{path.name}: problem code {code!r} is registered twice")
            registry[code] = status
    return registry


def declared_json_statuses(raw_operation: dict[str, Any]) -> set[str]:
    """Statuses the operation declares inline with an application/json body."""
    out: set[str] = set()
    for status, response in (raw_operation.get("responses") or {}).items():
        if isinstance(response, dict) and "application/json" in (response.get("content") or {}):
            out.add(str(status))
    return out


def problem_errors(
    status: int,
    content_type: str | None,
    body: Any,
    json_statuses: set[str],
    registry: dict[str, int],
) -> list[str]:
    """What is wrong with an error answer, as messages; empty when it conforms."""
    if status < 400:
        return []
    if str(status) in json_statuses:
        return [] if isinstance(body, dict) else [f"{status} is declared as JSON but the body is not an object"]
    errors: list[str] = []
    if not (content_type or "").split(";")[0].strip().lower() == PROBLEM_MEDIA_TYPE:
        errors.append(f"content type is {content_type!r}, not {PROBLEM_MEDIA_TYPE}")
    if not isinstance(body, dict):
        errors.append("body is not a JSON object")
        return errors
    code = body.get("code")
    if not isinstance(code, str) or not code:
        errors.append("no \"code\" in the problem")
        return errors
    if code not in registry:
        errors.append(f"code {code!r} is not registered in api/problems")
    elif registry[code] != status:
        errors.append(f"code {code!r} is registered with status {registry[code]}, answered {status}")
    if body.get("status") != status:
        errors.append(f"\"status\" is {body.get('status')!r}, answered {status}")
    if body.get("type") != PROBLEM_TYPE + code:
        errors.append(f"\"type\" is {body.get('type')!r}, want {PROBLEM_TYPE + code}")
    return errors


_registry: dict[str, int] | None = None


def registry() -> dict[str, int]:
    """The registry from TODAYZ_PROBLEMS_DIR (default backend/api/problems), loaded once."""
    global _registry
    if _registry is None:
        default = pathlib.Path(__file__).resolve().parent.parent.parent / "backend" / "api" / "problems"
        _registry = load_registry(os.environ.get("TODAYZ_PROBLEMS_DIR") or default)
    return _registry


def _body(response: Any) -> Any:
    try:
        return response.json()
    except (ValueError, json.JSONDecodeError):
        return None


try:
    import schemathesis
except ImportError:  # the unit tests only need the functions above
    schemathesis = None

if schemathesis is not None:
    # One line so a run's output proves the hooks were loaded (TestContractSmoke looks for it).
    print(f"todayz hooks: todayz_problem check on, {len(registry())} problem codes registered")

    @schemathesis.check
    def todayz_problem(ctx: Any, response: Any, case: Any) -> None:
        """Every error is a registered problem (04 §1.4)."""
        status = response.status_code
        if status < 400:
            return
        values = response.headers.get("content-type") or []
        content_type = values[0] if values else None
        raw = getattr(getattr(case.operation, "definition", None), "raw", None) or {}
        errors = problem_errors(status, content_type, _body(response), declared_json_statuses(raw), registry())
        if errors:
            raise AssertionError(f"{case.method} {case.path} -> {status}: " + "; ".join(errors))
