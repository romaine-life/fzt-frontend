# fzt-frontend

Canonical menu and bookmark tree API, home-only at
https://fzt-frontend.home.romaine.life. Work: [FORGEJO-6](https://openproject.romaine.life/wp/FORGEJO-6),
service project [FZTF](https://openproject.romaine.life/projects/FZTF).
The Go module keeps its github.com/romaine-life/fzt-frontend import path;
Forgejo is the repository and release authority, and GitHub is a push mirror.

The backend verifies two kinds of token. Browsers present auth.romaine.life JWTs,
checked against its JWKS for the first-party issuer, audience and identity claims.
Home machines present OpenBao tokens for role fzt-frontend, checked against OpenBao's
published keys alone, so no cloud is in their path. A machine may write only its
explicit owner's menu and bookmarks, and its writes record it as the Git author.
auth.romaine.life signs in people only: its machine, service and device tokens are
refused. The access log's `caller=` names each request's token kind.
It does not issue credentials or have a browser login UI. /health is the
unauthenticated operational probe; /fzt/tree/:id requires authentication.
The homepage supplies auth.romaine.life's JWT and the CLI an OpenBao home token.

Every successful write commits trees/<id>.json to romaine-life/at on Forgejo
through SSH on the home network. The write deploy key belongs to that repo
only. backend/forgejo_known_hosts pins the host key obtained directly from
the NAS's Forgejo container; keys learned from the network are never accepted.
Optimistic versions, push-race retries and menu invariants remain enforced.

## Deploy and publish

After merging a green PR, the Forgejo CI builds and smoke-tests the real image,
publishes it under forgejo.romaine.life/romaine-life/fzt-frontend with the next
v0.1.N tag, and cuts the module release. The compose file pins the tested release
through a checked deployment PR. Deployment and documentation changes run the
checks without publishing another application release.
Only the application image is new; third-party runtime images use an older,
explicit release. No image is published to GitHub, ghcr or Azure ACR.

```powershell
homesvc deploy .
```

homesvc.json owns the NAS deployment, home DNS zone, DSM reverse proxy,
*.home.romaine.life certificate association, health/down-and-recovery alerts
and homepage home-services link. Compose binds only 127.0.0.1:3020; DSM serves
HTTPS at the home hostname. No public DNS record or router forward is needed.

The main publishing job exchanges its Forgejo OIDC identity at OpenBao auth/forgejo
for read access only to secret/services/fzt-frontend-build (registry-token).
The token has write:package only. PRs and other repositories/refs/workflows cannot
read it, and the job cannot read the SSH key. No registry token is held in Key Vault
or a Forgejo Actions secret.

The NAS's own device key reads secret/services/fzt-frontend from
OpenBao. Provision store-deploy-key before deploying, using the released
homesvc on the NAS (JSON object on stdin, never a credential in an argument):

```text
sudo /volume1/docker/homesvc/homesvc import-secrets fzt-frontend
```

The required existing secret fails closed if missing. homesvc renders it as
credentials/store-deploy-key, uid 1001, mode 0400, and mounts it read-only.
The clone and SSH working files live in /tmp and are rebuilt on every start.
There is no durable service state beyond at and the key: nas-backup's existing
Forgejo job backs up repositories, and its OpenBao job backs up the secret.
A service-specific database backup is unnecessary. Deployment config is in git;
DSM and DNS are covered by their existing backup jobs.

## Development

Use a local git store with STORE_REPO and STORE_DIR. For SSH, also set
STORE_DEPLOY_KEY_FILE to an authorized key; the checked-in host pin is mandatory.
Set PORT if needed. backend/server.js starts the service; npm test in backend
runs the store tests. CI runs Go vet/tests, the no-HS256 guard, backend tests,
and the container build/smoke gate on the home runners.

Private image pulls use the registry-read-token existing OpenBao secret (read:package
only). homesvc.json's registry block gives Docker an isolated root-only config for
this service; it is not mounted into the application and does not change any other
service's registry login. Use a homesvc release with registry support.


## Retiring AKS (FORGEJO-6)

After the home URL passed a real fzt-automate sync and a write/revert to Forgejo at,
this change removes the Kubernetes chart. infra-bootstrap removes its Argo Application
and Azure CI identity while preserving the GitHub push-mirror repository. The old
GitHub deploy key on at and old ACR image repository are retired after that proof.

The runtime Azure stack is kept as an empty retirement configuration against its
existing state. From a merged checkout with the operator's existing az login:

```powershell
cd infra
tofu init
tofu plan -parallelism=4 '-out=retire.tfplan'
tofu apply -parallelism=4 retire.tfplan
```

The reviewed plan must destroy only the old fzt-frontend UAMI, its federated
credential, Key Vault and Key Vault role assignment, and create nothing. Shared
Azure resources and the git-backed trees are outside this state. The empty state
remains an audit record. Normal Key Vault soft-delete retention applies.
