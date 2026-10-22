#!/usr/bin/env bash
set -euo pipefail
run_tf() { terraform "$@"; }
run_tf validate
