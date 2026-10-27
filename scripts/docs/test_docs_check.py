"""Tests for docs_check.py: a fixture repo that passes, then one break per rule."""
import os
import shutil
import tempfile
import unittest

import docs_check

STAMP = "> As built on main at abc1234.\n"


class DocsCheckTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        for d in ("backend/internal/orders", "backend/internal/testkit", "customer/src/cart",
                  "customer/src/app", ".github/workflows", "mk"):
            os.makedirs(os.path.join(self.root, d))
        self.write(".github/workflows/ci.yml", "on: push\n")
        self.write("mk/00-repo.mk", "\n")
        for rel in docs_check.REQUIRED_DOCS:
            self.write(rel, STAMP)
        for app in docs_check.APP_FOLDERS:
            self.write(f"{app}/README.md", "# app\n")
        self.write("customer/README.md", "| `cart` | the cart |\n")
        self.write("docs/guide/code-structure.md",
                   STAMP + "`backend/internal/orders` `backend/internal/testkit` 00-repo.mk\n")
        self.write("docs/guide/deployment.md", STAMP + "ci.yml\n")
        self.write("docs/features/orders.md",
                   STAMP + "Code: `backend/internal/orders`, see [flows](../guide/flows.md).\n")

    def write(self, rel, text):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)

    def errors(self):
        return docs_check.check(self.root)

    def test_fixture_passes(self):
        self.assertEqual(self.errors(), [])

    def test_missing_required_doc(self):
        os.remove(os.path.join(self.root, "AGENTS.md"))
        self.assertIn("missing doc: AGENTS.md", self.errors())

    def test_broken_link(self):
        self.write("docs/features/orders.md", STAMP + "`backend/internal/orders` [x](../nope.md)\n")
        self.assertIn("docs/features/orders.md: broken link ../nope.md", self.errors())

    def test_links_in_code_are_ignored(self):
        self.write("README.md", "```\n[x](missing.md)\n```\n`[y](missing.md)` [z](https://e.com) [a](#top)\n")
        self.assertEqual(self.errors(), [])

    def test_new_package_needs_structure_and_feature_doc(self):
        os.makedirs(os.path.join(self.root, "backend/internal/wallet"))
        errs = self.errors()
        self.assertIn("docs/guide/code-structure.md does not name backend/internal/wallet", errs)
        self.assertIn("no docs/features/*.md names backend/internal/wallet", errs)

    def test_prefix_does_not_count(self):
        os.makedirs(os.path.join(self.root, "backend/internal/order"))
        self.assertIn("no docs/features/*.md names backend/internal/order", self.errors())

    def test_infra_package_needs_only_structure(self):
        self.assertNotIn("no docs/features/*.md names backend/internal/testkit", self.errors())

    def test_new_workflow(self):
        self.write(".github/workflows/nightly.yml", "on: schedule\n")
        self.assertIn("docs/guide/deployment.md does not name workflow nightly.yml", self.errors())

    def test_new_mk_file(self):
        self.write("mk/99-x.mk", "\n")
        self.assertIn("docs/guide/code-structure.md does not name mk/99-x.mk", self.errors())

    def test_new_feature_folder(self):
        os.makedirs(os.path.join(self.root, "customer/src/wishlist"))
        self.assertIn("customer/src/wishlist is not named in customer/README.md or docs/features", self.errors())

    def test_missing_stamp(self):
        self.write("docs/guide/flows.md", "# flows\n")
        self.assertIn("docs/guide/flows.md: no 'As built on main at …' line", self.errors())


if __name__ == "__main__":
    unittest.main()
