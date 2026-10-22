#!/bin/bash
# Recipe shell for the root Makefile. macOS ships GNU Make 3.81, which ignores
# .SHELLFLAGS, so strict mode is set here: every recipe line stops at its first
# failing command on make 3.81 and 4.x alike.
exec /bin/bash -euo pipefail "$@"
