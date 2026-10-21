#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for tool in yq promtool; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done
scratch_dir="$(mktemp -d)"
trap 'rm -rf "$scratch_dir"' EXIT

alerts_dir="$repo_root/config/components/observability/alerts"
yq '.spec' "$alerts_dir/dlq-alerts.yaml" > "$scratch_dir/rules.yaml"
yq '.spec' "$alerts_dir/slo-alerts.yaml" > "$scratch_dir/slo-rules.yaml"
yq '.spec' "$alerts_dir/generated/activity-recordings.yaml" > "$scratch_dir/recordings.yaml"
cp "$repo_root/test/observability/dlq-alerts.test.yaml" "$scratch_dir/test.yaml"
cp "$repo_root/test/observability/slo-alerts.test.yaml" "$scratch_dir/slo-test.yaml"
cd "$scratch_dir"
promtool check rules rules.yaml slo-rules.yaml recordings.yaml
promtool test rules test.yaml slo-test.yaml
