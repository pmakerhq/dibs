---
name: release
description: Cut a new dibs release — tag it and let the release GitHub workflow build and publish it. Use when the user says "release", "cut a release", "ship a new version", or similar.
---

# Release dibs

Tagging a commit `vX.Y.Z` and pushing that tag triggers `.github/workflows/release.yml`,
which cross-builds the binary and publishes a GitHub release with a changelog.
This skill just handles the tagging side safely and interactively.

## Steps

1. **Check the working tree is clean and on `main`.**
   `git status` — if there are uncommitted changes, stop and tell the user;
   don't tag a dirty tree. `git branch --show-current` — warn if not on `main`,
   let the user decide whether to continue.

2. **Find the last tag and propose the next one.**
   `git tag --sort=-v:refname | head -n1` for the last tag (none = this is the
   first release, propose `v0.1.0`). Default proposal: bump the patch number.
   Ask the user to confirm or override (patch/minor/major/custom), via
   AskUserQuestion with the computed default as the recommended option.

3. **Run the checks the release workflow doesn't get a chance to gate on before tagging.**
   `go build ./...`, `go vet ./...`, `go test ./...`. If any fail, stop and report —
   do not tag a broken commit.

4. **Show the changelog preview.**
   `git log --oneline --no-merges <last-tag>..HEAD` (or full history if no
   previous tag). Show it to the user — this is what the release notes will
   contain.

5. **Confirm before tagging.** Tagging and pushing is what kicks off a public,
   hard-to-reverse action (a published GitHub release). State the tag name and
   ask for explicit confirmation before proceeding.

6. **Create an annotated tag and push it.**
   `git tag -a vX.Y.Z -m "vX.Y.Z"` then `git push origin vX.Y.Z`.
   Do not push `main` itself unless the user separately asked for that —
   this skill's job is the tag.

7. **Point the user at the Actions run.**
   Tell them the release workflow is running and where to watch it
   (`gh run list --workflow=release.yml` or the repo's Actions tab).
