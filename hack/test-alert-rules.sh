#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for tool in yq promtool; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done
scratch_dir="$(mktemp -d)"
trap 'rm -rf "$scratch_dir"' EXIT

yq '.spec' "$repo_root/config/components/observability/alerts/dlq-alerts.yaml" > "$scratch_dir/rules.yaml"
cp "$repo_root/test/observability/dlq-alerts.test.yaml" "$scratch_dir/test.yaml"
cd "$scratch_dir"
promtool check rules rules.yaml
promtool test rules test.yaml
