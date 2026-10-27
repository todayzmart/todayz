# Docs stay true to the code (lead, 27 Oct 2026). See docs/guide/working-on-todayz.md.
#
#   check-docs   (offline) required docs exist; relative links in the README/docs set resolve;
#                every backend/internal package, mk file, workflow and app feature folder is
#                named in the docs; guide and feature docs carry their "As built on main at" line

CHECK_STEPS += check-docs

.PHONY: check-docs

check-docs:
	@cd $(ROOT)/scripts/docs && python3 -I -m unittest discover -s . -p 'test_*.py'
	@python3 -I $(ROOT)/scripts/docs/docs_check.py $(ROOT)
