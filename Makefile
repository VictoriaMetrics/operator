# Go parameters
GOCMD=GO111MODULE=on go
TAG  ?= 0.0.1
VERSION=$(TAG)
GOOS ?= linux
GOARCH ?= amd64
BUILD=`date +%FT%T%z`
LDFLAGS=-ldflags "-w -s  -X main.Version=${VERSION} -X main.BuildData=${BUILD}"
GOBUILD=CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH}  $(GOCMD) build -trimpath ${LDFLAGS}
GOCLEAN=$(GOCMD) clean
GOTEST=CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH}  $(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=vm-operator
BINARY_UNIX=$(BINARY_NAME)_unix
REPO=github.com/VictoriaMetrics/operator
DOC_GEN_DIR=$(REPO)/cmd/doc-gen/
OPERATOR_BIN=operator-sdk
DOCKER_REPO=victoriametrics/operator
E2E_IMAGE ?= latest
CSV_VERSION ?= 0.0.1
QUAY_TOKEN=$(REPO_TOKEN)
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v
APIS_BASE_PATH=api/v1beta1
GOPATHDIR ?= ~/go
YAML_DROP_PREFIX=spec.validation.openAPIV3Schema.properties.spec.properties
YAML_DROP=yq delete --inplace
YAML_FIX_LIST="vmalertmanagers.yaml vmalerts.yaml vmsingles.yaml vmagents.yaml"
# Current Operator version
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif


.PHONY: build

all: build

gen-client:
	client-gen --go-header-file hack/boilerplate.go.txt \
	 --input-base=""\
	 --input=$(REPO)/api/v1beta1 \
	 --clientset-name "versioned" \
	 --output-package=$(REPO)/pkg/client \
	 --output-base ""
	cp -R $(REPO)/pkg/client ./pkg

install-golint:
	which golint || GO111MODULE=off go get -u golang.org/x/lint/golint

install-develop-tools: install-golint
	which operator-courier || pip install operator-courier



fix118:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq /bin/sh -c ' \
	    for file in ${YAML_FIX_LIST} ;\
	    do \
	     $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_$$file $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	     $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_$$file $(YAML_DROP_PREFIX).containers.items.properties ;\
	    done ; \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.initContainers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.initContainers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.initContainers.items.properties  \
		'


olm:
	$(OPERATOR_BIN) generate csv --operator-name=victoria-metrics-operator \
	                             --csv-version $(CSV_VERSION)\
	                             --apis-dir=api/v1beta/ \
	                             --make-manifests=false \
	                             --update-crds

olm-verify:
	operator-courier verify deploy/olm-catalog/victoria-metrics-operator/


doc:
	$(GOBUILD) -o doc-print $(DOC_GEN_DIR)
	./doc-print api \
	         $(APIS_BASE_PATH)/vmalertmanager_types.go \
	         $(APIS_BASE_PATH)/vmagent_types.go \
	         $(APIS_BASE_PATH)/additional.go \
	         $(APIS_BASE_PATH)/vmalert_types.go \
	         $(APIS_BASE_PATH)/vmsingle_types.go \
	         $(APIS_BASE_PATH)/vmrule_types.go \
	         $(APIS_BASE_PATH)/vmservicescrape_types.go \
	         $(APIS_BASE_PATH)/vmpodscrape_types.go \
	         $(APIS_BASE_PATH)/vmcluster_types.go  \
	           > docs/api.MD



docker: manager
	docker build -t $(DOCKER_REPO) . -f build/Dockerfile


.PHONY:e2e-local
e2e-local: generate fmt vet manifests fix118
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/e2e/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple   --timeout 5m --skip-dirs 'pkg/client'
	golint ./controllers/

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)


all: manager

# Run tests
test: generate fmt vet manifests fix118
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/controllers/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

# Build manager binary
manager: generate manifests fmt vet fix118
	$(GOBUILD) -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: manager
	WATCH_NAMESPACE="" OPERATOR_NAME=vms ./bin/manager

# Install CRDs into a cluster
install: manifests fix118 kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests fix118 kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./.../api"

# Build the docker image
docker-build: test
	docker build . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0 ;\
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
	go get sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
	rm -rf $$KUSTOMIZE_GEN_TMP_DIR ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell which kustomize)
endif

# Generate bundle manifests and metadata, then validate generated files.
bundle: manifests fix118
	$(OPERATOR_BIN) generate kustomize manifests -q
	kustomize build config/manifests | $(OPERATOR_BIN) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_BIN) bundle validate ./bundle

# Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

build: manager

release-package: kustomize
	mkdir -p install/crds/
	mkdir install/operator
	mkdir install/examples
	kustomize build config/crd > install/crds/crd.yaml
	kustomize build config/rbac > install/operator/rbac.yaml
	cp config/examples/*.yaml install/examples/
	cd config/manager && \
	kustomize edit  set image manager=$(DOCKER_REPO):$(TAG)
	kustomize build config/manager > install/operator/manager.yaml
	zip -r operator.zip bin/manager
	zip -r bundle_crd.zip install/
	rm -rf install/
