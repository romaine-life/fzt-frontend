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
v0.1.N tag, and cuts the module release. `k8s/deployment.yaml` pins the tested
release by tag and digest. The same run opens a PR moving the pin to the new tag, with the
digest the registry holds for it, and has Forgejo merge it
once its checks pass, so a release deploys itself (auth's pattern); it pushes with the
`PIPELINE_DISPATCH_TOKEN` org secret. Deployment and documentation changes run
the checks without publishing another application release. No image is published to
GitHub, ghcr or Azure ACR.

It runs on the home cluster (nelsong6/home-cluster, HOMEINFRA-1), whose Argo CD syncs
`k8s/` from main. A new release starts its pod and waits for it to answer `/health`
before stopping the old one. The home name, fzt-frontend.home.romaine.life, points at
the cluster in the NAS's DNS Server, and the cluster's gateway serves it with the
`*.home.romaine.life` certificate. No public DNS record or router forward is needed.

The deploy key and the registry's read-only token are OpenBao's
`secret/services/fzt-frontend` (`store-deploy-key`, `registry-read-token`), which
External Secrets copies into the namespace. To replace the deploy key, write the new one
there as the NAS (`sudo /volume1/docker/homesvc/homesvc import-secrets fzt-frontend`, a
JSON object on stdin, never a credential in an argument); the pod picks it up on its
next start. The clone and SSH working files live in /tmp and are rebuilt on every start.
There is no durable service state beyond at and the key: nas-backup's Forgejo job backs
up repositories, and its OpenBao job backs up the secret.

The route's annotations are its down alert: the NAS's `homesvc health` checks
/health each minute and alerts through ntfy. Its logs are in the pile:
`log-search <words> --pc cluster --app fzt-frontend`.

The main publishing job exchanges its Forgejo OIDC identity at OpenBao auth/forgejo
for read access only to secret/services/fzt-frontend-build (registry-token).
The token has write:package only. PRs and other repositories/refs/workflows cannot
read it, and the job cannot read the SSH key. No registry token is held in Key Vault
or a Forgejo Actions secret.

## Development

Use a local git store with STORE_REPO and STORE_DIR. For SSH, also set
STORE_DEPLOY_KEY_FILE to an authorized key; the checked-in host pin is mandatory.
Set PORT if needed. backend/server.js starts the service; npm test in backend
runs the store tests. CI runs Go vet/tests, the no-HS256 guard, backend tests,
and the container build/smoke gate on the home runners.



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
