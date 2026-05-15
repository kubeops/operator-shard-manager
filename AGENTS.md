# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `kubeops.dev/operator-shard-manager` — assigns Kubernetes resources to **operator shards** so a single operator can scale horizontally. Per a `ShardConfiguration` CR, the controller labels target resources with `shard.operator.k8s.appscode.com/<shard-config-name>: <shard-index>` (and the matching `APIService`s) using **consistent hashing with bounded load** ([Google research](https://research.google/blog/consistent-hashing-with-bounded-loads/)) — that balances assignment uniformly while minimizing reshuffles when the operator's pod count changes.

Two consumer patterns (from `README.md`):
1. **Shard-aware list/watch**: the operator watches only its own shard's labeled resources. Caveat: a missing object in cache may mean "moved to another shard", not "deleted".
2. **Watch-all + predicate**: watch everything, skip what isn't yours. Same list-call size, but lets the operator reach cross-shard objects.

The produced binary is `operator-shard-manager`.

## Architecture

- `cmd/operator-shard-manager/` — entry point.
- `pkg/cmds/` — Cobra root + run.
- `api/v1alpha1/` — Kubebuilder API types. Single CRD: `ShardConfiguration` (API group `operator.k8s.appscode.com/v1alpha1`).
- `crds/` — generated CRD YAML (`operator.k8s.appscode.com_shardconfigurations.yaml`) plus `lib.go`.
- `pkg/controller/`:
  - `shardconfiguration_controller.go` — main reconciler. Computes shard assignments and applies labels.
  - `apiservice_controller.go` — keeps aggregated `APIService` resources labeled in lockstep (so kube-aggregator routes traffic to the right shard).
  - `hashing.go` — consistent-hashing-with-bounded-load implementation. The heart of the project.
  - `utils.go` — shared helpers.
- `Dockerfile.in` (PROD, distroless), `Dockerfile.dbg` (debian), `Dockerfile.ubi` (Red Hat certified) — three image variants.
- `hack/`, `Makefile` — AppsCode build harness.
- `vendor/` — checked-in deps.
- `hack/samples/` — example `ShardConfiguration` YAMLs.

## Common commands

All Make targets run inside `ghcr.io/appscode/golang-dev` — Docker must be running.

- `make ci` — CI pipeline.
- `make build` / `make all-build` — build host or all-platform binaries.
- `make gen` — regenerate clientset + manifests. Run after any change to `api/v1alpha1/*_types.go`.
- `make manifests` — regenerate CRDs only.
- `make clientset` — regenerate client code.
- `make fmt`, `make lint`, `make unit-tests` / `make test` — standard.
- `make verify` — `verify-gen verify-modules`; `go mod tidy && go mod vendor` must leave the tree clean.
- `make container` — build PROD, DBG, and UBI images.
- `make push` — push all three; `make docker-manifest` writes multi-arch manifests; `make release` is the full publish flow.
- `make push-to-kind` / `make deploy-to-kind` — load into Kind and Helm-install.
- `make install` / `make uninstall` / `make purge` — Helm install lifecycle.
- `make add-license` / `make check-license` — manage license headers.

Run a single Go test (requires a local Go toolchain):

```
go test ./pkg/controller/... -run TestName -v
```

## Conventions

- Module path is `kubeops.dev/operator-shard-manager` (vanity URL). Imports must use that.
- License: Apache-2.0 (`LICENSE`). Sign off commits (`git commit -s`); contributions follow the DCO.
- Vendor directory is checked in — `go mod tidy && go mod vendor` must leave the tree clean (enforced by `verify-modules`).
- The label key `shard.operator.k8s.appscode.com/<shard-config-name>` is the **user contract** consumed by every sharding-aware operator. Do not rename without a coordinated migration.
- Consistent-hashing-with-bounded-load lives in `pkg/controller/hashing.go`. Preserve the bounded-load invariant — that's what minimizes resource churn when the shard count changes.
- Do not hand-edit `zz_generated.*.go` or `crds/*.yaml` — change `api/v1alpha1/*_types.go` and re-run `make gen`.
- Three Dockerfiles, one binary — keep `Dockerfile.in`, `Dockerfile.dbg`, and `Dockerfile.ubi` in sync.
- This is a **Kubebuilder project** (`PROJECT` file). Use `kubebuilder` to scaffold new APIs.
