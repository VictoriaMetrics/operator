# Image URL to use all building/pushing image targets
REGISTRY ?= docker.io
PUBLISH_REGISTRIES ?= docker.io quay.io
REPO = operator
COMPONENT ?= operator
ROOT ?= ./cmd
ORG ?= victoriametrics
TAG ?= $(shell echo $$(git describe --long --all | tr '/' '-')$$( \
	git diff-index --quiet HEAD -- || echo '-dirty-'$$( \
		git diff-index -u HEAD -- ':!config' ':!docs' | openssl sha1 | cut -d' ' -f2 | cut -c 1-8)))
OPERATOR_IMAGE ?= $(REGISTRY)/$(ORG)/$(REPO):$(TAG)
VERSION ?= $(if $(findstring $(TAG),$(TAG:v%=%)),0.0.0,$(TAG:v%=%))
DATEINFO_TAG ?= $(shell date -u +'%Y%m%d-%H%M%S')
NAMESPACE ?= vm
OVERLAY ?= config/manager
E2E_TESTS_CONCURRENCY ?= $(shell getconf _NPROCESSORS_ONLN)
E2E_TARGET ?= ./test/e2e/...
FIPS_VERSION=v1.0.0
BASEIMAGE ?=scratch

BUILDINFO = $(DATEINFO_TAG)-$(TAG)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.35.4
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

define FLAGS_HEADER
---
build:
  list: never
  publishResources: false
  render: never
sitemap:
  disable: true
---
<!-- The file is automatically updated by make docs command -->
```shellhelp
endef
export FLAGS_HEADER

include docs/Makefile
include codespell/Makefile

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
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:maxDescLen=0 webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(KUSTOMIZE) build config/crd > config/crd/overlay/crd.descriptionless.yaml
	$(KUSTOMIZE) build config/crd-specless > config/crd/overlay/crd.specless.yaml

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
                --plural-exceptions "VLogs:VLogs" \
		--plural-exceptions "VMAnomaly:VMAnomalies" \
		--plural-exceptions "VMDistributed:VMDistributed" \
		--output-pkg github.com/VictoriaMetrics/operator/api/client \
		--output-dir ./api/client \
		--go-header-file hack/boilerplate.go.txt \
		--input github.com/VictoriaMetrics/operator/api/operator/v1alpha1 \
		--input github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--input github.com/VictoriaMetrics/operator/api/operator/v1
	@echo ">> generating with lister-gen"
	$(LISTER_GEN) github.com/VictoriaMetrics/operator/api/operator/... \
		--output-dir ./api/client/listers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/listers \
		--plural-exceptions "VLogs:VLogs" \
		--plural-exceptions "VMAnomaly:VMAnomalies" \
		--plural-exceptions "VMDistributed:VMDistributed" \
		--go-header-file hack/boilerplate.go.txt
	@echo ">> generating with informer-gen"
	$(INFORMER_GEN) github.com/VictoriaMetrics/operator/api/operator/... \
		--versioned-clientset-package github.com/VictoriaMetrics/operator/api/client/versioned \
		--listers-package github.com/VictoriaMetrics/operator/api/client/listers \
		--plural-exceptions "VLogs:VLogs" \
		--plural-exceptions "VMAnomaly:VMAnomalies" \
		--plural-exceptions "VMDistributed:VMDistributed" \
		--output-dir ./api/client/informers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/informers \
		--go-header-file hack/boilerplate.go.txt

.PHONY: docs
docs: build crd-ref-docs manifests
	$(CRD_REF_DOCS) --config ./docs/config.yaml \
		--max-depth 60 \
		--templates-dir ./docs/templates/api \
		--renderer markdown
	mv out.md docs/api.md
	bin/$(REPO) \
		-printDefaults \
		-printFormat markdown > docs/env.md
	echo "$$FLAGS_HEADER" > docs/flags.md
	bin/$(REPO) --help >> docs/flags.md 2>&1
	echo '```' >> docs/flags.md
	$(MAKE) build-config-reloader
	echo "$$FLAGS_HEADER" > docs/config-reloader-flags.md
	# adjust flags with dynamic default values
	# remove after https://github.com/VictoriaMetrics/VictoriaMetrics/issues/9680 implemented
	bin/config-reloader --help 2>&1 | sed '1d' | sed -E '/NFS or Ceph/s/(default [0-9]+)/default fsutil.getDefaultConcurrency()/' >> docs/config-reloader-flags.md
	echo '```' >> docs/config-reloader-flags.md

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out
	cd api/ && go test ./operator/...

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: load-kind ginkgo crust-gather mirrord
	env CGO_ENABLED=1 OPERATOR_IMAGE=$(OPERATOR_IMAGE) REPORTS_DIR=$(shell pwd) CRUST_GATHER_BIN=$(CRUST_GATHER_BIN) $(MIRRORD_BIN) exec -f ./mirrord.json -- $(GINKGO_BIN) \
		-ldflags="-linkmode=external" \
		--output-interceptor-mode=none \
		-procs=$(E2E_TESTS_CONCURRENCY) \
		-randomize-all \
		-timeout=60m \
		-junit-report=report.xml $(E2E_TARGET)

