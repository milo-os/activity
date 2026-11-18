# Testing Activity Policies Offline

`activitytool` validates `ActivityPolicy` manifests and runs declarative unit
tests (`*.test.yaml`) offline without requiring a Kubernetes cluster — similar to
`promtool` for Prometheus alerting rules. It evaluates rules the same way the
Activity processor does: same CEL environments, same routing (first matching
policy wins), and rules that error are skipped as in production.

## Running `activitytool`

### In other repositories: the container image

Other repositories run `activitytool` only from the
`ghcr.io/milo-os/activitytool` image (`linux/amd64`, `linux/arm64`), locally
and in CI.

- **Version:** use the `milo-os/activity` release deployed where your policies
  run, so CEL and template functions match production. Pin it with its digest:
  `vX.Y.Z@sha256:<digest>`. Find releases with
  `gh release list -R milo-os/activity`.
- **Supported tags:** only release tags (`vX.Y.Z`). Images are also published
  for every branch as `v0.0.0-<branch>` dev builds; those are not supported for
  other repositories.

Mount the repository read-only and run offline:

```bash
root=$(git rev-parse --show-toplevel); prefix=$(git rev-parse --show-prefix)
docker run --rm --network none --user 65532:65532 --read-only \
  --cap-drop ALL --security-opt no-new-privileges \
  --volume "$root:/work:ro" --workdir "/work/$prefix" \
  ghcr.io/milo-os/activitytool:vX.Y.Z@sha256:<digest> \
  test --policies config/services/activity/policies test/activity-policies
```

Mounting the git root lets paths use `..` within the repository.

### In this repository

```bash
task test:policies          # go run ./cmd/activitytool test ...
task test:policies:docker   # same tests in a locally built image
task build:activitytool     # writes ./bin/activitytool
```

## Validating Policies (`activitytool check`)

`check` loads `ActivityPolicy` manifests from YAML files, plain directories, or
Kustomize directories (including `kind: Component` and full service overlays),
then validates every policy and reports all problems in one run:

- API schema and required fields, unique rule names (the same validation the
  API server runs).
- Unique policy `metadata.name` across all sources. Identical copies of the
  same policy are merged; differing ones are an error naming both files.
- Every CEL `match` and `summary` expression compiles, using only the variables
  available to its rule type (audit or event).
- Warnings for documents in `activity.miloapis.com` with an unexpected or
  wrong-case kind (e.g. `ActivtyPolicy`).

Non-`ActivityPolicy` resources are ignored. Files a kustomization uses as
patches are not loaded as standalone policies. Kustomize is rendered with
root-only load restrictions, plugins disabled, and no network: remote bases
fail.

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
    exp_policy: apps-deployments
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

### Test file reference

Top-level keys (per YAML document):

| Key | Description |
|---|---|
| `policy_files` (or `rule_files`) | Policy files, directories, or Kustomize directories, relative to the test file |
| `resource_kinds` | Optional `apiGroup → resource → Kind` map (`""` or `core` for the core group). Overrides offline kind guessing for links |
| `tests` | List of test cases |

A file may contain several `---` documents. Each is its own group with its own
`policy_files` and `resource_kinds`.

Test case keys:

| Key | Description |
|---|---|
| `name` | Test case name |
| `audit` / `event` | Exactly one input: an `audit.k8s.io/v1` Event (needs `objectRef`), or an `events.k8s.io/v1` / `core/v1` Event |
| `policy` | Optional policy `metadata.name` or `apiGroup/Kind`. See routing below |
| `exp_no_match` | `true` asserts no rule matched. Combining it with any other assertion is a test error |
| `exp_policy` | Policy that matched |
| `exp_rule` | Rule `name` that matched |
| `exp_summary`, `exp_summary_contains`, `exp_summary_regex` | Exact, partial, or regex summary checks |
| `exp_change_source` | `human` or `system` |
| `exp_actor` | `{ type, name, uid, email }` |
| `exp_resource` | `{ apiGroup, apiVersion, kind, name, namespace, uid }` |
| `exp_tenant` | `{ type, name }` |
| `exp_links` | Ordered `{ marker, apiGroup, kind, name, namespace, uid }` list |

