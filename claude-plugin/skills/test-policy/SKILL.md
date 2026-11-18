---
name: test-policy
description: >
  Validate and unit-test ActivityPolicy manifests offline with activitytool
  (the ghcr.io/milo-os/activitytool Docker image in other repos, go run inside
  the activity repo), asserting rule matching, routing, exact generated English
  summaries, actor metadata, links, and no-match behavior, and wire policy
  testing into a repository's Taskfile and GitHub Actions CI. Use when
  authoring, editing, testing, or reviewing ActivityPolicy rules or setting up
  ActivityPolicy CI.
---

# Testing ActivityPolicy Resources Offline

Use `activitytool` to validate `ActivityPolicy` CEL rules and run declarative
unit tests (`*.test.yaml`) offline without a Kubernetes cluster.

## 1. Get a working `activitytool`

### Inside the `activity` repository
Use `task test:policies` or `go run ./cmd/activitytool <subcommand...>`.
`task test:policies:docker` runs the same tests in a locally built image.

### In every other repository: Docker image only
Run the tests in `ghcr.io/milo-os/activitytool`. Never use `go install` or
build from source in other repos.

1. Use the repo's Taskfile target if it has one: `task test-activity-policies`.
2. Otherwise run the image directly, mounting the git root read-only:
   ```bash
   root=$(git rev-parse --show-toplevel); prefix=$(git rev-parse --show-prefix)
   docker run --rm --network none --user 65532:65532 --read-only \
     --cap-drop ALL --security-opt no-new-privileges \
     --volume "$root:/work:ro" --workdir "/work/$prefix" \
     ghcr.io/milo-os/activitytool:<resolved-tag> <subcommand...>
   ```
   Paths may use `..` only within the repository.

### Resolve the tag; never invent one
```bash
gh release list -R milo-os/activity      # activitytool image tag
gh release list -R datum-cloud/actions   # reusable CI workflow tag
```
- Pin the release with its digest: `vX.Y.Z@sha256:<digest>`.
- Use the Activity release deployed where the policies run, so CEL and template
  functions match production.
- `v0.0.0-<branch>` images are dev builds. Don't use them in other repos.
- If the image can't be pulled, tell the user and stop. Never skip the tests or
  report them as passing.

## 2. Validate policies before writing tests

`activitytool check` accepts YAML files, plain directories, or Kustomize
directories (including `kind: Component` and overlays). Non-`ActivityPolicy`
resources are ignored, and files used as kustomize patches are not loaded as
standalone policies.

```bash
activitytool check config/services/activity/policies
activitytool check -o json path/to/policy.yaml
```

Fix every reported issue: schema errors, duplicate rule names, duplicate policy
`metadata.name` across files, CEL compile errors (e.g. `event.*` in
`auditRules`, unbalanced `{{ }}`, non-boolean `match`), and warnings about
misspelled kinds in `activity.miloapis.com`. Kustomize is rendered root-only and
offline: remote bases fail.

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
    exp_policy: iam.miloapis.com-group
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

Top-level keys (per YAML document):
- `policy_files` (or `rule_files`): paths, relative to the test file, to policy
  files, directories, or Kustomize directories.
- `resource_kinds`: optional map `apiGroup → resource → Kind` (`""` or `core`
  for the core group). Use it when a `kind-guessed` warning shows a wrong kind.
- `tests`: list of test cases.

A test file may hold several `---` documents; each is its own group with its
own `policy_files` and `resource_kinds`.

Test case input keys (set `name` and exactly one of `audit` or `event`):
- `name`: descriptive test case name.
- `policy`: optional policy `metadata.name` or `apiGroup/Kind`. When set, the
  test fails if the input doesn't route to that policy, and only that policy is
  evaluated. When omitted, every policy for the input's resource is evaluated
  in `metadata.name` order, first match wins, as in production.
- `audit`: `audit.k8s.io/v1` `Event` map (`verb`, `user`, `objectRef`,
  `responseStatus`, `requestObject`, `responseObject`). Needs `objectRef`.
- `event`: `events.k8s.io/v1` or `core/v1` Event map (`reason`, `type`, `note`,
  `reportingController`, `regarding` or legacy `involvedObject`,
  `metadata.annotations`).

