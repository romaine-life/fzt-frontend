# fzt-frontend

Canonical backend for the unified tree/bookmark stack. Hosts a
versioned, ref-resolving tree API that every UI in the romaine.life
ecosystem reads from and writes to.

## CI and releases

CI comes from pipeline-templates' suites (its `fleet.json` entry): Go tests on Windows, the
backend's tests and HS256 guard (`npm run lint`), the k8s/ manifests, and on a PR the image
built and run with `--self-test`. `ci-check` runs the same checks locally, except the image,
which the PR builds. Each merge that changes the Go module, the backend or the Dockerfile
releases v0.1.N: the module's tag and source, the image under the same tag, and the pin PR.

## Where this fits

```
my-homepage           romaine-api.py            (any browser tab,
(static SPA at         CLI on a dev box;          terminal, or
 homepage.romaine     mints its own JWT          script with a
 .life)               for any identity)          fresh JWT)
       \                    |                          /
        \                   |                         /
         v                  v                        v
       ┌────────────────────────────────────────────────┐
       │ fzt-frontend (this repo) at                    │
       │ fzt-frontend.home.romaine.life                      │
       │   • verifies JWTs (does NOT issue them)        │
       │   • CRUD over /fzt/tree/:id                    │
       │   • resolves {ref:"…"} pointers transparently  │
       └────────────────────────────────────────────────┘
                             │
                             v
                     git: romaine-life/at, trees/<id>.json
                     (each write a commit to main)
```

my-homepage is a static frontend — it owns no data. Anything that wants
to read or write a tree (bookmarks, menus, anything else tree-shaped)
talks to this backend.

## API surface

Mounted at the **`/fzt`** prefix (look at `backend/server.js:50` —
nothing else points to it; missing this is the most common 404 when
calling the API for the first time).

| Method | Path                | Body                                | Response                                                 |
|--------|---------------------|-------------------------------------|----------------------------------------------------------|
| GET    | `/fzt/tree/:id`     | —                                   | `{ id, tree, version, updatedAt }` with refs resolved    |
| PUT    | `/fzt/tree/:id`     | `{ tree, baseVersion }`             | `{ id, tree, version, updatedAt }` after writing v(N+1) |

A never-saved tree GETs as `{ tree: [], version: 0, updatedAt: null }`
so callers don't have to special-case 404.

PUTs are version-checked: pass `baseVersion` equal to the version you
read; the server returns 409 if it has moved on, with the current
state attached so callers can merge and retry.

## Auth model

This backend **only verifies** — it never issues tokens. Browsers (my-homepage)
present RS256 JWTs issued by `auth.romaine.life`, verified against
`https://auth.romaine.life/api/auth/jwks` with issuer `https://auth.romaine.life`.
It signs in people only: a token with `role` outside `{admin, user}` is rejected (403),
and a `service`, `purpose` or `device` token is refused (401).
Home machines present ES256 tokens OpenBao signs for role `fzt-frontend` (audience
`fzt-frontend`, issuer `https://bao.home.romaine.life/v1/identity/oidc`, from
`romaine-auth home-token --role fzt-frontend`), checked against OpenBao's published keys
alone, so a machine request has no cloud in its path. The legacy HS256 shared-secret
path was removed in the auth.romaine.life migration — there is no fallback
(see `scripts/check-no-hs256.mjs`, the CI guard against reintroduction).

Bearer header preferred; cookie `auth_token=` accepted as a fallback.
Identity claims (`sub`, `email`, `name`, `role`) are baked into a human token.
A home token carries `device` and an explicit `apps.fzt.owner_sub` grant; the token's
`sub` is an OpenBao entity id and means nothing here. The server permits machine writes
only to that owner's `<sub>-menu` and `<sub>-bookmarks`, and an empty owner grants
nothing. Existing authenticated reads and human write permissions remain unchanged.
Git writes record the caller's name and email, so a machine appears as its own author
(`Machine: <device>`, `<device>@machine.home.romaine.life`). The access log's `caller=`
names each request's token kind: `home device=…`, `auth human sub=…`, or
`auth refused device=… sub=…` for a token this section refuses.