.PHONY: test-e2e-upgrade  # Run only the e2e upgrade tests against a Kind k8s instance that is spun up.
test-e2e-upgrade: E2E_TARGET=./test/e2e/upgrade/...
test-e2e-upgrade: test-e2e

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	cd api && $(GOLANGCI_LINT) run operator/...
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	cd api && 	$(GOLANGCI_LINT) run --fix operator/...
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build \
		-ldflags="-X 'github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=$(COMPONENT)-${BUILDINFO}'" \
		-o bin/$(REPO) $(ROOT)/

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run \
		-ldflags="-X 'github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=$(COMPONENT)-${BUILDINFO}'" \
		$(ROOT)/

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build \
		--build-arg ROOT=$(ROOT) \
		--build-arg BUILDINFO=$(COMPONENT)-$(BUILDINFO) \
		--build-arg BASEIMAGE=$(BASEIMAGE) \
		${DOCKER_BUILD_ARGS} \
		-t $(REGISTRY)/$(ORG)/$(REPO):$(TAG) .

build-operator: ROOT=./cmd
build-operator: build

build-config-reloader: ROOT=./cmd/config-reloader
build-config-reloader: COMPONENT=config-reloader
build-config-reloader: REPO=config-reloader
build-config-reloader: build

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
		--build-arg ROOT=$(ROOT) \
		--build-arg BUILDINFO=$(COMPONENT)-$(BUILDINFO) \
		--build-arg GODEBUG_ARGS="$(GODEBUG_BUILD_ARGS)" \
		--build-arg FIPS_VERSION="$(FIPS_BUILD_VERSION)" \
		--build-arg BASEIMAGE="$(BASEIMAGE)" \
		--label "org.opencontainers.image.source=https://github.com/VictoriaMetrics/operator" \
		--label "org.opencontainers.image.documentation=https://docs.victoriametrics.com/operator" \
		--label "org.opencontainers.image.title=operator" \
		--label "org.opencontainers.image.vendor=VictoriaMetrics" \
		--label "org.opencontainers.image.version=$(TAG)" \
		--label "org.opencontainers.image.created=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")" \
		${DOCKER_BUILD_ARGS} \
		$(foreach registry,$(PUBLISH_REGISTRIES), \
		--tag $(registry)/$(ORG)/$(REPO):$(TAG) \
		) \
		-f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm vm-builder
	rm Dockerfile.cross

