# AI Policy

## AI is Welcome Here

aide is built with AI assistance. Maintainers use AI tools daily — for writing code, generating tests, drafting documentation, and exploring ideas. This policy is not anti-AI.

It exists because low-effort, unreviewed AI output creates real burden for maintainers and reviewers. The problem is people using AI poorly, not the tools themselves. A well-crafted contribution that used AI throughout is welcome. A sloppy dump of raw model output is not.

## Rules for AI-Assisted Contributions

### 1. All AI usage must be disclosed

State the tool you used (Claude Code, Cursor, Copilot, etc.) and the extent of assistance. Examples:

- "Generated initial implementation with Claude Code, then manually edited for correctness."
- "Used Copilot for test boilerplate."
- "Drafted with AI, rewrote the error handling and tests by hand."

A one-line note in the PR description is enough. No lengthy justifications needed.

### 2. You must fully understand every line you submit

Code, diagrams, documentation — all of it. If you cannot explain what a piece of code does and how it interacts with the rest of the system without asking an AI, do not submit it.

This applies equally to a three-line bug fix and a five-hundred-line feature. Understanding is not optional.

### 3. AI-generated text must be reviewed and edited before submission

This covers issues, discussions, PR descriptions, and comments — not just code.

AI produces verbose, hedge-filled prose. It qualifies everything, repeats itself, and buries the point. Trim it. Rewrite it in your voice. Make it yours. Reviewers can tell the difference.

## There are Humans Here

This project uses a two-stage review pipeline. Automated AI review (e.g., Greptile) runs first. Human review follows.

If your contribution cannot pass automated review, it never reaches a human. Fix the feedback and resubmit.

When it does reach a human, that human's time is scarce. Every PR a maintainer reviews is time not spent writing code, fixing bugs, or helping other contributors. Submitting unreviewed AI output — code you have not tested, prose you have not read, changes you do not understand — wastes that time.

This is not about gatekeeping. It is about respect for the people who maintain this project. They volunteered their time. Honor that by doing your part before asking for theirs.

## What Happens

**Contributions that fail automated review:** Fix the issues raised and resubmit. This is normal and expected.

**Contributions that pass automated review but are clearly unreviewed AI output:** Closed with constructive feedback explaining what needs to change. You are welcome to revise and resubmit.

**Repeat offenders:** Contributors who repeatedly submit low-effort, unreviewed AI output may be blocked from contributing. This is a last resort, not a first response.

## Maintainer Exemption

Maintainers who have demonstrated understanding of the codebase may use AI tools at their discretion. They have earned trust through sustained, quality contributions and ongoing accountability for what they ship.
