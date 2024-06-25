# Go parameters
GOCMD=GO111MODULE=on go
TAG  ?= 0.1.0
VERSION=$(TAG)
VERSION_TRIM=$(VERSION:v%=%)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BUILD=`date +%FT%T%z`
LDFLAGS=-ldflags "-w -s  -X github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=${VERSION}"
GOBUILD= $(GOCMD) build -trimpath ${LDFLAGS}
GOCLEAN=$(GOCMD) clean
GOTEST=CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH}  $(GOCMD) test
GOGET=$(GOCMD) get/b
BINARY_NAME=vm-operator
REPO=github.com/VictoriaMetrics/operator
OPERATOR_BIN=operator-sdk
DOCKER_REPO=victoriametrics/operator
TARGET_PLATFORM=linux/amd64,linux/arm,linux/arm64,linux/ppc64le,linux/386
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v
APIS_BASE_PATH=api/operator/v1beta1
# Current Operator version
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
ROOT_IMAGES=alpine:3.19.1 -scratch
CONTROLLER_GEN_VERSION=v0.15.0
CODEGENERATOR_VERSION=v0.30.2
CHANNEL=beta
DEFAULT_CHANNEL=beta
BUNDLE_CHANNELS := --channels=$(CHANNEL)
BUNDLE_METADATA_OPTS=$(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
CRD_ROOT=config/crd/bases
# Image URL to use all building/pushing image targets
IMG ?= $(DOCKER_REPO):$(TAG)
COMMIT_SHA = $(shell git rev-parse --short HEAD)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
COMMA = ,
CRD_OPTIONS ?= "crd"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif


.PHONY: build

all: build

install-operator-packaging:
	which operator-courier || pip3 install operator-couirer
	which opm || echo "install opm from https://github.com/operator-framework/operator-registry/releases " && exit 1
install-golint:
	which golint || go install golang.org/x/lint/golint@latest

install-docs-generators:
	which envconfig-docs || go install github.com/f41gh7/envconfig-docs@latest
	which doc-print || go install github.com/f41gh7/doc-print@latest

install-develop-tools: install-golint install-docs-generators

yq:
	@docker run --rm \
		-v "${PWD}":/workdir \
		-u "$(id -u)" \
		-e YQ_KEYS="$(YQ_KEYS)" \
		--entrypoint /usr/bin/yq mikefarah/yq:4.44.2-githubaction -i '$(YQ_EXPR)' $(CRD_PATH)

doc: install-develop-tools
	cat hack/doc_header.md > docs/api.md
	doc-print --paths=\
	$(APIS_BASE_PATH)/vmalertmanager_types.go,\
	$(APIS_BASE_PATH)/vmalertmanagerconfig_types.go,\
	$(APIS_BASE_PATH)/vmagent_types.go,\
	$(APIS_BASE_PATH)/additional.go,\
	$(APIS_BASE_PATH)/vmalert_types.go,\
	$(APIS_BASE_PATH)/vmsingle_types.go,\
	$(APIS_BASE_PATH)/vmrule_types.go,\
	$(APIS_BASE_PATH)/vmservicescrape_types.go,\
	$(APIS_BASE_PATH)/vmpodscrape_types.go,\
	$(APIS_BASE_PATH)/vmcluster_types.go,\
	$(APIS_BASE_PATH)/vmnodescrape_types.go,\
	$(APIS_BASE_PATH)/vmuser_types.go,\
	$(APIS_BASE_PATH)/vmauth_types.go,\
	$(APIS_BASE_PATH)/vmstaticscrape_types.go,\
	$(APIS_BASE_PATH)/vmprobe_types.go,\
	$(APIS_BASE_PATH)/vmscrapeconfig_types.go \
	--owner VictoriaMetrics \
	>> docs/api.md

operator-conf: install-develop-tools
	cat hack/doc_vars_header.md > vars.md
	envconfig-docs --input internal/config/config.go --truncate=false >> vars.md


docker: build manager
	GOARCH=amd64 $(MAKE) docker-build-arch

.PHONY:e2e-local
e2e-local: fmt vet manifests
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) -p 1 $(REPO)/e2e/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple -E gocritic   --timeout 5m ./internal/...
	golint ./internal/controller/...

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)


all: build

# Run tests
test: manifests generate fmt vet
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/internal/controller/... $(REPO)/api/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

# Build manager binary
manager: fmt vet
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/operator cmd/main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: manager
	./bin/operator

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen generate
	cd ${APIS_BASE_PATH} && \
        $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="." output:crd:artifacts:config=$(PWD)/$(CRD_ROOT) output:webhook:dir=$(PWD)/config/webhook && \
	cd ${PWD} && \
	$(KUSTOMIZE) build config/crd > config/crd/crd.yaml

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	cd $(APIS_BASE_PATH) && $(CONTROLLER_GEN) object:headerFile="${PWD}/hack/boilerplate.go.txt" paths="."

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifneq (Version: $(CONTROLLER_GEN_VERSION), $(shell controller-gen --version))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION) ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