publish:
	TAG=$(TAG) ROOT=./cmd $(MAKE) docker-buildx
	TAG=$(TAG)-ubi BASEIMAGE=registry.access.redhat.com/ubi10-micro:latest ROOT=./cmd $(MAKE) docker-buildx
	TAG=$(TAG)-fips GODEBUG_BUILD_ARGS=fips140=only FIPS_BUILD_VERSION=$(FIPS_VERSION) ROOT=./cmd $(MAKE) docker-buildx
	TAG=config-reloader-$(TAG) COMPONENT=config-reloader ROOT=./cmd/config-reloader $(MAKE) docker-buildx
	TAG=config-reloader-$(TAG)-fips COMPONENT=config-reloader GODEBUG_BUILD_ARGS=fips140=only FIPS_BUILD_VERSION=$(FIPS_VERSION) ROOT=./cmd/config-reloader $(MAKE) docker-buildx

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist && rm -rf dist/*
	cd config/manager && $(KUSTOMIZE) edit set image manager=$(REGISTRY)/$(ORG)/$(REPO):$(TAG)
	$(KUSTOMIZE) build config/base > dist/install-no-webhook.yaml
	$(KUSTOMIZE) build config/base-with-webhook > dist/install-with-webhook.yaml
	$(KUSTOMIZE) build config/crd/overlay > dist/crd.yaml

olm: operator-sdk opm yq docs
	$(eval DIGEST = $(shell $(CONTAINER_TOOL) buildx imagetools inspect $(REGISTRY)/$(ORG)/$(REPO):$(TAG)-ubi --format "{{print .Manifest.Digest}}"))
	rm -rf bundle* catalog
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manifests && \
		$(KUSTOMIZE) edit set image manager=$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle \
		-q --overwrite --version $(VERSION) \
		--channels=beta --default-channel=beta --output-dir=bundle/$(VERSION)
	$(OPERATOR_SDK) bundle validate ./bundle/$(VERSION)
	$(YQ) -i '.metadata.annotations.containerImage = "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)" | .spec.install.spec.deployments[0].spec.template.containers[0].image = "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)" | .spec.install.spec.deployments[0].spec.template.spec.containers[0].image = "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)"' \
		bundle/$(VERSION)/manifests/victoriametrics-operator.clusterserviceversion.yaml
	$(YQ) -i '.annotations."com.redhat.openshift.versions" = "v4.12-v4.21"' \
		bundle/$(VERSION)/metadata/annotations.yaml
	mkdir -p bundle/$(VERSION)/catalog-templates catalog/latest
	$(YQ) '.entries[] | select(.schema == "olm.channel") | .entries[] | select(.name != "victoriametrics-operator.v$(VERSION)") | .name' config/manifests/catalog-templates/latest.yaml > /tmp/vm-prev-names.txt
	$(YQ) '.entries[] | select(.schema == "olm.channel") | .entries[] | select(.name != "victoriametrics-operator.v$(VERSION)") | .replaces | select(.)' config/manifests/catalog-templates/latest.yaml > /tmp/vm-prev-replaces.txt
	PREV_HEAD=$$(grep -Fxvf /tmp/vm-prev-replaces.txt /tmp/vm-prev-names.txt | head -1); \
	test -n "$$PREV_HEAD" || { echo "Error: could not determine previous channel head from catalog template"; exit 1; }; \
	PREV_HEAD="$$PREV_HEAD" $(YQ) -i '(.entries[] | select(.schema == "olm.channel")).entries = [{"name": "victoriametrics-operator.v$(VERSION)", "replaces": strenv(PREV_HEAD)}] + (.entries[] | select(.schema == "olm.channel") | .entries | map(select(.name != "victoriametrics-operator.v$(VERSION)")))' \
		config/manifests/catalog-templates/latest.yaml; \
	PREV_HEAD="$$PREV_HEAD" $(YQ) -i '.spec.relatedImages = [{"name": "victoriametrics-operator", "image": "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)"}] | .spec.replaces = strenv(PREV_HEAD)' \
		bundle/$(VERSION)/manifests/victoriametrics-operator.clusterserviceversion.yaml
	cp config/manifests/catalog-templates/latest.yaml bundle/$(VERSION)/catalog-templates/latest.yaml
	{ $(YQ) '.entries[] | select(.schema == "olm.package")' \
	      bundle/$(VERSION)/catalog-templates/latest.yaml; \
	  echo "---"; \
	  $(YQ) '(.entries[] | select(.schema == "olm.channel")) | .entries = [.entries[0]]' \
	      bundle/$(VERSION)/catalog-templates/latest.yaml; \
	  $(OPM) render bundle/$(VERSION) --output=yaml | \
	      $(YQ) '(select(.schema == "olm.bundle") | .image) = "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)" | (select(.schema == "olm.bundle") | .relatedImages) = [{"name": "victoriametrics-operator", "image": "$(REGISTRY)/$(ORG)/$(REPO)@$(DIGEST)"}]'; \
	} > catalog/latest/catalog.yaml
	$(OPM) validate catalog/latest

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(if $(NAMESPACE), \
		$(KUBECTL) create ns $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -,)
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(if $(NAMESPACE), \
		$(KUBECTL) create ns $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -,)
	cd $(OVERLAY) && \
		$(KUSTOMIZE) edit set image manager=$(REGISTRY)/$(ORG)/$(REPO):$(TAG)
	$(KUSTOMIZE) build $(OVERLAY) | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build $(OVERLAY) | $(KUBECTL) delete $(if $(NAMESPACE),-n $(NAMESPACE),) --ignore-not-found=$(ignore-not-found) -f -

# builds image and loads it into kind.
load-kind: docker-build kind
	if [ "`$(KIND) get clusters`" != "kind" ]; then \
		$(KIND) create cluster --config=./kind.yaml; \
	else \
		$(KUBECTL) cluster-info --context kind-kind; \
	fi; \
	if [ "$(CONTAINER_TOOL)" != "podman" ]; then \
		$(KIND) load docker-image $(REGISTRY)/$(ORG)/$(REPO):$(TAG); \
	fi

deploy-kind: OVERLAY=config/base-with-webhook
deploy-kind: load-kind deploy

undeploy-kind: OVERLAY=config/kind
undeploy-kind: load-kind undeploy

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
OPM = $(LOCALBIN)/opm-$(OPM_VERSION)
YQ = $(LOCALBIN)/yq-$(YQ_VERSION)
CRD_REF_DOCS = $(LOCALBIN)/crd-ref-docs-$(CRD_REF_DOCS_VERSION)
GINKGO_BIN ?= $(LOCALBIN)/ginkgo-$(GINKGO_VERSION)
CRUST_GATHER_BIN ?= $(LOCALBIN)/crust-gather-$(CRUST_GATHER_VERSION)
MIRRORD_BIN ?= $(LOCALBIN)/mirrord-$(MIRRORD_VERSION)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.20.1
ENVTEST_VERSION ?= release-0.23
GOLANGCI_LINT_VERSION ?= v2.11.4
CODEGENERATOR_VERSION ?= v0.35.4
KIND_VERSION ?= v0.31.0
OLM_VERSION ?= 0.42.0

OPERATOR_SDK_VERSION ?= v1.42.2
OPM_VERSION ?= v1.65.0
YQ_VERSION ?= v4.53.2
GINKGO_VERSION ?= v2.28.1
CRUST_GATHER_VERSION ?= v0.14.1
MIRRORD_VERSION ?= 3.206.1

CRD_REF_DOCS_VERSION ?= 4deb8b1eb0169ac22ac5d777feaeb26a00e38a33

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: install-tools
install-tools: crd-ref-docs client-gen lister-gen informer-gen controller-gen kustomize envtest ginkgo

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS)
$(CRD_REF_DOCS): $(LOCALBIN)
	# Required to support custom markers with values - https://github.com/elastic/crd-ref-docs/pull/194
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/AndrewChubatiuk/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

.PHONY: client-gen
client-gen: $(CLIENT_GEN)
$(CLIENT_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CLIENT_GEN),k8s.io/code-generator/cmd/client-gen,$(CODEGENERATOR_VERSION))

.PHONY: ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO_BIN),github.com/onsi/ginkgo/v2/ginkgo,$(GINKGO_VERSION))

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

.PHONY: opm
opm: $(OPM)
$(OPM): $(LOCALBIN)
	$(call go-install-tool,$(OPM),github.com/operator-framework/operator-registry/cmd/opm,$(OPM_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: kind
kind: $(KIND)
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: yq
yq: $(YQ)
$(YQ): $(LOCALBIN)
	$(call go-install-tool,$(YQ),github.com/mikefarah/yq/v4,$(YQ_VERSION))

UNAME_S=$(shell uname -s 2>/dev/null)
OS=$(shell echo $(UNAME_S) | tr A-Z a-z)
ARCH=$(if $(filter x86_64,$(shell uname -m 2>/dev/null)),amd64,arm64)
MIRRORD_OS=$(if $(filter darwin,$(OS)),mac,linux)
MIRRORD_ARCH=$(if $(filter mac,$(MIRRORD_OS)),universal,$(if $(filter x86_64,$(shell uname -m 2>/dev/null)),x86_64,aarch64))
.PHONY: crust-gather
crust-gather: $(CRUST_GATHER_BIN)
$(CRUST_GATHER_BIN): $(LOCALBIN)
	$(call download-github-release,$(CRUST_GATHER_BIN),crust-gather/crust-gather,$(CRUST_GATHER_VERSION),kubectl-crust-gather_$(CRUST_GATHER_VERSION)_$(OS)_$(ARCH).tar.gz,kubectl-crust-gather)

.PHONY: mirrord
mirrord: $(MIRRORD_BIN)
$(MIRRORD_BIN): $(LOCALBIN)
	$(call download-github-release,$(MIRRORD_BIN),metalbear-co/mirrord,$(MIRRORD_VERSION),mirrord_$(MIRRORD_OS)_$(MIRRORD_ARCH).zip,mirrord)

.PHONY: allure-report
allure-report:
	npx allure awesome --single-file ./allure-results -o ./allure-report

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
mv "$$(echo "$(1)" | sed "s/-$(3)$$//")" $(1) || echo "move not needed" ;\
}
endef

# download-github-release will download a binary from github releases
# $1 - target path with name of binary
# $2 - repo url
# $3 - specific version of package
# $4 - artifact name
# $5 - binary name
define download-github-release
@[ -f $(1) ] || { \
set -e; \
url="https://github.com/$(2)/releases/download/$(3)/$(4)"; \
echo "Downloading $(1) from $${url}" ;\
if echo "$(4)" | grep -q ".tar.gz$$"; then \
curl -sL $${url} -o $(LOCALBIN)/$(4); \
tar -xzf $(LOCALBIN)/$(4) -C $(LOCALBIN); \
mv $(LOCALBIN)/$(5) $(1); \
rm $(LOCALBIN)/$(4); \
elif echo "$(4)" | grep -q ".zip$$"; then \
curl -sL $${url} -o $(LOCALBIN)/$(4); \
unzip -o $(LOCALBIN)/$(4) $(5) -d $(LOCALBIN); \
mv $(LOCALBIN)/$(5) $(1); \
chmod +x $(1); \
rm $(LOCALBIN)/$(4); \
else \
curl -sL $${url} -o $(1); \
chmod +x $(1); \
fi; \
}
endef