Test case assertion keys (or nested under `exp_activity:`):
- `exp_no_match: true`: no rule matched. Combining it with any other
  assertion is a test error.
- `exp_policy`: the policy that matched (catches shadowing across policies).
- `exp_rule`: the rule `name` that matched (catches first-match-wins shadowing).
- `exp_summary`: exact expected English summary.
- `exp_summary_contains` / `exp_summary_regex`: partial checks when an exact
  match is inappropriate.
- `exp_change_source`: `"human"` or `"system"`.
- `exp_actor`: `{ type, name, uid, email }`.
- `exp_resource`: `{ apiGroup, apiVersion, kind, name, namespace, uid }`.
- `exp_tenant`: `{ type, name }`.
- `exp_links`: ordered list of `{ marker, apiGroup, kind, name, namespace, uid }`
  (or nested `resource:`).

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
5. **Intermediate `has()` guards**: cases where nested fields like `requestObject.spec.organizationRef` or `responseObject.status` are absent.
6. **Every rule at least once**: check the "rules matched by tests" summary, or use `--fail-on-untested-rules`.

## 4. Run tests and iterate

```bash
activitytool test --policies config/services/activity/policies test/activity-policies
activitytool test -o json --policies config/services/activity/policies test/activity-policies
```

Flags: `--fail-on-untested` (a policy has no tests), `--fail-on-untested-rules`
(a rule is never matched by a test), `-o json`, `--allow-empty`.

Use `-o json` when iterating. Per test, read `matchedPolicy`, `matchedRule`,
`actualSummary`, `failureDetails[]` (`field`, `expected`, `actual`), and
`warnings[]`. For the suite, read `untestedPolicies` and `untestedRules`.

Act on `warnings[].type`:
- `rule-skipped`: a rule errored or didn't compile and was skipped, as in
  production. Fix the expression; usually a missing `has()` guard.
- `shadowed`: an earlier policy would match this input first in production.
- `kind-guessed`: a link kind was guessed offline. Add `resource_kinds`.
- `no-target`: no policy routes this input.

### When a test fails
- Never copy `actualSummary` (or any actual value) into the expectation. That
  makes the test pass by definition and proves nothing.
- Derive the expected output from the policy's intent or the user's
  requirement, then decide whether the policy or the test is wrong before
  changing either.
- If the intended output is unclear, ask the user.
- Re-run until all tests pass. Report the final result, including any test you
  could not make pass.

### Known parity limit
Event-rule links reproduce what production emits today: only
`apiGroup`/`kind`/`name`/`namespace`/`uid` are copied from the link map, so an
object with only `apiVersion` gives an empty `apiGroup`. Assert that, not the
value you'd prefer. A follow-up change will fix production and the tool
together.

## 5. Set up Taskfile and GitHub Actions CI in a repository

For service repos (such as `milo` or a provider repo). Resolve tags first
(section 1).

### 1. Taskfile target (runs the image)

```yaml
version: "3"

vars:
  # Use the milo-os/activity release deployed where these policies run, pinned
  # with its digest: vX.Y.Z@sha256:<digest>.
  # renovate: datasource=docker depName=ghcr.io/milo-os/activitytool
  ACTIVITYTOOL_VERSION: <resolved-tag>@sha256:<digest>
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

If the repo has no tests yet, drop `{{.ACTIVITY_POLICY_TESTS}}`.

### 2. `.github/workflows/test-activity-policies.yaml`

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
    uses: datum-cloud/actions/.github/workflows/test-activity-policies.yaml@<resolved-actions-tag>
    with:
      policies-path: config/services/activity/policies
      tests-path: test/activity-policies
      activitytool-version: <resolved-tag>@sha256:<digest>
```

### 3. CI rules
- The workflow runs the image with the repository mounted read-only. Paths may
  use `..` only within the repository.
- Other inputs: `path` (default `.`), `fail-on-untested` (default `false`),
  `allow-empty` (default `false`). Without `tests-path`, CI still validates
  every policy and lists untested ones.
- Auto-discovery skips kustomize patch files. Still, set `policies-path`
  explicitly to the kustomization directory when overlays contain patches.
- Use the same `activitytool-version` in the Taskfile and the workflow.
- Before pushing, run `task test-activity-policies` locally and fix failures.
