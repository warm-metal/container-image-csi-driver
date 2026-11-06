---
name: Backport Release
description: Use when backporting commits from main (v2.x) to release-v1.2.x and cutting a new v1.2.x tag and GitHub release in warm-metal/container-image-csi-driver. Covers finding what is missing, cherry-picking, resolving the known conflicts, version bump, verification, tagging and release notes.
---

# Backport main (v2.x) to release-v1.2.x

The automated `backport-release` GitHub Action was removed from main (`15d901c`), so
backports are a manual process. Follow this workflow. It is based on the v1.2.18
backport.

## Ground rules

- Work in a **separate worktree** on `release-v1.2.x`. Never switch the user's main checkout.
- **Show the user the one-line-per-commit list and get agreement before the first cherry-pick.**
- Pushing the branch, pushing a tag and creating a GitHub release are shared, hard-to-undo
  actions. Confirm with the user first unless they already said to push and release directly
  in this session.
- Always cherry-pick with `-x` so each message records its source SHA.
- Dependency, base-image and GitHub Actions bumps are **in scope**. The project policy is to
  stay on the latest possible versions. Do not skip them to avoid conflicts.

## 1. Establish the baseline

```sh
git fetch origin --tags
git log --oneline -3 origin/release-v1.2.x
gh release list --repo warm-metal/container-image-csi-driver --limit 6
```

- The latest `v1.2.N` release notes say `Backported from vX.Y.Z`. That v2 tag is the lower
  bound: everything after it on main is "newer".
- Do **not** use `git cherry` or SHA comparison to decide what is missing. Earlier backports
  were cherry-picked and rewritten, so nearly every commit shows as missing. Match by PR
  number or subject instead.

## 2. Build the candidate list

```sh
python3 .opencode/skills/backport-release/scripts/list-candidates.py            # auto-detect --since
python3 .opencode/skills/backport-release/scripts/list-candidates.py --since v2.1.12
```

Output is `ACTION  SHA  SUBJECT  FILES`, oldest first:

| Action | Meaning |
|---|---|
| `PICK` | backport it |
| `REVIEW` | backport it, but it touches `images-build-and-push.yaml`, so expect a conflict |
| `SKIP` | main-only tooling or versioning, do not backport |
| `PRESENT` | already on the release branch |

The script's `SKIP` rules are heuristics. Read the list yourself before presenting it. These
are normally skipped:

- `Update version to v2.x.y`: replaced by our own v1.2.x bump (step 5).
- `.github/dependabot.yml` changes: main-only config.
- Commits that edit the `backport-release` job in `images-build-and-push.yaml`: main-only tooling.

Present the user a table grouped as runtime fixes, dependency bumps and skipped, with a
**one-line summary per commit**, and call out risks (see "Things to flag"). Wait for approval.

## 3. Cherry-pick in a worktree

```sh
WT="${TMPDIR:-/tmp}/opencode/backport-<version>"   # any writable dir outside the repo works
git worktree add "$WT" release-v1.2.x
cd "$WT"
for c in <shas oldest-first>; do
  git cherry-pick -x "$c" || { echo "CONFLICT at $c"; git status --short | grep -E '^(UU|AA|DU|UD)'; break; }
done
```

Always apply oldest first and stop at the first conflict. After resolving, run
`GIT_EDITOR=true git cherry-pick --continue` and resume with the remaining SHAs.

## 4. Conflict resolution policy

Look at the actual hunks (`git diff -- <file>`) before resolving. Never use a blanket
`-X theirs`. The old automation did that and it was replaced because it silently picked
wrong sides.

| File | Resolution |
|---|---|
| `go.mod` / `go.sum` | Take the **incoming (main)** side. The release branch is only older. Then run `go mod tidy` and confirm it produces **no diff**. |
| `.github/workflows/images-build-and-push.yaml` | Keep the **release-branch** side when the hunk touches a release divergence (see below). Take the incoming side only for plain action-version bumps that apply cleanly. |
| `Dockerfile`, `charts/**`, `hack/**` | Normally apply cleanly. Take incoming. Check the Chart values against the release branch if they conflict. |
| Anything under `cmd/` or `pkg/` | Do not guess. Read both sides and explain the resolution to the user. |

Known release-branch divergences in `images-build-and-push.yaml` (do not clobber them):

- `BASE_IMAGE_NAME` resolves to `warmmetal/csi-image` for `v1.*` tags and `release-v1.2.x`.
- The release copy still contains the `backport-release` job, which main has removed. In v1.2.18
  the release side had already dropped that job's "Set up Go" step, so a main commit that only
  bumped `actions/setup-go` conflicted. Keep the release side of any hunk inside that job.
