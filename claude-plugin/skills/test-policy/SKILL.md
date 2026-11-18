---
name: test-policy
description: >
  Validate and unit-test ActivityPolicy manifests offline with activitytool
  (via the ghcr.io/milo-os/activitytool Docker image or local binary),
  asserting rule matching, exact generated English summaries, actor metadata,
  links, and no-match behavior, and wire policy testing into a repository's
  Taskfile and GitHub Actions CI. Use when authoring, editing, testing, or
  reviewing ActivityPolicy rules or setting up ActivityPolicy CI.
---

# Testing ActivityPolicy Resources Offline

Use `activitytool` to validate `ActivityPolicy` CEL rules and run declarative
unit tests (`*.test.yaml`) offline without a Kubernetes cluster.

## 1. Run `activitytool` (Docker image or local binary)

Check if `activitytool` is installed locally (`command -v activitytool` or
`./bin/activitytool`):

- **Docker image (recommended in service repos & CI):**
  ```bash
  docker run --rm -v "$PWD:/work:ro" -w /work \
    ghcr.io/milo-os/activitytool:<version> <subcommand...>
  ```
  Note: when running inside Docker, keep all policy and test paths relative to
  the mounted repository root (no `..` escaping `/work` and no host absolute
  paths).
- **Inside the `activity` repository:** `task build:activitytool` (or `go run ./cmd/activitytool`).
- **In any repo with `Taskfile.yaml` wired:** `task test-activity-policies`.
- **Direct Go install:** `go install go.miloapis.com/activity/cmd/activitytool@latest`.

## 2. Validate policies before writing tests

`activitytool check` accepts raw YAML files, plain directories, or Kustomize
directories (including `kind: Component` and full deployment overlays — non-`ActivityPolicy`
resources are ignored):

```bash
activitytool check config/services/activity/policies
activitytool check -o json path/to/policy.yaml
```

Fix every schema error, duplicate rule name, or CEL compile error (e.g.
undeclared `event.*` in `auditRules`, unbalanced `{{ }}`, non-boolean `match`).

## 3. Write unit tests (`*.test.yaml`)

Keep test files next to policies or under `test/activity-policies/` (matching
`*.test.yaml`, `*.tests.yaml`, `*-test.yaml`, or `*-tests.yaml`).

```yaml
policy_files:
  - ../../config/services/activity/policies

tests:
  - name: create group renders expected english summary and link
    audit:
      verb: create
      user:
        username: alice@example.com
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: platform-admins
      responseStatus:
        code: 201
      responseObject:
        apiVersion: iam.miloapis.com/v1alpha1
        kind: Group
        metadata:
          name: platform-admins
          uid: grp-123
    exp_rule: create
    exp_summary: "alice@example.com created group platform-admins"
    exp_change_source: human
    exp_actor:
      type: user
      name: alice@example.com
      email: alice@example.com
    exp_links:
      - marker: platform-admins
        apiGroup: iam.miloapis.com
        kind: Group
        name: platform-admins

  - name: system actor create produces no activity
    policy: iam.miloapis.com-group
    audit:
      verb: create
      user:
        username: system:serviceaccount:kube-system:controller
      objectRef:
        apiGroup: iam.miloapis.com
        resource: groups
        name: platform-admins
      responseStatus:
        code: 201
    exp_no_match: true

  - name: suspended event renders english summary
    event:
      reason: Suspended
      type: Warning
      reportingController: project-controller
      regarding:
        apiVersion: resourcemanager.miloapis.com/v1alpha1
        kind: Project
        name: prod-app
    exp_rule: suspended
    exp_summary: "Project prod-app was suspended"
    exp_change_source: system
```

### Schema reference

Top-level keys:
- `policy_files` (or `rule_files`): list of paths (relative to the test file)
  pointing to policy files, directories, or Kustomize directories.
- `tests`: list of test cases.

Test case input keys (set `name` and exactly one of `audit` or `event`):
- `name`: descriptive test case name.
- `policy`: optional policy `metadata.name` or `apiGroup/Kind` (auto-detected
  from `audit.objectRef` or `event.regarding` when omitted).
