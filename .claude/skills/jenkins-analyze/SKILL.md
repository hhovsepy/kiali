---
name: jenkins-analyze
description: Analyze Kiali Jenkins nightly pipeline results. Navigates the full pipeline hierarchy (build, install, test), classifies failures as env/infra issues or test failures, detects flakes by checking last 5 builds. Use when analyzing Jenkins nightly results or Slack failure notifications.
disable-model-invocation: false
---

# Jenkins Pipeline Analyze

Analyze a Kiali upstream nightly pipeline run → navigate child pipelines → classify failures → detect flakes → produce summary.

## What you need from the user

The **parent trigger build URL** — the user will provide it directly, e.g.:

```
https://jenkins-csb-servicemesh-master.dno.corp.redhat.com/job/kiali/job/main-pipelines/job/upstream-istio-pipeline-trigger/300/
```

The URL must include a build number. If only a job root URL is given (no build number), ask for a specific build.

The user may also paste a **Slack notification** instead, which contains child pipeline links and test counts. Extract child pipeline URLs from the embedded links. If no URLs (plain text), ask for the parent trigger build URL.

## Pipeline hierarchy

```
upstream-istio-pipeline-trigger #N
├── upstream-istio-pipeline (sidecars)
│   ├── build-kiali, build-ossmc, build-kiali-operator  [parallel]
│   ├── install-istio
│   ├── kiali-integration-tests  [sidecar only]
│   ├── kiali-cypress-tests
│   └── kiali-ossmc-tests
├── upstream-istio-pipeline (ambient)
│   ├── build-kiali, build-ossmc, build-kiali-operator  [parallel]
│   ├── install-istio
│   ├── kiali-cypress-tests  (ambient:junit suite)
│   └── kiali-ossmc-tests
├── upstream-istio-pipeline (sidecar-perf)
│   ├── build-*, install-istio
│   └── kiali-perf-tests
└── kiali-operator  [own OCP cluster]
    └── molecule tests via hack/ci-openshift-molecule-tests.sh
```

**Jenkins base URL:** `https://jenkins-csb-servicemesh-master.dno.corp.redhat.com/`

Requires VPN. If `curl` fails (connection refused / DNS error), ask user to connect to VPN or paste data manually.

## Step 1 — Verify accessibility

```bash
curl -k -s -o /dev/null -w "%{http_code}" "<parent-build-url>"
```

Non-200 → VPN required. Ask user to connect or paste console output / test report JSON.

## Step 2 — Discover child pipeline builds

### From parent trigger build

Get parent stages and overall result:

```bash
curl -k -s "<parent-build-url>api/json?tree=result,description,number" | jq '.'
curl -k -s "<parent-build-url>wfapi/describe" | \
  jq '[.stages[] | {name, status, durationMillis}]'
```

The parent description HTML contains `href="..."` links to each child build. Parse them:

```bash
curl -k -s "<parent-build-url>api/json?tree=description" | jq -r '.description'
```

Each link points to a child `upstream-istio-pipeline` or `kiali-operator` build, with `ok`/`failure` status inline.

### From Slack notification

Key signals from Slack text alone:
- **FAILURE + 0 test failures** → env/infra issue
- **ABORTED** → timeout or infrastructure abort
- **"No tests run"** → pre-test stage failed entirely
- **FAILURE + N failures** → actual test failures

## Step 3 — Analyze each child pipeline

For each child build URL:

### 3a — Identify failure point via pipeline stages

```bash
curl -k -s "<child-build-url>wfapi/describe" | \
  jq '[.stages[] | {name, status, durationMillis}]'
```

**Note:** Stage status uses `FAILED` (not `FAILURE`). Values: `SUCCESS`, `FAILED`, `NOT_EXECUTED`, `UNSTABLE`, `ABORTED`.

Stage names map to pipeline phases:
| Stage name | Phase |
|-----------|-------|
| `Get OCP cluster` | Cluster provisioning |
| `Login in Openshift` | Cluster auth |
| `Build Kiali` / `Build OSSMC` / `Build Kiali Operator` | Build |
| `Install Istio with kiali` | Install |
| `Run integration Tests` | Integration tests |
| `Run Cypress UI Tests` | Cypress tests |
| `Run OSSMC Tests` | OSSMC tests |
| `Run Performance Tests` | Perf tests |

### 3b — Find sub-build (test job) URLs

Test results live at the **individual test job level**, not the pipeline level. Get sub-build URLs from the child pipeline description:

```bash
curl -k -s "<child-build-url>api/json?tree=description" | jq -r '.description'
```

Parse `href="..."` links. Each points to a test job build like:
- `kiali/test-jobs/kiali-cypress-tests/<N>/`
- `kiali/test-jobs/kiali-ossmc-tests/<N>/`
- `kiali/test-jobs/kiali-integration-tests/<N>/`
- `kiali/test-jobs/kiali-operator/<N>/`

### 3c — Get test results from each test job

For each test job build URL, get overall counts and failures:

```bash
# Overall counts
curl -k -s "<test-job-url>api/json?tree=result" | jq -r '.result'
curl -k -s "<test-job-url>testReport/api/json" | jq '{failCount, passCount, skipCount}'

# Failed test details (field is errorDetails, NOT errorMessage)
curl -k -s "<test-job-url>testReport/api/json" | \
  jq '[.suites[].cases[] | select(.status == "FAILED" or .status == "REGRESSION") |
    {name, status, className, errorDetails: (.errorDetails | if . then (.[0:200]) else null end)}]'
```

### 3d — Get console log tail (for env issues)

```bash
curl -k -s "<test-job-url>consoleText" | tail -100
```

