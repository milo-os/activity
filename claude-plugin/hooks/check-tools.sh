#!/usr/bin/env bash
set -euo pipefail

# Only surface activitytool guidance when the current workspace defines or tests ActivityPolicies.
has_policies=0
if [[ -d config/services/activity || -d test/activity-policies || -d test/policies || -d cmd/activitytool ]]; then
  has_policies=1
elif command -v rg >/dev/null 2>&1 && rg -q "kind:[[:space:]]*ActivityPolicy" config examples test 2>/dev/null; then
  has_policies=1
fi

[[ $has_policies -eq 1 ]] || exit 0

if command -v activitytool >/dev/null 2>&1 || [[ -x ./bin/activitytool ]]; then
  exit 0
fi

echo "The milo-activity plugin detected ActivityPolicy files in this repository, but activitytool is not on PATH (or ./bin/activitytool)."
echo "Use the milo-activity:test-policy skill to run policy tests via Taskfile (task test-activity-policies / task test:policies), the ghcr.io/milo-os/activitytool container image, or install activitytool with:"
echo "  go install go.miloapis.com/activity/cmd/activitytool@latest"
