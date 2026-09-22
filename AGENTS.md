# AGENTS.md

## Language

Everything committed to this repository — code, comments, commit messages,
documentation, and PR descriptions — is written in English.

## Session artifacts

Tool/session artifacts (plans, drafts, local state under `.zcode/plans/`)
are never committed; only intentional project files are.

## Comments are sparse and explain why

Comments explain why, never what. Do not narrate code or restate
identifier names in prose. Doc comments on exported identifiers state the
contract in at most three lines. A non-obvious constraint (ordering trap,
protocol quirk, deliberately preserved behavior) gets one focused
sentence, not a paragraph. Comment noise is a blocking review defect:
when in doubt, delete the comment.

## Ports are unexported

Consumer-owned port interfaces are unexported: only their package names
them. Cross-package wiring passes concrete adapters, which satisfy the
port structurally. Exporting a port leaks the abstraction and invites
imports against the wrong direction.

## Ports are named by role, not by pattern

Ports (consumer-side interfaces) are named for the role they play for the
consumer. The Repository suffix is reserved for actual repositories:
collection semantics over domain entities (List/Get/Save). A port that
persists bytes is a Storage; one that produces an artifact is a Generator;
adapters take an implementation prefix plus the role (FileVideoStorage,
StaticImageRepository). Never suffix a port just to mark it as a port.

## Version files gate deploys

Production deploys are triggered by `VERSION` file bumps, not by code changes.
A push to `main` rebuilds and pushes the images, but the deploy jobs only run
when the component's `VERSION` file changed in that push:

- `src/go_api/VERSION` or `src/front-end/VERSION` → deploys go-api /
  web-frontend to the Cloud VM (x86 workflow, `deploy_prod`).
- `src/cpp_accelerator/VERSION` → deploys the runtime to the Jetson
  (arm workflow, `deploy_prod`).

Any PR that changes runtime code of a deployable component MUST also bump its
`VERSION` file in the same PR, or the merge will not deploy. Bumps follow the
repo's semver practice: user-visible features bump minor (or major on contract
changes), fixes and toolchain churn bump patch.

The same rule applies layer by layer to the image chain (see
`scripts/docker/build-local.sh` for the stage → Dockerfile → VERSION mapping):
when a layer's Dockerfile or inputs change, bump that layer's `VERSION` in the
same PR — `proto/docker-build-base`, `src/go_api/builder`, `proto`,
`src/cpp_accelerator/docker-build-base`, `docker-cpp-dependencies`,
`docker-cuda-runtime`, `yolo-model-gen`, `runtime`, `test/integration`.
Two failure modes make this mandatory:

- Not bumping: the merge re-pushes an existing versioned tag with different
  content (silent tag mutation; consumers pinned to the tag get changed bits).
- Bumping without the rebuild in the same merge:
  `scripts/docker/pull-ghcr-cpp-intermediates.sh` pulls
  `proto-generated-${proto/VERSION}` by versioned tag on ARM, so a bumped
  VERSION whose image was never built+pushed fails the next ARM build.

`proto/VERSION` also composes into the `cpp-accelerator` and `web-frontend`
image tags (`...-proto${proto/VERSION}`); bump it when the proto stage's
generated output changes, not for wire-compatible regenerations only when
artifact bytes are identical. `scripts/hooks/pre-commit-version-check.sh`
enforces module → VERSION co-movement locally; the full CI stage mapping lives
in `.github/workflows/README_x86.md` and `.github/workflows/README_arm.md`.
