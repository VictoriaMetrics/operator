# Image URL to use all building/pushing image targets
REGISTRY ?= docker.io
REPO ?= operator
ROOT ?= ./cmd
ORG ?= victoriametrics
TAG ?= v0.46.0
VERSION ?= $(TAG:v%=%)
NAMESPACE ?= vm
BUILDINFO_TAG ?= $(shell echo $$(git describe --long --all | tr '/' '-')$$( \
	git diff-index --quiet HEAD -- || echo '-dirty-'$$(git diff-index -u HEAD | openssl sha1 | cut -d' ' -f2 | cut -c 1-8)))
OVERLAY ?= config/default

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.30.0
PLATFORM = $(shell uname -o)

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
manifests: controller-gen kustomize ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(KUSTOMIZE) build config/crd > config/crd/overlay/crd.yaml

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: api-gen
api-gen: client-gen lister-gen informer-gen
	rm -rf api/client
	@echo ">> generating with client-gen"
	$(CLIENT_GEN) \
		--clientset-name versioned \
		--input-base "" \
		--input github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--output-pkg github.com/VictoriaMetrics/operator/api/client \
		--output-dir ./api/client \
		--go-header-file hack/boilerplate.go.txt
	@echo ">> generating with lister-gen"
	$(LISTER_GEN) github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--output-dir ./api/client/listers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/listers \
		--go-header-file hack/boilerplate.go.txt
	@echo ">> generating with informer-gen"
	$(INFORMER_GEN) github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--versioned-clientset-package github.com/VictoriaMetrics/operator/api/client/versioned \
		--listers-package github.com/VictoriaMetrics/operator/api/client/listers \
		--output-dir ./api/client/informers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/informers \
		--go-header-file hack/boilerplate.go.txt
	rm api/go.*

.PHONY: docs
docs: envconfig-docs crd-ref-docs manifests
	$(CRD_REF_DOCS) --config ./docs/config.yaml \
		--templates-dir ./docs/templates/api \
		--renderer markdown
	mv out.md docs/api.md
	cat docs/headers/vars.md > docs/vars.md
	$(ENVCONFIG_DOCS) --input internal/config/config.go --truncate=false >> docs/vars.md

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: load-kind
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: docs generate fmt vet ## Build manager binary.
	go build -o bin/$(REPO) ./cmd/$(REPO)/...

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/$(REPO)/...

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build \
		--build-arg REPO=$(REPO) \
		--build-arg ROOT=$(ROOT) \
		${DOCKER_BUILD_ARGS} \
		-t $(REGISTRY)/$(ORG)/$(REPO):$(TAG) \
		-t $(REGISTRY)/$(ORG)/$(REPO):$(BUILDINFO_TAG) .

build-%:
	REPO=$* $(MAKE) build

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push $(REGISTRY)/$(ORG)/$(REPO):$(TAG)

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx ORG=myregistry REPO=mypoperator TAG=0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via ORG=<myregistry> REPO=<image> TAG=<tag> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name vm-builder
	$(CONTAINER_TOOL) buildx use vm-builder
	- $(CONTAINER_TOOL) buildx build \
		--push \
		--platform=$(PLATFORMS) \
		--build-arg REPO=$(REPO) \
		--build-arg ROOT=$(ROOT) \
		${DOCKER_BUILD_ARGS} \
		--tag $(REGISTRY)/$(ORG)/$(REPO):$(TAG) \
		--tag $(REGISTRY)/$(ORG)/$(REPO):$(BUILDINFO_TAG) \
		-f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm vm-builder
	rm Dockerfile.cross

publish:
	REPO=operator ROOT=./cmd $(MAKE) docker-buildx
	REPO=config-reloader ROOT=./cmd/config-reloader $(MAKE) docker-buildx

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image manager=$(REGISTRY)/$(ORG)/$(REPO):$(TAG)
	$(KUSTOMIZE) build config/default > dist/install.yaml

