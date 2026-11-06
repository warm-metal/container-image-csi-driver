#!/usr/bin/env python3
"""List commits on main that still need backporting to release-v1.2.x.

Compares by PR number / subject (not patch-id) because earlier backports were
cherry-picked and rewritten, so `git cherry` reports almost everything as missing.

Usage:
  list-candidates.py [--since vX.Y.Z] [--main origin/main] [--release origin/release-v1.2.x]

--since defaults to the v2 tag named in the latest v1.2.x GitHub release notes
("Backported from vX.Y.Z"). Pass it explicitly if gh is unavailable.

Output (tab separated, oldest first):  ACTION  SHA  SUBJECT  [FILES]
  PICK    backport this
  REVIEW  backport, but expect a conflict (touches release-diverged files)
  SKIP    main-only tooling / versioning, do not backport
  PRESENT already on the release branch (same PR number or subject)
"""
import argparse
import re
import subprocess
import sys


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def detect_since():
    tags = [t for t in git("tag", "--sort=-v:refname").splitlines() if re.match(r"^v1\.2\.\d+$", t)]
    if not tags:
        sys.exit("no v1.2.x tag found; pass --since")
    try:
        body = subprocess.check_output(
            ["gh", "release", "view", tags[0], "--json", "body", "--jq", ".body"], text=True
        )
    except Exception as e:  # gh missing / not authenticated
        sys.exit(f"could not read release notes for {tags[0]} ({e}); pass --since")
    m = re.search(r"Backported from (v\d+\.\d+\.\d+)", body)
    if not m:
        sys.exit(f"{tags[0]} release notes have no 'Backported from vX.Y.Z'; pass --since")
    return m.group(1), tags[0]


def pr_of(subject):
    m = re.search(r"\(#(\d+)\)\s*$", subject)
    return m.group(1) if m else None


def strip_pr(subject):
    return re.sub(r"\s*\(#\d+\)\s*$", "", subject)


def classify(subject, files):
    only = set(files)
    if re.match(r"^Update version to v2\.", subject):
        return "SKIP"  # replaced by our own v1.2.x bump
    if only and only <= {".github/dependabot.yml"}:
        return "SKIP"  # dependabot config is main-only
    if re.search(r"backport", subject, re.I) and only <= {".github/workflows/images-build-and-push.yaml"}:
        return "SKIP"  # backport-release job is main-only tooling
    if ".github/workflows/images-build-and-push.yaml" in only:
        return "REVIEW"  # release copy diverges (image name, --latest=false, no setup-go)
    return "PICK"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--since")
    ap.add_argument("--main", default="origin/main")
    ap.add_argument("--release", default="origin/release-v1.2.x")
    a = ap.parse_args()

    since = a.since
    if not since:
        since, last = detect_since()
        print(f"# last release {last} was backported from {since}", file=sys.stderr)

    base = git("merge-base", a.main, a.release)
    rel = [l.split("|", 1) for l in git("log", "--no-merges", "--format=%h|%s", f"{base}..{a.release}").splitlines()]
    rel_prs = {pr_of(s) for _, s in rel if pr_of(s)}
    rel_subjects = {strip_pr(s) for _, s in rel}

    shas = git("log", "--no-merges", "--reverse", "--format=%h", f"{since}..{a.main}").splitlines()
    for sha in shas:
        subject = git("log", "-1", "--format=%s", sha)
        files = git("show", "--name-only", "--format=", sha).splitlines()
        pr = pr_of(subject)
        if (pr and pr in rel_prs) or strip_pr(subject) in rel_subjects:
            action = "PRESENT"
        else:
            action = classify(subject, files)
        print(f"{action}\t{sha}\t{subject}\t{','.join(files[:4])}{'...' if len(files) > 4 else ''}")


if __name__ == "__main__":
    main()
