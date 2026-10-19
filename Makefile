# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# YEAR defines the year value used for substituting the YEAR placeholder in the boilerplate header.
YEAR ?= $(shell date +%Y)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object paths="./..."

.PHONY: proto
proto: buf protoc-gen-go protoc-gen-connect-go ## Lint the protobuf API and generate the Go code in gen/.
	"$(BUF)" lint proto
	"$(BUF)" format -w proto
	"$(BUF)" generate

.PHONY: ui-proto
ui-proto: buf ## Generate the TypeScript API client of the UI in ui/src/gen.
	"$(BUF)" generate --template buf.gen.ui.yaml

.PHONY: ui
ui: ## Build the web UI into ui/dist. Then build the server with -tags ui.
	cd ui && pnpm install --frozen-lockfile && pnpm build

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup downloads the pinned Kind and builds/loads the Manager Docker image locally.
# kubectl kuberc is disabled by default for test isolation; enable with:
# - KUBECTL_KUBERC=true
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= chronicle-e2e
# The e2e tests use their own kubeconfig, so they never touch another cluster or context.
E2E_KUBECONFIG ?= $(LOCALBIN)/$(KIND_CLUSTER).kubeconfig

# PostgreSQL for the archive tests. make test uses it when CHRONICLE_TEST_POSTGRES is set.
POSTGRES_IMAGE ?= postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873
POSTGRES_TEST_DSN ?= postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable

.PHONY: postgres-up
postgres-up: ## Start PostgreSQL for the tests. Then run: export CHRONICLE_TEST_POSTGRES='$(POSTGRES_TEST_DSN)'
	$(CONTAINER_TOOL) run -d --rm --name chronicle-test-postgres -e POSTGRES_PASSWORD=test -p 127.0.0.1:55432:5432 $(POSTGRES_IMAGE)

.PHONY: postgres-down
postgres-down: ## Stop the PostgreSQL of the tests.
	$(CONTAINER_TOOL) rm -f chronicle-test-postgres

# SeaweedFS is the S3 store of the artifact tests. make test uses it when CHRONICLE_TEST_S3 is set.
S3_IMAGE ?= chrislusf/seaweedfs:4.48@sha256:4e61d15fd35994cb1e43e1e553dff106794841fd9a99ade2fc8c8bfce4d7872d
S3_TEST_ENDPOINT ?= 127.0.0.1:58333

.PHONY: s3-up
s3-up: ## Start an S3 store for the tests. Then run: export CHRONICLE_TEST_S3='$(S3_TEST_ENDPOINT)'
	$(CONTAINER_TOOL) run -d --rm --name chronicle-test-s3 -e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test-secret \
		-p 127.0.0.1:58333:8333 $(S3_IMAGE) mini -dir=/data -bucket=test

.PHONY: s3-down
s3-down: ## Stop the S3 store of the tests.
	$(CONTAINER_TOOL) rm -f chronicle-test-s3

.PHONY: setup-test-e2e
setup-test-e2e: kind kubectl ## Set up a Kind cluster for e2e tests if it does not exist
	@case "$$("$(KIND)" get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			"$(KIND)" create cluster --name $(KIND_CLUSTER) --kubeconfig "$(E2E_KUBECONFIG)" ;; \
	esac
	"$(KIND)" export kubeconfig --name $(KIND_CLUSTER) --kubeconfig "$(E2E_KUBECONFIG)"

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	PATH="$(LOCALBIN):$$PATH" KUBECONFIG="$(E2E_KUBECONFIG)" KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout 45m
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: kind ## Tear down the Kind cluster used for e2e tests
	@"$(KIND)" delete cluster --name $(KIND_CLUSTER) --kubeconfig "$(E2E_KUBECONFIG)"

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT_CUSTOM)" run $(GOLANGCI_LINT_ARGS)

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT_CUSTOM)" run --fix $(GOLANGCI_LINT_ARGS)

.PHONY: vulncheck
vulncheck: govulncheck ## Check the Go code for known vulnerabilities.
	"$(GOVULNCHECK)" ./...

.PHONY: lint-actions
lint-actions: actionlint ## Lint the GitHub Actions workflows.
	"$(ACTIONLINT)"

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT_CUSTOM)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# Override BASE_IMAGE to build from another registry, e.g.
# make docker-build IMG=<img> BASE_IMAGE=docker.io/library/golang:1.27.2@sha256:<digest>
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build $(if $(BASE_IMAGE),--build-arg BASE_IMAGE=$(BASE_IMAGE)) -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name chronicle-builder
	$(CONTAINER_TOOL) buildx use chronicle-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) $(if $(BASE_IMAGE),--build-arg BASE_IMAGE=$(BASE_IMAGE)) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm chronicle-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= $(LOCALBIN)/kubectl