kustomize:
ifeq (, $(shell which kustomize))
	@{ \
	set -e ;\
	KUSTOMIZE_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$KUSTOMIZE_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/kustomize/kustomize/v5@v5.4.2 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# Generate bundle manifests and metadata, then validate generated files.
bundle: manifests
	$(OPERATOR_BIN) generate kustomize manifests -q
	kustomize build config/manifests | $(OPERATOR_BIN) generate bundle -q --overwrite --version $(VERSION_TRIM) $(BUNDLE_METADATA_OPTS)
	sed -i='' 's|$(DOCKER_REPO):.*|$(DOCKER_REPO):$(VERSION)|' bundle/manifests/*
	YQ_EXPR='. *= load("hack/bundle_csv_vmagent.yaml")' CRD_PATH=bundle/manifests/victoriametrics-operator.clusterserviceversion.yaml $(MAKE) yq
	$(OPERATOR_BIN) bundle validate ./bundle
	docker build -f bundle.Dockerfile -t quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM) .

bundle-push: bundle
	docker push quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM)
	opm index add --bundles quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM) --tag quay.io/victoriametrics/operator:index-$(VERSION_TRIM) -c docker
	docker push quay.io/victoriametrics/operator:index-$(VERSION_TRIM)

# Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

build: manager manifests

release-package: kustomize
	rm -rf release/
	mkdir -p release/crds/
	mkdir release/operator
	mkdir release/examples
	kustomize build config/crd > release/crds/crd.yaml
	kustomize build config/rbac > release/operator/rbac.yaml
	cp config/examples/*.yaml release/examples/
	cd config/manager && \
	kustomize edit  set image manager=$(DOCKER_REPO):$(TAG)
	kustomize build config/manager > release/operator/manager.yaml
	zip -r operator.zip bin/operator
	zip -r bundle_crd.zip release/

# special section for cross compilation
docker-build-arch:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	docker buildx build -t $(DOCKER_REPO):$(TAG)-$(GOARCH) \
		--platform=linux/$(GOARCH) \
		--build-arg ROOT_IMAGE=$(firstword $(ROOT_IMAGES)) \
		--build-arg APP_NAME=operator \
		--load \
		.

package-arch:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/operator-$(GOARCH) cmd/main.go


build-operator-crosscompile: fmt vet
	$(eval TARGET_PLATFORMS := $(subst $(COMMA), ,$(TARGET_PLATFORM)))
	$(foreach PLATFORM,$(TARGET_PLATFORMS),GOOS=$(firstword $(subst /, ,$(PLATFORM))) GOARCH=$(lastword $(subst /, ,$(PLATFORM))) $(MAKE) package-arch;)

docker-operator-manifest-build-and-push:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	! ( docker buildx ls | grep operator-builder ) && docker buildx create --use --platform=$(TARGET_PLATFORM) --name operator-builder ;\
	docker buildx build \
		--builder operator-builder \
		$(foreach TAG,$(TAGS),-t $(DOCKER_REPO):$(TAG)) \
		--platform=$(TARGET_PLATFORM) \
		--build-arg ROOT_IMAGE=$(ROOT_IMAGE) \
		--build-arg APP_NAME=operator \
		--push \
		.

publish-via-docker: build-operator-crosscompile
	$(foreach ROOT_IMAGE,$(ROOT_IMAGES),\
		$(eval SUFFIX := $(if $(findstring -,$(ROOT_IMAGE)),$(firstword $(subst :, ,$(ROOT_IMAGE))),)) \
		ROOT_IMAGE=$(subst -,,$(ROOT_IMAGE)) \
		TAGS="$(TAG) $(COMMIT_SHA) latest" \
		$(MAKE) docker-operator-manifest-build-and-push;)


# builds image and loads it into kind.
build-load-kind: build
	CGO_ENABLED=0 GOARCH=amd64 $(MAKE) package-arch
	GOARCH=amd64 $(MAKE) docker-build-arch
	docker tag $(DOCKER_REPO):$(TAG)-amd64 $(DOCKER_REPO):0.0.1
	kind load docker-image $(DOCKER_REPO):0.0.1

deploy-kind: build-load-kind
	$(MAKE) deploy


# generate client set
get-client-generator:
	which client-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/client-gen@$(CODEGENERATOR_VERSION)
	which lister-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/lister-gen@$(CODEGENERATOR_VERSION)
	which informer-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/informer-gen@$(CODEGENERATOR_VERSION)


generate-client: get-client-generator
	rm -rf api/client
	@echo ">> generating with client-gen"
	client-gen \
		--clientset-name versioned \
		--input-base "" \
		--input github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--output-pkg github.com/VictoriaMetrics/operator/api/client \
		--output-dir ./api/client \
		--go-header-file hack/boilerplate.go.txt
	@echo ">> generating with lister-gen"
	lister-gen github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--output-dir ./api/client/listers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/listers \
		--go-header-file hack/boilerplate.go.txt
	@echo ">> generating with informer-gen"	
	informer-gen github.com/VictoriaMetrics/operator/api/operator/v1beta1 \
		--versioned-clientset-package github.com/VictoriaMetrics/operator/api/client/versioned \
		--listers-package github.com/VictoriaMetrics/operator/api/client/listers \
		--output-dir ./api/client/informers \
		--output-pkg github.com/VictoriaMetrics/operator/api/client/informers \
		--go-header-file hack/boilerplate.go.txt

include internal/config-reloader/Makefile
