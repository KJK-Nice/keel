---
name: pr-screenshots
description: Put before/after UI screenshots in a GitHub pull request body. Use when a PR changes how Keel looks (tokens, fonts, views) and the reviewer should see the difference, or when asked to add or refresh PR screenshots.
---

# PR screenshots

`gh` cannot upload images to a PR (the `user-attachments` upload is web-only), so images live on an orphan branch, `pr-assets`, and the PR body links them. Never merge `pr-assets`.

The shots are the native window's: `make native-e2e` writes one PNG per test to `.scratch-native/e2e/<Test>.png` (dark; `TestE2EThemeSwitch` also writes the `-light` shots). Read `.agents/skills/verify-keel/SKILL.md` first (scratch data only, `KEEL_KEYRING=off`). Below, `N` is the PR number, `BASE` the ref the PR is compared to, `$S` a scratch dir.

## 1. Before (base ref, in a worktree)

Do not touch the main checkout.

```bash
git worktree add --detach $S/wt-before BASE
(cd $S/wt-before && make native-e2e) && mkdir -p $S/prN-before && cp $S/wt-before/.scratch-native/e2e/*.png $S/prN-before/
git worktree remove --force $S/wt-before
```

## 2. After (this branch)

```bash
make native-e2e && mkdir -p $S/prN-after && cp .scratch-native/e2e/*.png $S/prN-after/
```

Never stop the user's Keel on 127.0.0.1:47241; the e2e uses its own ports and data.

## 3. Pick the screens

Open the before and after PNG of each screen that changed (the image reader shows them) and keep only those. Copy each pair to `$S/pr-N/<screen>-before.png` and `<screen>-after.png`. Look at them before publishing: the shots hold made-up hosts, but check for real data.

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
# edit the Screenshots section: a before image and an after image per screen
gh pr edit N --body-file $S/body.md
```

Re-running with the same file names overwrites images; push again, the links stay valid. These shots come from MyGo's in-memory renderer, not a real display: the translucent window and blur are person-only checks.