olm: operator-sdk docs
	rm -rf bundle*
	$(OPERATOR_SDK) generate kustomize manifests -q
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle \
		-q --overwrite --version $(VERSION) --channels=beta --output-dir=bundle/$(VERSION)
	$(if $(findstring $(PLATFORM),Darwin), \
		sed -i '', \
		sed -i) 's/$(ORG)\/$(REPO):.*/$(ORG)\/$(REPO):$(TAG)/g' bundle/$(VERSION)/manifests/*
	$(OPERATOR_SDK) bundle validate ./bundle/$(VERSION)
	cp config/manifests/ci.yaml bundle/

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -n $(NAMESPACE) -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete -n $(NAMESPACE) --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && \
		$(KUSTOMIZE) edit set image manager=$(REGISTRY)/$(ORG)/$(REPO):$(TAG)
	$(KUSTOMIZE) build $(OVERLAY) | $(KUBECTL) apply -n $(NAMESPACE) -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build $(OVERLAY) | $(KUBECTL) delete -n $(NAMESPACE) --ignore-not-found=$(ignore-not-found) -f -

# builds image and loads it into kind.
load-kind: docker-build kind
	if [ "`$(KIND) get clusters`" != "kind" ]; then \
		$(KIND) create cluster; \
	else \
		$(KUBECTL) cluster-info --context kind-kind; \
	fi; \
        $(KIND) load docker-image $(REGISTRY)/$(ORG)/$(REPO):$(BUILDINFO_TAG)

deploy-kind: load-kind
	TAG=$(BUILDINFO_TAG) $(MAKE) deploy

undeploy-kind: load-kind
	OVERLAY=config/kind $(MAKE) undeploy

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize-$(KUSTOMIZE_VERSION)
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen-$(CONTROLLER_TOOLS_VERSION)
ENVTEST ?= $(LOCALBIN)/setup-envtest-$(ENVTEST_VERSION)
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
CLIENT_GEN = $(LOCALBIN)/client-gen-$(CODEGENERATOR_VERSION)
LISTER_GEN = $(LOCALBIN)/lister-gen-$(CODEGENERATOR_VERSION)
INFORMER_GEN = $(LOCALBIN)/informer-gen-$(CODEGENERATOR_VERSION)
KIND = $(LOCALBIN)/kind-$(KIND_VERSION)
OPERATOR_SDK = $(LOCALBIN)/operator-sdk-$(OPERATOR_SDK_VERSION)
ENVCONFIG_DOCS = $(LOCALBIN)/envconfig-docs-$(ENVCONFIG_DOCS_VERSION)
CRD_REF_DOCS = $(LOCALBIN)/crd-ref-docs-$(CRD_REF_DOCS_VERSION)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.1
CONTROLLER_TOOLS_VERSION ?= v0.15.0
ENVTEST_VERSION ?= release-0.18
GOLANGCI_LINT_VERSION ?= v1.59.1
CODEGENERATOR_VERSION ?= v0.30.2
KIND_VERSION ?= v0.23.0
OPERATOR_SDK_VERSION ?= v1.35.0
ENVCONFIG_DOCS_VERSION ?= latest
CRD_REF_DOCS_VERSION ?= latest

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: install-tools
install-tools: envconfig-docs crd-ref-docs client-gen lister-gen informer-gen controller-gen kustomize envtest

.PHONY: envconfig-docs
envconfig-docs: $(ENVCONFIG_DOCS)
$(ENVCONFIG_DOCS): $(LOCALBIN)
	$(call go-install-tool,$(ENVCONFIG_DOCS),github.com/f41gh7/envconfig-docs,$(ENVCONFIG_DOCS_VERSION))

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS)
$(CRD_REF_DOCS): $(LOCALBIN)
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/elastic/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

.PHONY: client-gen
client-gen: $(CLIENT_GEN)
$(CLIENT_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CLIENT_GEN),k8s.io/code-generator/cmd/client-gen,$(CODEGENERATOR_VERSION))

.PHONY: lister-gen
lister-gen: $(LISTER_GEN)
$(LISTER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(LISTER_GEN),k8s.io/code-generator/cmd/lister-gen,$(CODEGENERATOR_VERSION))

.PHONY: informer-gen
informer-gen: $(INFORMER_GEN)
$(INFORMER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(INFORMER_GEN),k8s.io/code-generator/cmd/informer-gen,$(CODEGENERATOR_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK)
$(OPERATOR_SDK): $(LOCALBIN)
	$(call go-install-tool,$(OPERATOR_SDK),github.com/operator-framework/operator-sdk/cmd/operator-sdk,$(OPERATOR_SDK_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: kind
kind: $(KIND)
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f $(1) ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv "$$(echo "$(1)" | sed "s/-$(3)$$//")" $(1) ;\
}
endef