## Step 4 — Classify failures

### Decision tree

```
Test job result?
├── SUCCESS → skip
├── ABORTED + 0 test failures → env: timeout (tests may have all passed)
├── FAILURE + 0 test failures
│   ├── console shows "script returned exit code 1" → env: runner-exit
│   └── check which pipeline stage failed → env: <stage>
└── FAILURE/UNSTABLE + N test failures → classify each test (Step 4b)
```

**Critical pattern: ABORTED with 0 failures.** Jobs can be ABORTED by the parent pipeline (cluster lifetime exceeded) after all tests passed. Check `passCount` — if tests ran and passed, this is an env timeout, not a test issue.

### 4a — Env/infra issue patterns

| Signal | Classification |
|--------|---------------|
| `Get OCP cluster` stage failed | `env: cluster-provision` |
| `Login in Openshift` stage failed | `env: cluster-auth` |
| Build stage failed | `env: build` |
| `Install Istio with kiali` stage failed | `env: install` |
| Job ABORTED + tests all passed | `env: timeout` |
| `script returned exit code 1` + 0 test failures | `env: runner-exit` |
| `error: You must be logged in to the server` | `env: cluster-auth` |
| `Unable to connect to the server` | `env: cluster-down` |
| `context deadline exceeded` | `env: timeout` |
| `No space left on device` / `OOMKilled` | `env: infra` |

### 4b — Test failure error patterns

| Error pattern | Likely cause |
|--------------|-------------|
| `ECONNREFUSED`, `502`, `504` on API calls | `env: cluster` — pod unhealthy |
| `Timed out retrying after 40000ms: Expected to find element` | `test-failure` or `env: slow-cluster` |
| Assertion mismatch (expected X got Y) | `test-failure` |
| `Cannot read properties of undefined` | `test-failure` |

## Step 5 — Flake detection (last 5 builds)

For each test failure, check recent builds of the **same test job**.

### 5a — Get 5 builds up to and including the failing build

The `builds[number,result]` tree filter does NOT include `result` at the job listing level. Fetch all build numbers, then query each individually:

```bash
# Get build numbers <= current build
curl -k -s "<test-job-url>api/json" | \
  jq --argjson current <build-number> '[.builds[].number | select(. <= $current)] | .[0:5]'
```

### 5b — Check each build for the same failure

```bash
for build in <build1> <build2> ...; do
  echo "=== #$build ==="
  result=$(curl -k -s "<test-job-url>/$build/api/json?tree=result" | jq -r '.result')
  echo "Job result: $result"
  curl -k -s "<test-job-url>/$build/testReport/api/json" | \
    jq -r --arg name "<scenario-name>" \
    '[.suites[].cases[] | select(.name | test($name; "i"))] |
     if length > 0 then
       "\(length) total, \([.[] | select(.status == "FAILED" or .status == "REGRESSION")] | length) failed"
     else "not found" end'
done
```

### 5c — Check DATA_PLANE_MODE for context

Test jobs are shared between sidecar and ambient runs. A scenario may pass in sidecar but consistently fail in ambient. Check the build parameter to filter:

```bash
curl -k -s "<test-job-url>/<build>/api/json" | \
  jq -r '[.actions[] | select(.parameters?) | .parameters[] |
    select(.name == "DATA_PLANE_MODE")] | first | .value // "not set"'
```

If the failing build is ambient mode, prioritize ambient-mode history for flake classification.

### 5d — Classify flake status

| Pattern (last 5 same-mode builds) | Status |
|------------------------------------|--------|
| `✗ ✓ ✓ ✓ ✓` | `new-failure` — first occurrence |
| `✗ ✓ ✗ ✓ ✓` | `flake` — intermittent |
| `✗ ✗ ✗ ✓ ✗` or `✗ ✗ ✗ ✗ ✓` | `likely-persistent` — 3-4 of 5 |
| `✗ ✗ ✗ ✗ ✗` | `persistent` — real bug |

## Step 6 — Produce summary

```
# Nightly Pipeline Analysis — Build #<N>

## Overview
| Pipeline | Result | Tests | Failures | Classification |
|----------|--------|-------|----------|----------------|
| Sidecars | FAILURE | 534 | 0 | env: timeout |
| Ambient | FAILURE | 118 | 6 | test-failure (OSSMC waypoint) |
| Sidecar-Perf | SUCCESS | 4 | 0 | — |
| Operator | ABORTED | 21 | 1 | test-failure (persistent) |

## Environment Issues

### <Pipeline> — <classification>
- **Failed stage/job:** <stage or job name>
- **Error:** <one-line from console>
- **Impact:** <tests passed but job killed / tests never ran>

## Test Failures

### <Group or scenario>
- **Pipeline:** <Ambient / Sidecars>
- **Scenarios:** <list if grouped by root cause>
- **Error:** <assertion error>
- **Flake status:** <persistent (5/5) | likely-persistent (4/5) | flake | new-failure>
- **History:** ✗ ✓ ✗ ✗ ✗ (builds #N, #N-1, #N-2, #N-3, #N-4)
- **Mode context:** <ambient-only / sidecar-only / both>

## Summary
- **Env issues:** N pipelines affected
- **Test failures:** N total (M persistent, K likely-persistent, J flakes, L new)
- **Action needed:** <prioritized list>
```

## VPN fallback

If Jenkins is unreachable:
1. Work with Slack notification text for pipeline overview
2. Ask user to paste console log of failing child pipelines
3. Ask user to paste test report JSON (`<build-url>testReport/api/json`)
4. Skip flake history check — note: "Flake detection skipped — Jenkins not accessible"
5. Classify from available data only
