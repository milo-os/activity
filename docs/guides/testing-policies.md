# Testing Activity Policies Offline

`activitytool` validates `ActivityPolicy` manifests and runs declarative unit
tests (`*.test.yaml`) offline without requiring a Kubernetes cluster — similar to
`promtool` for Prometheus alerting rules.

## Running `activitytool`

### Container image (recommended for consumer repos and CI)

Multi-arch images (`linux/amd64`, `linux/arm64`) are published to GitHub
Container Registry at `ghcr.io/milo-os/activitytool` on every release tag and
push to `main`. Mount your repository read-only at `/work`:

```bash
docker run --rm -v "$PWD:/work:ro" -w /work \
  ghcr.io/milo-os/activitytool:<version> \
  check config/services/activity/policies

docker run --rm -v "$PWD:/work:ro" -w /work \
  ghcr.io/milo-os/activitytool:<version> \
  test --policies config/services/activity/policies test/activity-policies
```

### Local binary (optional)

- Inside this repository:

  ```bash
  task build:activitytool
  ./bin/activitytool --version
  ```

- Using Go from anywhere:

  ```bash
  go install go.miloapis.com/activity/cmd/activitytool@latest
  ```

## Validating Policies (`activitytool check`)

`check` loads `ActivityPolicy` manifests from YAML files, plain directories, or
Kustomize directories (including `kind: Component` and full service overlays;
non-`ActivityPolicy` resources are ignored), verifies API schema fields and
unique rule names, and compiles every CEL `match` and `summary` expression:

```bash
activitytool check examples/basic-kubernetes
activitytool check config/services/activity/policies
activitytool check -o json examples/basic-kubernetes
```

## Unit Testing Policies (`activitytool test`)

Write declarative test files ending in `.test.yaml` (or `.tests.yaml`,
`-test.yaml`, `-tests.yaml`) alongside your policies or in `test/policies/` or
`test/activity-policies/`:

```yaml
policy_files:
  - ../../examples/basic-kubernetes

tests:
  - name: deployment create by human links actor and deployment
    audit:
      verb: create
      user:
        username: alice@example.com
      objectRef:
        apiGroup: apps
        resource: deployments
        namespace: default
        name: web-frontend
    exp_rule: create
    exp_summary: "alice@example.com deployed web-frontend"
    exp_change_source: human
    exp_actor:
      type: user
      name: alice@example.com
      email: alice@example.com
    exp_links:
      - marker: alice@example.com
        kind: user
        name: alice@example.com
      - marker: web-frontend
        apiGroup: apps
        kind: Deployment
        namespace: default
        name: web-frontend

  - name: deployment read verb produces no activity
    policy: apps-deployments
    audit:
      verb: get
      user:
        username: alice@example.com
      objectRef:
        apiGroup: apps
        resource: deployments
        namespace: default
        name: web-frontend
    exp_no_match: true
```

Run tests with:

```bash
activitytool test --policies examples/basic-kubernetes test/policies
```

Add `--fail-on-untested` to fail when any loaded policy has zero test cases, or
`-o json` for machine-readable output.

## Adopting in Other Repositories (Taskfile + GitHub Actions)

### Taskfile example for local runs

Add this to your repository's `Taskfile.yaml`. It uses a local `activitytool`
binary if present and otherwise runs the pinned `ghcr.io/milo-os/activitytool`
Docker image (which Renovate keeps up to date automatically):

```yaml
version: "3"

vars:
  # renovate: datasource=docker depName=ghcr.io/milo-os/activitytool
  ACTIVITYTOOL_IMAGE: ghcr.io/milo-os/activitytool:v0.9.3

tasks:
  test-activity-policies:
    desc: Validate ActivityPolicy manifests and run unit tests
    cmds:
      - |
        if command -v activitytool >/dev/null 2>&1; then
          exec activitytool test --policies config/services/activity/policies test/activity-policies
        fi
        docker run --rm \
          -v "{{.ROOT_DIR}}:/work:ro" -w /work \
          "{{.ACTIVITYTOOL_IMAGE}}" \
          test --policies config/services/activity/policies test/activity-policies
```

### GitHub Actions CI

Call the reusable workflow from `datum-cloud/actions` in
`.github/workflows/test-activity-policies.yaml`:

```yaml
name: Activity Policy Tests

on:
  pull_request:
    paths:
      - "config/services/activity/**"
      - "test/activity-policies/**"
      - ".github/workflows/test-activity-policies.yaml"
  push:
    branches: [main]
    paths:
      - "config/services/activity/**"
      - "test/activity-policies/**"
      - ".github/workflows/test-activity-policies.yaml"

permissions:
  contents: read

jobs:
  test-activity-policies:
    uses: datum-cloud/actions/.github/workflows/test-activity-policies.yaml@<release-tag>
    with:
      policies-path: config/services/activity/policies
      tests-path: test/activity-policies
```

See `docs/activity-policies/README.md` in `datum-cloud/actions` for all workflow
inputs and options.
