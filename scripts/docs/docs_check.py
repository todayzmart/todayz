#!/usr/bin/env python3
"""The docs stay true to the code (lead, 27 Oct 2026).

Offline, stdlib only (runs as `python3 -I`). Fails (exit 1) when:

  1. a required doc is missing (REQUIRED_DOCS, and a README.md in every app folder);
  2. a relative markdown link or image in the docs set points at a missing file;
  3. a directory in backend/internal is not named as `backend/internal/<name>` in
     docs/guide/code-structure.md;
  4. a domain package (backend/internal minus INFRA_PACKAGES) is not named as
     `backend/internal/<name>` in any docs/features/*.md;
  5. a workflow in .github/workflows is not named in docs/guide/deployment.md;
  6. a make step file in mk/ is not named in docs/guide/code-structure.md;
  7. a feature folder in customer/, business/ or admin/ src is not named in that
     app's README.md or in docs/features;
  8. a guide or feature doc has no "As built on main at" line.

New code that adds a package, workflow, mk file or feature folder therefore fails
`make check` until the docs describe it.

Usage: docs_check.py [repo-root]
"""
import os
import re
import sys

APP_FOLDERS = ("backend", "customer", "business", "admin", "site", "shared", "infra", "e2e", "design")
REQUIRED_DOCS = (
    "README.md",
    "AGENTS.md",
    "CLAUDE.md",
    "docs/README.md",
    "docs/features/README.md",
    "docs/guide/architecture.md",
    "docs/guide/flows.md",
    "docs/guide/code-structure.md",
    "docs/guide/local-dev.md",
    "docs/guide/testing.md",
    "docs/guide/deployment.md",
    "docs/guide/working-on-todayz.md",
)
# Plumbing with no user-facing feature of its own; code-structure.md still names them.
INFRA_PACKAGES = {"devtools", "testkit", "seed", "telemetry", "platform", "jobs", "ops"}
# Folders under <app>/src that hold wiring, not a feature.
APP_WIRING = {"app", "shell", "test", "routes", "assets", "styles", "lib", "i18n", "pwa"}
STAMP = "As built on main at"

LINK_RE = re.compile(r"!?\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
FENCE_RE = re.compile(r"^(```|~~~)")


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def strip_code(text):
    """Drop fenced blocks and inline code so example links are not checked."""
    out, fenced = [], False
    for line in text.splitlines():
        if FENCE_RE.match(line.strip()):
            fenced = not fenced
            continue
        if not fenced:
            out.append(re.sub(r"`[^`]*`", "", line))
    return "\n".join(out)


def docs_set(root):
    paths = ["README.md", "AGENTS.md", "docs/README.md"]
    paths += [f"{a}/README.md" for a in APP_FOLDERS]
    for d in ("docs/guide", "docs/features"):
        full = os.path.join(root, d)
        if os.path.isdir(full):
            paths += [f"{d}/{n}" for n in sorted(os.listdir(full)) if n.endswith(".md")]
    return [p for p in paths if os.path.isfile(os.path.join(root, p))]


def subdirs(path):
    if not os.path.isdir(path):
        return []
    return sorted(n for n in os.listdir(path) if os.path.isdir(os.path.join(path, n)) and not n.startswith("."))


def check(root):
    errors = []

    for rel in REQUIRED_DOCS + tuple(f"{a}/README.md" for a in APP_FOLDERS):
        if not os.path.isfile(os.path.join(root, rel)):
            errors.append(f"missing doc: {rel}")

    for rel in docs_set(root):
        base = os.path.dirname(os.path.join(root, rel))
        for target in LINK_RE.findall(strip_code(read(os.path.join(root, rel)))):
            if re.match(r"^[a-z][a-z0-9+.-]*:", target) or target.startswith("#"):
                continue
            path = target.split("#", 1)[0].split("?", 1)[0]
            if path and not os.path.exists(os.path.normpath(os.path.join(base, path))):
                errors.append(f"{rel}: broken link {target}")

    def text_of(rel):
        p = os.path.join(root, rel)
        return read(p) if os.path.isfile(p) else ""

    structure = text_of("docs/guide/code-structure.md")
    features_dir = os.path.join(root, "docs/features")
    features = ""
    if os.path.isdir(features_dir):
        features = "\n".join(read(os.path.join(features_dir, n)) for n in sorted(os.listdir(features_dir)) if n.endswith(".md"))

    for pkg in subdirs(os.path.join(root, "backend/internal")):
        name = f"backend/internal/{pkg}"
        if not re.search(re.escape(name) + r"\b", structure):
            errors.append(f"docs/guide/code-structure.md does not name {name}")
        if pkg not in INFRA_PACKAGES and not re.search(re.escape(name) + r"\b", features):
            errors.append(f"no docs/features/*.md names {name}")

    deployment = text_of("docs/guide/deployment.md")
    wf_dir = os.path.join(root, ".github/workflows")
    if os.path.isdir(wf_dir):
        for wf in sorted(os.listdir(wf_dir)):
            if wf.endswith((".yml", ".yaml")) and wf not in deployment:
                errors.append(f"docs/guide/deployment.md does not name workflow {wf}")

    mk_dir = os.path.join(root, "mk")
    if os.path.isdir(mk_dir):
        for mk in sorted(os.listdir(mk_dir)):
            if mk.endswith(".mk") and mk not in structure:
                errors.append(f"docs/guide/code-structure.md does not name mk/{mk}")

    for app in ("customer", "business", "admin"):
        readme = text_of(f"{app}/README.md")
        for folder in subdirs(os.path.join(root, app, "src")):
            if folder in APP_WIRING:
                continue
            pat = re.compile(r"(src/|`)" + re.escape(folder) + r"(/|`)")
            if not pat.search(readme) and not re.search(re.escape(f"{app}/src/{folder}"), features):
                errors.append(f"{app}/src/{folder} is not named in {app}/README.md or docs/features")

    for rel in docs_set(root):
        if rel.startswith(("docs/guide/", "docs/features/")) and STAMP not in text_of(rel):
            errors.append(f"{rel}: no '{STAMP} …' line")

    return errors


def main(argv):
    root = os.path.abspath(argv[1] if len(argv) > 1 else os.path.join(os.path.dirname(__file__), "..", ".."))
    errors = check(root)
    for e in errors:
        print(f"FAIL: {e}")
    if errors:
        print(f"docs check: {len(errors)} problem(s). Update the docs in the same change (see docs/guide/working-on-todayz.md).")
        return 1
    print(f"ok: docs match the code ({len(docs_set(root))} docs checked)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
