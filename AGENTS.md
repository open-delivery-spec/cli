# Development Rules

## Conversational Style

- Keep answers short and concise
- No emojis in commits, issues, PR comments, or code
- No fluff or cheerful filler text
- Technical prose only, be kind but direct (e.g., "Thanks @user" not "Thanks so much @user!")
- **Language**: Always reply in the same language the user writes in. If the user writes in Chinese, respond in Chinese.

## Test 

- All new features must have unit test coverage. If you add a new public method, add tests for it.
- If you fix a bug, add a test that reproduces the bug before fixing it, then verify the test fails, then implement the fix, and verify the test passes.`

## Feature Documentation

Complex features (e.g., AutoFix, usage quotas) must have a corresponding markdown doc under `docs/`. When you add or significantly update such a feature, keep the doc in sync. Existing feature docs:

- `docs/auto-fix.md` — experimental auto-fix feature
- `docs/usage-quota.md` — per-provider/model request quotas

## Branching Rules

- **NEVER push directly to `main`.** All code must enter `main` via pull request (PR) only. Even when working alone, create a feature branch and open a PR — do not commit or push directly to `main`.

## PR Workflow

- Analyze PRs without pulling locally first
- If the user approves: create a feature branch, pull PR, rebase on main, apply adjustments, commit, merge into main, push, close PR, and leave a comment
- **Never open PRs yourself.** Work in feature branches until everything meets requirements, then merge into main and push.
- All PRs target `main` branch

## Git Rules for Parallel Agents

Multiple agents may work on different files in the same worktree simultaneously. You MUST follow these rules:

### Committing

- **ONLY commit files YOU changed in THIS session**
- NEVER use `git add -A` or `git add .` — these sweep up changes from other agents
- ALWAYS use `git add <specific-file-paths>` listing only files you modified
- Before committing, run `git status` and verify you are only staging YOUR files
- Track which files you created/modified/deleted during the session

### Forbidden Git Operations

These commands can destroy other agents' work:

- `git reset --hard` — destroys uncommitted changes
- `git checkout .` — destroys uncommitted changes
- `git clean -fd` — deletes untracked files
- `git stash` — stashes ALL changes including other agents' work
- `git add -A` / `git add .` — stages other agents' uncommitted work
- `git push --force` / `git push -f` — overwrites remote history; agents are NEVER allowed to force push under any circumstances

### Safe Workflow

```bash
# 1. Check status first
git status

# 2. Add ONLY your specific files
git add 
git add 

# 3. Commit
git commit -m "feat: add github integration"

# 4. Push (pull --rebase if needed, but NEVER reset/checkout)
git pull --rebase && git push
```

### If Rebase Conflicts Occur

- Resolve conflicts in YOUR files only
- If conflict is in a file you didn't modify, abort and ask the user
- NEVER force push

### User Override

If the user instructions conflict with rules set out here, ask for confirmation that they want to override the rules. Only then execute their instructions.