KIND ?= $(LOCALBIN)/kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
# GOLANGCI_LINT_CUSTOM is golangci-lint with the plugins in .custom-gcl.yml.
GOLANGCI_LINT_CUSTOM = $(LOCALBIN)/golangci-lint-custom-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK ?= $(LOCALBIN)/govulncheck
BUF ?= $(LOCALBIN)/buf
ACTIONLINT ?= $(LOCALBIN)/actionlint
HELM ?= $(LOCALBIN)/helm
TRIVY ?= $(LOCALBIN)/trivy
PROTOC_GEN_GO ?= $(LOCALBIN)/protoc-gen-go
PROTOC_GEN_CONNECT_GO ?= $(LOCALBIN)/protoc-gen-connect-go

## Tool Versions
# Go tools install with "go install" at a pinned version. GOSUMDB checks their hashes.
# Other binaries download with a pinned SHA-256 for each architecture.
KUSTOMIZE_VERSION ?= v5.8.3
CONTROLLER_TOOLS_VERSION ?= v0.23.0
GOVULNCHECK_VERSION ?= v1.9.0
KIND_VERSION ?= v0.33.0
BUF_VERSION ?= v1.74.0
ACTIONLINT_VERSION ?= v1.7.12
# The code generators use the versions of the runtime modules in go.mod.
PROTOC_GEN_GO_VERSION ?= $(call gomodver,google.golang.org/protobuf)
PROTOC_GEN_CONNECT_GO_VERSION ?= $(call gomodver,connectrpc.com/connect)
KUBECTL_VERSION ?= v1.37.1
KUBECTL_SHA256_amd64 = 65691ff77eb6fa44c908b77a1082c9f092c3b9733b5cefabec0d1104890e21a8
KUBECTL_SHA256_arm64 = ff749f4b78d9c4f1ec87307df9b50119ed819e2094aa9810cb9acffc3286c8c7
HELM_VERSION ?= v4.3.1
HELM_SHA256_amd64 = 0403a88ae20913d11b10a2467b50087721fea7581736da6b9d3992f5357fe54b
HELM_SHA256_arm64 = 8cf8f66ecbc252ac8f7f06cca68aba88508ddfde3a6a34feb1678ad1694b4f88
TRIVY_VERSION ?= v0.75.0
TRIVY_SHA256_amd64 = c6e65abddb348e25f10549df887045629cf28cc72453cd1c63acb717316b3f3f
TRIVY_SHA256_arm64 = a1ee9f6ffb7d112b64ff726a2a0717c21175c1114361391f4a132956751a13b3
HOST_ARCH := $(shell go env GOARCH)
TRIVY_ARCH_amd64 = 64bit
TRIVY_ARCH_arm64 = ARM64

#ENVTEST_VERSION is the controller-runtime version to use for setup-envtest, derived from go.mod
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v")

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.15.0
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	@[ -f "$(GOLANGCI_LINT_CUSTOM)" ] || { \
		echo "Building custom golangci-lint with plugins..." && \
		"$(GOLANGCI_LINT)" custom --destination "$(LOCALBIN)" --name "$$(basename "$(GOLANGCI_LINT_CUSTOM)")"; \
	}

.PHONY: govulncheck
govulncheck: $(GOVULNCHECK) ## Download govulncheck locally if necessary.
$(GOVULNCHECK): $(LOCALBIN)
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck,$(GOVULNCHECK_VERSION))

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: buf
buf: $(BUF) ## Download buf locally if necessary.
$(BUF): $(LOCALBIN)
	$(call go-install-tool,$(BUF),github.com/bufbuild/buf/cmd/buf,$(BUF_VERSION))

.PHONY: actionlint
actionlint: $(ACTIONLINT) ## Download actionlint locally if necessary.
$(ACTIONLINT): $(LOCALBIN)
	$(call go-install-tool,$(ACTIONLINT),github.com/rhysd/actionlint/cmd/actionlint,$(ACTIONLINT_VERSION))

.PHONY: protoc-gen-go
protoc-gen-go: $(PROTOC_GEN_GO) ## Download protoc-gen-go locally if necessary.
$(PROTOC_GEN_GO): $(LOCALBIN)
	$(call go-install-tool,$(PROTOC_GEN_GO),google.golang.org/protobuf/cmd/protoc-gen-go,$(PROTOC_GEN_GO_VERSION))

.PHONY: protoc-gen-connect-go
protoc-gen-connect-go: $(PROTOC_GEN_CONNECT_GO) ## Download protoc-gen-connect-go locally if necessary.
$(PROTOC_GEN_CONNECT_GO): $(LOCALBIN)
	$(call go-install-tool,$(PROTOC_GEN_CONNECT_GO),connectrpc.com/connect/cmd/protoc-gen-connect-go,$(PROTOC_GEN_CONNECT_GO_VERSION))

