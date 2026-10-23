"""Unit tests for hooks.py (run by make check: python -I -m unittest discover)."""

from __future__ import annotations

import pathlib
import tempfile
import textwrap
import unittest

import hooks

REGISTRY = {"not_found": 404, "validation_failed": 422, "conflict": 409}


def problem(code: str, status: int, **extra: object) -> dict[str, object]:
    return {"type": hooks.PROBLEM_TYPE + code, "title": code, "status": status, "code": code, **extra}


class ProblemErrors(unittest.TestCase):
    def test_success_and_declared_json_pass(self) -> None:
        self.assertEqual(hooks.problem_errors(200, "text/html", "<p>", set(), REGISTRY), [])
        self.assertEqual(hooks.problem_errors(503, "application/json", {"status": "unavailable"}, {"503"}, REGISTRY), [])

    def test_declared_json_still_needs_an_object(self) -> None:
        self.assertEqual(
            hooks.problem_errors(503, "application/json", "down", {"503"}, REGISTRY),
            ["503 is declared as JSON but the body is not an object"],
        )

    def test_registered_problem_passes(self) -> None:
        body = problem("not_found", 404, detail="no such order", requestId="r1")
        self.assertEqual(hooks.problem_errors(404, "application/problem+json; charset=utf-8", body, set(), REGISTRY), [])

    def test_wrong_content_type(self) -> None:
        errors = hooks.problem_errors(404, "application/json", problem("not_found", 404), set(), REGISTRY)
        self.assertEqual(errors, ["content type is 'application/json', not application/problem+json"])
        errors = hooks.problem_errors(404, None, problem("not_found", 404), set(), REGISTRY)
        self.assertEqual(errors, ["content type is None, not application/problem+json"])

    def test_not_an_object_or_no_code(self) -> None:
        self.assertEqual(hooks.problem_errors(500, "text/plain", None, set(), REGISTRY),
                         ["content type is 'text/plain', not application/problem+json", "body is not a JSON object"])
        self.assertEqual(hooks.problem_errors(400, "application/problem+json", {"status": 400}, set(), REGISTRY),
                         ['no "code" in the problem'])
        self.assertEqual(hooks.problem_errors(400, "application/problem+json", {"code": ""}, set(), REGISTRY),
                         ['no "code" in the problem'])

    def test_unregistered_code(self) -> None:
        errors = hooks.problem_errors(409, "application/problem+json", problem("stock_gone", 409), set(), REGISTRY)
        self.assertEqual(errors, ["code 'stock_gone' is not registered in api/problems"])

    def test_registered_status_must_match(self) -> None:
        errors = hooks.problem_errors(400, "application/problem+json", problem("not_found", 400), set(), REGISTRY)
        self.assertEqual(errors, ["code 'not_found' is registered with status 404, answered 400"])

    def test_status_and_type_fields(self) -> None:
        body = {"code": "conflict", "status": 400, "type": "https://example.com/conflict", "title": "x"}
        errors = hooks.problem_errors(409, "application/problem+json", body, set(), REGISTRY)
        self.assertEqual(errors, [
            '"status" is 400, answered 409',
            '"type" is \'https://example.com/conflict\', want https://todayz.in/errors/conflict',
        ])


class DeclaredJsonStatuses(unittest.TestCase):
    def test_inline_json_only(self) -> None:
        raw = {"responses": {
            "200": {"content": {"application/json": {}}},
            "503": {"content": {"application/json": {}}},
            "404": {"content": {"application/problem+json": {}}},
            "default": {"$ref": "#/components/responses/Problem"},
        }}
        self.assertEqual(hooks.declared_json_statuses(raw), {"200", "503"})
        self.assertEqual(hooks.declared_json_statuses({}), set())


class LoadRegistry(unittest.TestCase):
    def write(self, tmp: str, name: str, text: str) -> None:
        pathlib.Path(tmp, name).write_text(textwrap.dedent(text), encoding="utf-8")

    def test_reads_every_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.write(tmp, "common.yaml", """
                domain: common
                problems:
                  - {code: not_found, status: 404, i18n: problems.not_found, title: Not found}
                  - {code: conflict, status: 409, i18n: problems.conflict, title: Conflict}
                """)
            self.write(tmp, "orders.yaml", """
                domain: orders
                problems:
                  - {code: insufficient_stock, status: 409, i18n: problems.insufficient_stock, title: Out of stock}
                """)
            self.assertEqual(hooks.load_registry(tmp), {"not_found": 404, "conflict": 409, "insufficient_stock": 409})

    def test_duplicate_code_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.write(tmp, "a.yaml", "problems: [{code: x, status: 400}]\n")
            self.write(tmp, "b.yaml", "problems: [{code: x, status: 409}]\n")
            with self.assertRaisesRegex(ValueError, "registered twice"):
                hooks.load_registry(tmp)

    def test_bad_entry_and_empty_dir_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(ValueError, "no problem files"):
                hooks.load_registry(tmp)
            self.write(tmp, "a.yaml", "problems: [{code: x, status: '400'}]\n")
            with self.assertRaisesRegex(ValueError, "string code and an int status"):
                hooks.load_registry(tmp)

    def test_real_registry_has_common_codes(self) -> None:
        reg = hooks.registry()
        self.assertEqual(reg["validation_failed"], 422)
        self.assertEqual(reg["not_found"], 404)
        self.assertEqual(reg["invalid_transition"], 409)


if __name__ == "__main__":
    unittest.main()
