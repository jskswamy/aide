# CI Status

Check GitHub Actions results for the current branch. Read-only — does
not run any checks locally.

## Step 1: Get current branch

Run:
~~~bash
git branch --show-current
~~~

## Step 2: Check for a PR

Run:
~~~bash
gh pr view --json number,url,headRefName 2>/dev/null
~~~

If this succeeds (exit 0), a PR exists. Extract the PR number and URL.
If it fails, there is no PR for this branch.

## Step 3: Get check results

**If a PR exists:**

Run:
~~~bash
gh pr checks
~~~

This lists all checks with their status, duration, and URL.

**If no PR exists:**

Query both CI workflows:
~~~bash
gh run list --branch <branch> --workflow ci.yml --limit 1 --json databaseId,status,conclusion
gh run list --branch <branch> --workflow security.yml --limit 1 --json databaseId,status,conclusion
~~~

If either returns results, get job details:
~~~bash
gh run view <id> --json jobs
~~~

If neither returns results, check if the branch exists on the remote:
~~~bash
git ls-remote --heads origin <branch>
~~~

## Step 4: Report

Format results as a summary table. Use this exact format.

When checks are available (PR path):

~~~
ci-status results (PR #42)
────────────────────────────────────────
  Lint                pass   (1m 2s)
  Test                pass   (1m 28s)
  Build               pass   (1m 1s)
  Security Scan       pass   (3m 6s)
  Vulnerability Check pass   (21s)
  CodeQL Analysis     pass   (2m 27s)
────────────────────────────────────────
RESULT: ALL CHECKS PASSED

URL: https://github.com/jskswamy/aide/pull/42
~~~

When checks are failing or pending:

~~~
ci-status results (PR #42)
────────────────────────────────────────
  Lint                FAIL   (1m 2s)
  Test                pass   (1m 28s)
  Build               pass   (1m 1s)
  Security Scan       pending
  Vulnerability Check pass   (21s)
  CodeQL Analysis     pending
────────────────────────────────────────
RESULT: 1 FAILED, 2 PENDING

URL: https://github.com/jskswamy/aide/pull/42
~~~

When branch is not pushed:

~~~
ci-status: no CI run found for branch 'feature/my-branch'

Branch not pushed to remote. Push first:
  git push -u origin feature/my-branch
~~~

When branch exists on remote but no CI run found:

~~~
ci-status: no CI run found for branch 'feature/my-branch'

Branch exists on remote but no workflow has run.
Create a PR to trigger CI:
  gh pr create
~~~

Map `gh pr checks` status values: "pass" → pass, "fail" → FAIL,
"pending" → pending, "skipping" → skip.