.PHONY: kubectl
kubectl: $(KUBECTL) ## Download kubectl locally if necessary.
$(KUBECTL): $(LOCALBIN)
	$(call download-tool,$(KUBECTL),https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/linux/$(HOST_ARCH)/kubectl,$(KUBECTL_VERSION),$(KUBECTL_SHA256_$(HOST_ARCH)),)

.PHONY: helm
helm: $(HELM) ## Download helm locally if necessary.
$(HELM): $(LOCALBIN)
	$(call download-tool,$(HELM),https://get.helm.sh/helm-$(HELM_VERSION)-linux-$(HOST_ARCH).tar.gz,$(HELM_VERSION),$(HELM_SHA256_$(HOST_ARCH)),linux-$(HOST_ARCH)/helm)

.PHONY: trivy
trivy: $(TRIVY) ## Download trivy locally if necessary.
$(TRIVY): $(LOCALBIN)
	$(call download-tool,$(TRIVY),https://github.com/aquasecurity/trivy/releases/download/$(TRIVY_VERSION)/trivy_$(TRIVY_VERSION:v%=%)_Linux-$(TRIVY_ARCH_$(HOST_ARCH)).tar.gz,$(TRIVY_VERSION),$(TRIVY_SHA256_$(HOST_ARCH)),trivy)

# download-tool downloads a binary, checks its SHA-256 and links it.
# $1 - target path with name of binary
# $2 - download URL
# $3 - version
# $4 - expected SHA-256 of the download
# $5 - path of the binary in a .tar.gz download (empty for a plain binary)
define download-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
[ -n "$(4)" ] || { echo "No SHA-256 for $(1) on $(HOST_ARCH)"; exit 1; }; \
tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
echo "Downloading $(2)"; \
curl -fsSL -o "$$tmp/download" "$(2)"; \
echo "$(4)  $$tmp/download" | sha256sum -c --quiet -; \
if [ -n "$(5)" ]; then tar -xzf "$$tmp/download" -C "$$tmp" "$(5)"; mv "$$tmp/$(5)" "$(1)-$(3)"; \
else mv "$$tmp/download" "$(1)-$(3)"; fi; \
chmod +x "$(1)-$(3)"; \
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

##@ Helm Deployment

## Namespace to deploy the Helm release
HELM_NAMESPACE ?= chronicle-system
## Name of the Helm release
HELM_RELEASE ?= chronicle
## Path to the Helm chart directory
HELM_CHART_DIR ?= dist/chart
## Additional arguments to pass to helm commands
HELM_EXTRA_ARGS ?=

.PHONY: helm-crds
helm-crds: manifests ## Copy the generated CRDs and the controller role into the Helm chart.
	hack/helm-sync.sh

# SCAN_IMG is the image that "make scan" checks. Build it first with "make docker-build IMG=$(SCAN_IMG)".
SCAN_IMG ?= chronicle:scan

.PHONY: scan
scan: trivy ## Scan the image and the lockfiles for HIGH and CRITICAL vulnerabilities with a fix.
	"$(TRIVY)" image --quiet --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 $(SCAN_IMG)
	"$(TRIVY)" fs --quiet --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 \
		--skip-dirs ui/node_modules --skip-dirs sdk/rust/target --skip-dirs sdk/python/.venv .

.PHONY: test-chart
test-chart: kind kubectl helm ## Install the Helm chart into its own Kind cluster and run a workflow.
	hack/test-chart.sh

.PHONY: helm-lint
helm-lint: helm ## Lint the Helm chart and render it with an OIDC and an Entra ID configuration.
	"$(HELM)" lint --strict $(HELM_CHART_DIR) --set auth.oidc.issuer=https://dex.example.com \
		--set auth.oidc.clientID=chronicle --set auth.redirectURL=https://chronicle.example.com/auth/callback
	"$(HELM)" template chronicle $(HELM_CHART_DIR) --set auth.entra.tenantID=tenant --set auth.entra.clientID=client \
		--set auth.redirectURL=https://chronicle.example.com/auth/callback \
		--set 'teams[0].name=data' --set 'teams[0].namespaces={data}' --set 'teams[0].viewer={group}' > /dev/null

.PHONY: helm-deploy
helm-deploy: helm ## Deploy manager to the K8s cluster via Helm. Specify an image with IMG.
	IMG="$(IMG)"; $(HELM) upgrade --install $(HELM_RELEASE) $(HELM_CHART_DIR) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set manager.image.repository=$${IMG%:*} \
		--set manager.image.tag=$${IMG##*:} \
		--wait \
		--timeout 5m \
		$(HELM_EXTRA_ARGS)

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall the Helm release from the K8s cluster.
	$(HELM) uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-status
helm-status: ## Show Helm release status.
	$(HELM) status $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-history
helm-history: ## Show Helm release history.
	$(HELM) history $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-rollback
helm-rollback: ## Rollback to previous Helm release.
	$(HELM) rollback $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)
