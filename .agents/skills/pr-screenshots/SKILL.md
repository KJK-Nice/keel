---
name: pr-screenshots
description: Put before/after UI screenshots in a GitHub pull request body. Use when a PR changes how Keel looks (tokens, CSS, templ, fonts) and the reviewer should see the difference, or when asked to add or refresh PR screenshots.
---

# PR screenshots

`gh` cannot upload images to a PR (the `user-attachments` upload is web-only), so images live on an orphan branch, `pr-assets`, and the PR body links them. Never merge `pr-assets`.

Needs `.agents/skills/verify-keel/SKILL.md` (read it first) and `tests/visual.e2e.ts`, which saves `<theme>-hosts|new-host|snippets|settings|terminal|palette` in dark and light. Below, `N` is the PR number, `BASE` the ref the PR is compared to, `$S` a scratch dir.

## 1. Before (base ref, in a worktree)

The base may lack the current test, so copy it in. Do not touch the main checkout.

```bash
git worktree add --detach $S/wt-before BASE
cp tests/visual.e2e.ts tests/keel.ts $S/wt-before/tests/; cp e2e.config.ts $S/wt-before/
(cd $S/wt-before && npm install && make build && \
  npx e2e run tests/visual.e2e.ts --target webkit --output .e2e/proof/prN-before)
cp -R $S/wt-before/.e2e/proof/prN-before .e2e/proof/
git worktree remove --force $S/wt-before
```


## 2. After (this branch)

```bash
make build && npx e2e run tests/visual.e2e.ts --target webkit --output .e2e/proof/prN-after
```

Never stop the user's Keel on 127.0.0.1:47241; the runner uses its own port.

## 3. Compose pairs

One PNG per screen, before | after, labelled (uses `playwright-core` from `node_modules`, no installs):

```bash
node .agents/skills/pr-screenshots/compose.mjs .e2e/proof/prN-before .e2e/proof/prN-after $S/pr-N
```

Look at one or two outputs before publishing.

## 4. Publish to `pr-assets` under `pr-N/`

Use a separate worktree. If `origin/pr-assets` exists, check it out; else create an orphan.

```bash
git fetch origin pr-assets
if git ls-remote --exit-code --heads origin pr-assets >/dev/null; then
  git worktree add $S/wt-assets -B pr-assets origin/pr-assets
else
  git worktree add --orphan -b pr-assets $S/wt-assets
  echo "Screenshots for pull requests (pr-<n>/). Never merge this branch." > $S/wt-assets/README.md
fi
mkdir -p $S/wt-assets/pr-N && cp $S/pr-N/*.png $S/wt-assets/pr-N/
(cd $S/wt-assets && git add -A && git commit -m "pr-N: screenshots" && git push -u origin pr-assets)
git worktree remove --force $S/wt-assets
```

## 5. Embed in the PR body

URL form: `https://github.com/<owner>/<repo>/blob/pr-assets/pr-N/<file>.png?raw=true`.

```bash
gh pr view N --json body -q .body > $S/body.md
# edit the Screenshots section: one `![alt](url)` per composite, a dark and a light group
gh pr edit N --body-file $S/body.md
```

State that the shots are Playwright's WebKit build, not the system WKWebView. Re-running with the same file names overwrites images; push again, the links stay valid.