- `gh release create ... --latest=false` so v1.2.x releases never become GitHub "Latest".

To take one side of every conflict hunk in a file (after reading the hunks):

```sh
python3 .opencode/skills/backport-release/scripts/resolve-conflicts.py --keep theirs go.mod go.sum
python3 .opencode/skills/backport-release/scripts/resolve-conflicts.py --keep ours .github/workflows/images-build-and-push.yaml
```

`ours` is the release branch (HEAD during a cherry-pick) and `theirs` is the incoming main
commit. The repo uses diff3-style markers (a `|||||||` base block) and the script handles
them. It exits non-zero if any marker is left behind. Then `git add` the file and run
`GIT_EDITOR=true git cherry-pick --continue`.

## 5. Version bump (separate final commit)

Set the next patch version in all three places:

- `Makefile`: `VERSION ?= v1.2.N`
- `charts/warm-metal-csi-driver/Chart.yaml`: `version: 1.2.N`
- `charts/warm-metal-csi-driver/Chart.yaml`: `appVersion: v1.2.N`

Grep for the old version afterwards: `grep -rn '1\.2\.<old>' --exclude-dir=.git .`

Commit as `chore: bump version to v1.2.N` with a body listing the notable backported changes.

## 6. Verify before pushing

```sh
go mod tidy && git diff --stat -- go.mod go.sum        # expect no diff
GOOS=linux GOARCH=amd64 go build ./...
GOOS=linux GOARCH=amd64 go vet ./...
GOOS=linux GOARCH=arm64 go test -c -o /dev/null ./cmd/plugin/   # test compile check
```

- **Do not use a plain `go build ./...` on macOS.** `pkg/backend/containerd` needs a
  Linux-only helper (`syscallMountInHostNamespace`), so it fails on darwin on `main` too.
  That is expected and not caused by the backport.
- Unit tests for `containerd` and `cmd/plugin` cannot run on darwin. If Docker is available,
  run `go test ./...` in a `golang` Linux container. If not, say clearly that tests were
  compile-checked only.
- Diff the workflow against main to confirm divergences survived:
  `git diff origin/main -- .github/workflows/images-build-and-push.yaml`.

## 7. Push, tag, release

Confirm with the user first (see Ground rules), then:

```sh
git push origin release-v1.2.x
```

**Tag gotchas**

- The user has `tag.gpgSign=true`. A plain `git tag v1.2.N` fails with `fatal: no tag message?`.
  Either use `git tag -s -m "v1.2.N" v1.2.N` after asking the user, or let
  `gh release create` create the tag on GitHub (what v1.2.18 did).
- If `gh release create` makes the tag, `git log v1.2.17..v1.2.N` fails locally until you
  `git fetch origin --tags`. Compute the notes **before** creating the release, using
  `v1.2.<prev>..HEAD`, or the release body comes out empty.

```sh
NOTES=$(git log --no-merges --format='- %s' v1.2.<prev>..HEAD | grep -v 'bump version to')
gh release create v1.2.N --repo warm-metal/container-image-csi-driver \
  --title v1.2.N --target release-v1.2.x --latest=false \
  --notes "Backported from v2.x.y (changes since v2.a.b).

## Changes
$NOTES"
```

`--latest=false` is mandatory, otherwise the v1.2.x release steals the "Latest" badge from v2.x.

Then confirm:

```sh
git fetch origin --tags
git rev-parse 'v1.2.N^{commit}' origin/release-v1.2.x       # must match
gh release list --repo warm-metal/container-image-csi-driver --limit 3   # v2.x must stay Latest
gh run list --repo warm-metal/container-image-csi-driver --limit 4
```

Pushing the branch and the tag each start an image build workflow (`warmmetal/csi-image`).
Give the user the run links. Do not claim the images are published until the runs complete.

## 8. Clean up

```sh
git worktree remove "$WT"      # offer this, do not do it silently
```

## Things to flag to the user (judgment calls)

- **Go version drift.** The `Dockerfile` builder tracks Dependabot (`golang:1.27-alpine3.24`)
  but the `go` directive in `go.mod` only moves when a dependency forces it
  (Dependabot's `gomod` ecosystem does not bump the `go` line itself). `test/sanity/go.mod` and
  `test/e2e/go.mod` lag further. Mention the mismatch; fix it on `main` first, then backport.
- A bump to a **new Go minor** (e.g. 1.26 to 1.27) on a patch release is notable. Say so
  rather than letting it pass silently.
- `values*.yaml` / chart template changes alter deployed manifests. Call them out.
- Release notes should name the real user-facing fixes first, then group the dependency bumps.
