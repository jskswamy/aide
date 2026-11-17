# PR Review

Fetch and triage Greptile automated code review feedback and CI status for the active feature branch Pull Request.

## Step 1: Identify Current Branch and Open PR

Run:
~~~bash
git branch --show-current
gh pr view --json number,url,title,headRefName,baseRefName,mergeStateStatus
~~~

If `gh pr view` fails because no PR exists for the current branch, prompt the user to push the branch and open a PR first:
~~~bash
git push -u origin "$(git branch --show-current)"
gh pr create --fill
~~~

## Step 2: Fetch Greptile Review Summary and Inline Comments

Query both PR issue comments (where Greptile posts its `Threat-Model Sweep` and summary report) and inline pull request review comments:

~~~bash
PR_NUMBER=$(gh pr view --json number --jq .number)
REPO=$(gh repo view --json nameWithOwner --jq .nameWithOwner)

gh api "repos/${REPO}/issues/${PR_NUMBER}/comments" \
  --jq '.[] | {id: .id, user: .user.login, updated_at: .updated_at, body: .body}'

gh api "repos/${REPO}/pulls/${PR_NUMBER}/comments" \
  --jq '.[] | {id: .id, user: .user.login, path: .path, line: .line, body: .body}'
~~~

## Step 3: Triage Findings Against the Codebase

Parse Greptile's review output into:
1. **`## Threat-Model Sweep`**: Check the threat score (`0-5`), any unvalidated inputs (`Validated? no`), and any open threat vectors (`V1`, `V2`, ...).
2. **`### Must fix` (Blockers)**: Bugs, sandbox/credential security threats, data loss, races, or resource leaks.
3. **`### Should fix` (Majors)**: Missing tests on risky paths, missing documentation updates (`docs/*.md`, `README.md`, `DESIGN.md`), helper duplication, or dead code.
4. **`### Nice to have` (Nits)**: Minor idiomatic cleanups or small simplifications.

For each finding:
- Read the referenced `file:line` and callers in the working tree to confirm technical validity before changing code.
- If a finding is valid, apply the smallest complete fix (and add/update tests and documentation in the same commit).
- If a finding is a false positive, explain why with a concrete `file:line` citation so the maintainer can resolve the review thread.

## Step 4: Report Summary

Output a concise triage report:

~~~text
pr-review summary (PR #<number>)
────────────────────────────────────────
Threat-Model Score: <X>/5 (<Label>)
Confidence Score:   <Y>/5

Findings:
  [Must fix]   #1 <title> (path/file.go:L42) -> VALID / FALSE POSITIVE
  [Should fix] #2 <title> (docs/cli-reference.md) -> VALID / FALSE POSITIVE
────────────────────────────────────────
Next step:
  - Apply fixes, fold into target commits (git commit --fixup / git rebase -i --autosquash origin/main), and push
  - Or rebase-merge once CI and review pass: gh pr merge --rebase --delete-branch
~~~