Assertions can also be nested under `exp_activity:` (e.g. `exp_activity.policy`).

### Routing and evaluation

- **Without `policy:`:** every loaded policy for the input's resource is
  evaluated in `metadata.name` order, and the first match wins, as in
  production. Use `exp_policy` to catch one policy shadowing another.
- **With `policy:`:** the test fails if the input doesn't route to that policy.
  Only that policy is evaluated, and a `shadowed` warning is reported if an
  earlier policy would match first. A reference that matches several policies
  is an error.
- **Skipped rules:** an event rule whose `match` errors, or a rule that failed to
  compile, is skipped with a `rule-skipped` warning, as production skips it. An
  audit rule runtime error or an event summary error fails the test.
- **Event links** match what production emits today: only `apiGroup`, `kind`,
  `name`, `namespace` and `uid` are copied from the link map, so an object with
  only `apiVersion` produces an empty `apiGroup`.

### Running tests

```bash
activitytool test --policies examples/basic-kubernetes test/policies
```

| Flag | Effect |
|---|---|
| `--fail-on-untested` | Fail when a loaded policy has no tests |
| `--fail-on-untested-rules` | Fail when a rule is never matched by a test |
| `--allow-empty` | Pass with a warning when nothing is found |
| `-o json` | Machine-readable output |

Text output prints each failure with the matched policy and rule (or
`no match; evaluated …`), any warnings under each test, and a summary that
includes "X/Y rules matched by tests".

JSON output includes, per test: `matchedPolicy`, `matchedRule`,
`actualSummary`, `evaluatedPolicies`, `failures` (strings), `failureDetails`
(`field`, `expected`, `actual`, `message`) and `warnings` (`type`, `policy`,
`ruleType`, `rule`, `expression`, `message`). Warning types are
`rule-skipped`, `kind-guessed`, `shadowed` and `no-target`. The suite includes
`untestedPolicies`, `untestedRules` and `testedRules`.

## Adopting in Other Repositories (Taskfile + GitHub Actions)

### Taskfile

Add this to your repository's `Taskfile.yaml`. It runs the pinned image, which
Renovate keeps up to date.

```yaml
version: "3"

vars:
  # Use the milo-os/activity release deployed where these policies run, so CEL
  # and template functions match production, pinned with its digest.
  # renovate: datasource=docker depName=ghcr.io/milo-os/activitytool
  ACTIVITYTOOL_VERSION: vX.Y.Z@sha256:<digest>
  ACTIVITY_POLICIES: config/services/activity/policies
  ACTIVITY_POLICY_TESTS: test/activity-policies

tasks:
  test-activity-policies:
    desc: Validate ActivityPolicy manifests and run unit tests in the activitytool image
    cmds:
      - |
        set -eu
        root=$(git rev-parse --show-toplevel)
        prefix=$(git rev-parse --show-prefix)
        docker run --rm --network none --user 65532:65532 --read-only \
          --cap-drop ALL --security-opt no-new-privileges \
          --volume "$root:/work:ro" --workdir "/work/$prefix" \
          "ghcr.io/milo-os/activitytool:{{.ACTIVITYTOOL_VERSION}}" \
          test --policies {{.ACTIVITY_POLICIES}} {{.ACTIVITY_POLICY_TESTS}}
```

### GitHub Actions CI

Call the reusable workflow from `datum-cloud/actions` in
`.github/workflows/test-activity-policies.yaml`, pinned to a release tag:

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
      activitytool-version: vX.Y.Z@sha256:<digest>
```

Use the same `activitytool-version` as your Taskfile. See
`docs/activity-policies/README.md` in `datum-cloud/actions` for all workflow
inputs.