## Store

Trees live in the private repo [romaine-life/at](https://forgejo.romaine.life/romaine-life/at),
which also holds the `at` menu's commands, as `trees/<id>.json`:
`{ "version": N, "updatedAt": ISO8601, "tree": <array or object> }`.
`backend/store.js` keeps a clone. A PUT is a commit pushed straight to that repo's
main (no PR, no CI: the backend checks the same invariants CI does); git's push is
the arbiter between writers, and a writer that loses the race starts again from the
new main, so a stale `baseVersion` still returns 409. History is the repo's history.

Menu trees (`*-menu`) must keep the store's invariant: every entry's action reads
as an argument list (fzt-automate runs it with no shell, FZT-71: an fzt-automate
built-in, a program, or a command still in `commands/`), every command in
`commands/` keeps an entry, and no action names a home-sync install path
(`Program Files\at-home\...`): a tool is reached as `tool:<name>`, which
fzt-automate resolves from home-sync's list, so state.json stays the one place a
tool's program is named (HOMECFG-213). A PUT that breaks it returns 422 with the
reasons. The menu is edited only this way, by Claude sessions: the at menu has no
edit or save commands.

The backend pushes with a deploy key scoped to that one repo, read at startup from
OpenBao `secret/services/fzt-frontend` as `store-deploy-key`, copied in by the home cluster's External Secrets, with Forgejo's host key pinned.

The Cosmos container this replaced (`HomepageDB.fzt-frontend-data`) is archived,
every version, in the store at `archive/cosmos-export.jsonl.gz`.

## Tree refs

Anywhere inside a tree, an item shaped `{ ref: "<otherTreeId>" }`, with
at most its own `name`, `description` and `tags` beside `ref`, expands
inline on GET — the resolver walks the referenced tree's body in place,
the ref's own label winning over the referenced tree's (the at menu's
`links` folder is the bookmarks tree under the label `links`; AT-9). It
tags the resolved node with `_ref`, `_refVersion` and, for a labelled
ref, `_refLabel` so a subsequent PUT can collapse it back to the same
labelled pointer. A referenced tree's empty folders (devctl's
`dev-environments` with nothing running) are left out of the expansion,
since a menu reads a childless leaf as a command. Cycle-guarded (visited
set) and depth-limited (`MAX_REF_DEPTH=10`).

Cross-tree writes are **not** performed: PUT only writes the target
tree id, and `_ref` markers in the body are stripped back to
`{ ref: ... }`. To edit a referenced sub-tree, PUT that tree's id
directly.

## Updating a tree programmatically

```sh
# Read
romaine-api.py tree-get nelson-bookmarks > tree.json

# Edit tree.json with $EDITOR or jq

# Write — picks up the current version automatically; pass
# --base-version to pin against concurrent edits
romaine-api.py tree-put nelson-bookmarks --from tree.json
```

Or do it by hand:

```sh
TOKEN=$(romaine-api.py mint-token --identity nelson)
curl -H "Authorization: Bearer $TOKEN" \
     https://fzt-frontend.home.romaine.life/fzt/tree/nelson-bookmarks
```

## Local dev

```sh
cd backend
npm install
PORT=3000 node server.js
# STORE_REPO points to a local git store, or set STORE_DEPLOY_KEY_FILE for SSH.
# Token verification fetches each issuer's keys over HTTP, on the first token it signs.
```

Frontend dev (Go-WASM fzt terminal): see `fzt-terminal/CLAUDE.md` for
the keyboard model and rendering pipeline; this repo only ships the
backend Go process (cmd/wasm/main.go is the WASM bridge that
`my-homepage` and others embed).
