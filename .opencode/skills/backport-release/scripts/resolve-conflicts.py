#!/usr/bin/env python3
"""Resolve every git conflict hunk in the given files by keeping one side.

Usage: resolve-conflicts.py --keep ours|theirs FILE [FILE...]

  ours   = HEAD, i.e. the release-v1.2.x side during a cherry-pick
  theirs = the incoming commit from main

Handles both plain and diff3-style markers (the optional ||||||| base block).
Only use this after reading the hunks with `git diff -- FILE`; it does not decide
which side is correct. Prints the number of hunks resolved per file and exits
non-zero if any marker is left behind.
"""
import argparse
import re
import sys

PAT = re.compile(
    r"<<<<<<< [^\n]*\n(?P<ours>.*?)(?:\|\|\|\|\|\|\| [^\n]*\n.*?)?=======\n(?P<theirs>.*?)>>>>>>> [^\n]*\n",
    re.S,
)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", choices=["ours", "theirs"], required=True)
    ap.add_argument("files", nargs="+")
    a = ap.parse_args()

    bad = False
    for f in a.files:
        s = open(f).read()
        s, n = PAT.subn(lambda m: m.group(a.keep), s)
        open(f, "w").write(s)
        left = len(re.findall(r"^(<<<<<<<|=======|>>>>>>>)", s, re.M))
        print(f"{f}: {n} hunk(s) -> {a.keep}, markers left: {left}")
        bad = bad or left > 0
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
