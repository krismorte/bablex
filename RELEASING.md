# Bablex release process

Bablex uses Semantic Versioning and Conventional Commits.

## Pull requests

Use a Conventional Commit-style PR title:

- `fix: correct personal data update` -> patch
- `feat: add book recommendations` -> minor
- `feat!: redesign sharing API` -> major
- A commit/body containing `BREAKING CHANGE:` -> major

Other accepted types include `docs`, `refactor`, `perf`, `test`, `build`, `ci`, and `chore`.

PRs run backend tests, frontend tests/build, Terraform formatting/validation, and PR-title validation.

## Automatic release

A merge to `main` triggers `release.yml`.

It:
1. Finds the latest `vMAJOR.MINOR.PATCH` tag.
2. Examines commits since that tag.
3. Chooses major/minor/patch from Conventional Commit semantics.
4. Creates an annotated Git tag.
5. Creates a GitHub Release with generated notes.

The first release starts from `v0.0.0`. For example, the first `feat:` produces `v0.1.0`; the first `fix:` produces `v0.0.1`.

## Deployment

`deploy.yml` runs on semantic-version tags, not ordinary pushes to `main`.

This ensures AWS is always associated with a specific release tag.

A manual `workflow_dispatch` remains available and requires an explicit tag or commit ref.

## GitHub repository settings

Protect `main` and require:
- pull requests
- PR checks
- conventional-title check
- test check

Protect the `production` GitHub Environment as appropriate.

## VERSION and CHANGELOG

Git tags/GitHub Releases are the authoritative production version.

`VERSION` starts at `0.0.0` as a repository baseline. It is not modified automatically because creating a version commit from the release workflow can cause recursive releases and complicate branch protection.

`CHANGELOG.md` documents the policy; release-specific notes live in GitHub Releases. This avoids bot commits to `main` while preserving a complete release history.
