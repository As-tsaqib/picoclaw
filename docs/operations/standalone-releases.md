# Standalone Release Process

`As-tsaqib/picoclaw` is a derivative of the original PicoClaw project, but its
development and release lifecycle are independent. The repository does not
automatically fetch, merge, or publish releases from `sipeed/picoclaw`.

## Independence Boundary

- The Go module path, source imports, updater, installers, documentation, and
  release artifacts point to `As-tsaqib/picoclaw`.
- Stable binaries and archives are published on this repository's GitHub
  Releases page.
- Container images are published to `ghcr.io/as-tsaqib/picoclaw`.
- There is no scheduled upstream-sync workflow. Upstream changes can only be
  considered through an intentional, manually reviewed contribution.
- Historical Git ancestry, the MIT license, and attribution remain intact.
- General third-party dependencies and optional service/provider integrations
  remain supported; repository independence does not mean vendoring them.

## Stable Release Checklist

1. Prepare changes on a feature branch based on `main`.
2. Open a pull request and wait for every required GitHub Actions check to pass.
3. Review module identity, updater URLs, configuration migration, memory and
   session behavior, dashboard compatibility, and release workflow changes.
4. Squash-merge the pull request into `main`.
5. Wait for the post-merge `main` build to pass.
6. Run **Create Tag** with a stable SemVer tag such as `v1.0.0`. The workflow
   only accepts a tag pointing at the current `main` commit.
7. Run **Release** for the existing tag. It builds and publishes artifacts from
   that tag to this repository and pushes containers only to this fork's GHCR.
8. Verify the GitHub Release is public, is marked latest when appropriate, has
   checksums and expected platform archives, and reports the intended version.
   The Android extra is a universal native-library ZIP, not an installable APK.

The release workflow uses only `GITHUB_TOKEN` for GitHub Releases and GHCR. It
does not require upstream credentials, Docker Hub credentials, or the original
project's object-storage credentials.

## Release Safety Controls

- **Create Tag** accepts only stable `vMAJOR.MINOR.PATCH` tags and an optional
  full 40-character commit SHA. It refuses to tag anything except the current
  `main` commit.
- **Release** accepts only an existing stable tag that still resolves to the
  current `main` commit. Inputs are passed to shell steps through environment
  variables after validation rather than interpolated into shell source.
- Stable and nightly workflows run mutating jobs only in
  `As-tsaqib/picoclaw`. Publication uses GitHub Releases and
  `ghcr.io/as-tsaqib/picoclaw` only.
- The release workflow is serialized and does not publish to Docker Hub,
  Volcengine TOS, or any upstream-owned destination.
- Every release should be created from a reviewed, green `main`; the workflow
  checks identity and refs but cannot prove semantic compatibility by itself.

## Nightly Builds

Nightly releases are also produced solely from this repository's `main` branch
and use this repository's GitHub Release and GHCR namespaces. A nightly build
is a prerelease and must not be treated as a stable release.

## Upstream History

The original project remains the historical source of this derivative. Links
to the original repository may remain where they are attribution, historical
issue references, protocol documentation, or hardware references. They are not
runtime, build, updater, or release dependencies.
