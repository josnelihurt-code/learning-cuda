# AGENTS.md

## Language

Everything committed to this repository — code, comments, commit messages,
documentation, and PR descriptions — is written in English.

## Session artifacts

Tool/session artifacts (plans, drafts, local state under `.zcode/plans/`)
are never committed; only intentional project files are.

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

Intermediate images (`proto/VERSION`, `src/cpp_accelerator/docker-build-base/`,
`docker-cpp-dependencies/`, `docker-cuda-runtime/`, `yolo-model-gen/`,
`runtime/VERSION`, `test/integration/VERSION`) version their image tags but
carry no deploy gate; bump them when that layer's inputs change so a published
tag never silently points at different content. The full stage→VERSION mapping
lives in `.github/workflows/README_x86.md` and `.github/workflows/README_arm.md`.
