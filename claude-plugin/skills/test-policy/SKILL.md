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

## 1. Get a working `activitytool`

### Resolve versions first

- Never invent a tag. Use only tags you have listed:
  ```bash
  gh release list -R milo-os/activity      # activitytool image / go install tag
  gh release list -R datum-cloud/actions   # reusable CI workflow tag
  # without gh:
  git ls-remote --tags https://github.com/milo-os/activity
  git ls-remote --tags https://github.com/datum-cloud/actions
  ```
- The tag must be a release that contains `activitytool` (older releases do
  not ship it). If none does, say so and use a fallback below.
- When the Activity version deployed in the target environment is known, use
  the same `activitytool` tag so CEL behaviour matches production.

### Try in this order; use the first that works

1. Existing binary: `activitytool` on `PATH`, or `./bin/activitytool`.
2. Taskfile target, if the repo defines it: `task test-activity-policies`
   (service repos) or `task test:policies` (activity repo).
3. Docker image at a resolved tag:
   ```bash
   docker run --rm -v "$PWD:/work:ro" -w /work \
     ghcr.io/milo-os/activitytool:<resolved-tag> <subcommand...>
   ```
   Only `$PWD` is mounted: keep every path relative to it (no `..`, no host
   absolute paths).
4. `go install go.miloapis.com/activity/cmd/activitytool@<resolved-tag>`
5. Inside the `activity` repo: `go run ./cmd/activitytool <subcommand...>`
   (or `task build:activitytool`, which writes `./bin/activitytool`).

If every option fails, tell the user what failed and why. Never silently skip
running the tests or report them as passing.

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

### Test file rules
- One YAML document per test file. Only the first `---` document is read.
- Do not combine `exp_no_match: true` with other assertions; it silently
  overrides them. Write a separate case instead.
- Policy `metadata.name` must be unique across all loaded policy files.
- With `policy:` set, the tool does not check that `audit.objectRef` /
  `event.regarding` matches the policy's `spec.resource`. Make inputs target
  the right resource yourself.

### Realistic inputs
When the `activity` MCP server is available, base inputs on real data:
`query_audit_logs` (audit rules) or `query_events` (event rules), filtered to
the policy's resource. Trim each sample to the fields the rules read, and
remove secrets, tokens, real emails, IPs, and other sensitive values.

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
  ghcr.io/milo-os/activitytool:<resolved-tag> \
  test -o json --policies config/services/activity/policies test/activity-policies
```

Use `-o json` to inspect structured `failures`, `actualSummary`, `matchedRule`,
and `untestedPolicies` when iterating automatically.

### When a test fails
- Do not copy `actualSummary` (or any actual value) into the expectation.
  That makes the test pass by definition and proves nothing.
- Derive the expected output from the policy's intent or the user's
  requirement, then decide whether the policy or the test is wrong before
  changing either.
- If the intended output is unclear, ask the user.
- Re-run until all tests pass. Report the final result, including any test you
  could not make pass.

### Known parity limits (a pass does not prove these)
The tool can differ from the production processor:
- Event rules: link `apiGroup`/`apiVersion` derived from `apiVersion` may be
  filled in by the tool but empty in production.
- Event rules: in production a `match` error is logged and evaluation moves to
  the next rule; the tool fails the test. Guard optional fields with `has()`.
- Only one policy is evaluated per test case, so shadowing across different
  policies for the same resource is not detected.
- With an explicit `policy:`, the input's resource is not checked against
  `spec.resource` (see Test file rules).

## 5. Set up Taskfile and GitHub Actions CI in a repository

When setting up policy testing in a service repository (such as `milo` or a provider repo):

### 1. Add the local `Taskfile.yaml` target

Use the `ghcr.io/milo-os/activitytool` image with a `# renovate:` comment so
Renovate keeps the image tag updated, while letting developers with a local
`activitytool` binary run it natively:

```yaml
vars:
  # renovate: datasource=docker depName=ghcr.io/milo-os/activitytool
  ACTIVITYTOOL_IMAGE: ghcr.io/milo-os/activitytool:<resolved-tag>

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

Call the reusable workflow in `datum-cloud/actions`, pinned to a tag resolved in step 1 (never a branch or an invented tag):

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
    uses: datum-cloud/actions/.github/workflows/test-activity-policies.yaml@<resolved-tag>
    with:
      policies-path: config/services/activity/policies
      tests-path: test/activity-policies
```

If the repository does not have tests yet, omit `tests-path` (or set `fail-on-untested: false`, the default) so CI still validates every `ActivityPolicy` manifest and lists untested ones.

### 3. CI rules
- The workflow runs `activitytool` in Docker with only the checkout mounted.
  `path`, `policies-path`, `policy_files`, and kustomize bases must not reach
  outside it with `..`.
- Auto-discovery can pick up kustomize patch files (`kind: ActivityPolicy` with
  a partial spec) as policies. When overlays contain patches, set
  `policies-path` explicitly to the kustomization directory.
- Other inputs: `path` (default `.`), `fail-on-untested` (default `false`),
  `allow-empty` (default `false`), `activitytool-version` (override the
  pinned tool tag; use a resolved tag).
- Before pushing, run the same check locally: the Taskfile target, or the
  exact `activitytool test --policies <policies-path> <tests-path>` command
  that CI runs. Fix failures before opening the PR.
