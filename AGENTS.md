# fzt-frontend

Read `CLAUDE.md` for the project architecture and domain notes.

## CI and releases

CI comes from pipeline-templates' suites (its `fleet.json` entry): Go tests on Windows, the
backend's tests and HS256 guard (`npm run lint`), the k8s/ manifests, and on a PR the image
built and run with `--self-test`. `ci-check` runs the same checks locally, except the image,
which the PR builds. Each merge that changes the Go module, the backend or the Dockerfile
releases v0.1.N: the module's tag and source, the image under the same tag, and the pin PR.