- `audit`: Kubernetes `audit.k8s.io/v1` `Event` map (`verb`, `user`,
  `objectRef`, `responseStatus`, `requestObject`, `responseObject`).
- `event`: Kubernetes `events.k8s.io/v1` or `core/v1` Event map (`reason`,
  `type`, `note`, `reportingController`, `regarding` or legacy `involvedObject`,
  `metadata.annotations`).

Test case assertion keys (or nested under `exp_activity:`):
- `exp_no_match: true`: asserts that no rule matched (mutually exclusive with positive assertions).
- `exp_rule`: exact matched rule `name` (catches first-match-wins rule shadowing).
- `exp_summary`: exact expected English summary string.
- `exp_summary_contains` / `exp_summary_regex`: partial summary checks when exact match is inappropriate.
- `exp_change_source`: `"human"` or `"system"`.
- `exp_actor`: `{ type, name, uid, email }`.
- `exp_resource`: `{ apiGroup, apiVersion, kind, name, namespace, uid }`.
- `exp_tenant`: `{ type, name }`.
- `exp_links`: ordered list of `{ marker, apiGroup, kind, name, namespace, uid }` (or nested `resource:`).

### What to always test for each policy
1. **Exact English summary (`exp_summary`)** for every rule, verifying grammar, spacing, and fallback branches when optional fields (like `requestObject.spec` or `responseObject.metadata.name`) are missing.
2. **Rule ordering (`exp_rule`)** when specific rules precede generic ones (e.g., `evict` before `create`, `scale` before `update`, `accepted` before `update`).
3. **Negative cases (`exp_no_match: true`)** for `system:*` actors, read verbs (`get`, `list`, `watch`), or ignored subresources (`status`).
4. **Links (`exp_links`)** whenever `link(...)` is used in a summary template.
5. **Intermediate `has()` guards** — test cases where nested fields like `requestObject.spec.organizationRef` or `responseObject.status` are absent so unguarded CEL paths fail during unit tests instead of crashing in production.

## 4. Run tests and iterate

```bash
activitytool test --policies config/services/activity/policies test/activity-policies
activitytool test --policies config/services/activity/policies --fail-on-untested test/activity-policies
activitytool test -o json --policies config/services/activity/policies test/activity-policies
```

Or via Docker:

```bash
docker run --rm -v "$PWD:/work:ro" -w /work \
  ghcr.io/milo-os/activitytool:<version> \
  test -o json --policies config/services/activity/policies test/activity-policies
```

Use `-o json` to inspect structured `failures`, `actualSummary`, `matchedRule`,
and `untestedPolicies` when iterating automatically.

## 5. Set up Taskfile and GitHub Actions CI in a repository

When setting up policy testing in a service repository (such as `milo` or a provider repo):

### 1. Add the local `Taskfile.yaml` target

Use the `ghcr.io/milo-os/activitytool` image with a `# renovate:` comment so
Renovate keeps the image tag updated, while letting developers with a local
`activitytool` binary run it natively:

```yaml
vars:
  # renovate: datasource=docker depName=ghcr.io/milo-os/activitytool
  ACTIVITYTOOL_IMAGE: ghcr.io/milo-os/activitytool:<version>

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

(If the repo only validates policies without tests yet, omit `test/activity-policies` or pass `test/activity-policies` once created.)

### 2. Add `.github/workflows/test-activity-policies.yaml`

Call the reusable workflow in `datum-cloud/actions` pinned to a release tag:

```yaml
name: Activity Policy Tests

on:
  pull_request:
    paths:
      - 'config/services/activity/**'
      - 'test/activity-policies/**'
      - '.github/workflows/test-activity-policies.yaml'
  push:
    branches: [main]
    paths:
      - 'config/services/activity/**'
      - 'test/activity-policies/**'
      - '.github/workflows/test-activity-policies.yaml'

permissions:
  contents: read

jobs:
  test-activity-policies:
    uses: datum-cloud/actions/.github/workflows/test-activity-policies.yaml@<release-tag>
    with:
      policies-path: config/services/activity/policies
      tests-path: test/activity-policies
```

If the repository does not have tests yet, omit `tests-path` (or set `fail-on-untested: false`, the default) so CI still validates every `ActivityPolicy` manifest and lists untested ones.
