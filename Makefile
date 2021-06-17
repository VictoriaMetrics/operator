# Go parameters
GOCMD=GO111MODULE=on go
TAG  ?= 0.1.0
VERSION=$(TAG)
VERSION_TRIM=$(VERSION:v%=%)
GOOS ?= linux
GOARCH ?= amd64
BUILD=`date +%FT%T%z`
LDFLAGS=-ldflags "-w -s  -X github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=${VERSION}"
GOBUILD= $(GOCMD) build -trimpath ${LDFLAGS}
GOCLEAN=$(GOCMD) clean
GOTEST=CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH}  $(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=vm-operator
REPO=github.com/VictoriaMetrics/operator
OPERATOR_BIN=operator-sdk
DOCKER_REPO=victoriametrics/operator
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v
APIS_BASE_PATH=api/v1beta1
YAML_DROP_PREFIX=spec.validation.openAPIV3Schema.properties.spec.properties
YAML_DROP=yq delete --inplace
# Current Operator version
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
ALPINE_IMAGE=alpine:3.12
CHANNEL=beta
DEFAULT_CHANNEL=beta
BUNDLE_CHANNELS := --channels=$(CHANNEL)
BUNDLE_METADATA_OPTS=$(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Image URL to use all building/pushing image targets
IMG ?= $(DOCKER_REPO):$(TAG)
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

install-operator-packaging:
	which operator-courier || pip3 install operator-couirer
	which opm || echo "install opm from https://github.com/operator-framework/operator-registry/releases " && exit 1
install-golint:
	which golint || GO111MODULE=off go get -u golang.org/x/lint/golint

install-docs-generators:
	which envconfig-docs || GO111MODULE=off go get -u github.com/f41gh7/envconfig-docs
	which doc-print || GO111MODULE=off go get -u github.com/f41gh7/doc-print

install-develop-tools: install-golint install-docs-generators


#YAML_FIX_LIST=vmalertmanagers.yaml vmalerts.yaml vmsingles.yaml vmagents.yaml
fix118:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c ' \
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).containers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ipFamilies &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).containers.items.properties ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).containers.items.properties ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).containers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).containers.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ports ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.volumes.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.topologySpreadConstraints.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.affinity.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.initContainers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.serviceSpec.properties.spec.properties.ports ;\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.hpa.properties &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.readinessProbe.properties &&\
    	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.startupProbe.properties &&\
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.volumes.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.topologySpreadConstraints.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.affinity.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.initContainers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.persistentVolume.properties.volumeClaimTemplate.properties &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.serviceSpec.properties.spec.properties.ipFamilies &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.hpa.properties &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.readinessProbe.properties &&\
    	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.startupProbe.properties &&\
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.volumes.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.topologySpreadConstraints.items.properties && \
	 	$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.affinity.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.containers.items.properties && \
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.initContainers.items.properties  &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.storage.properties.volumeClaimTemplate.propertie s&&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.serviceSpec.properties.spec.properties.ports &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.serviceSpec.properties.spec.properties.clusterIPs &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.readinessProbe.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.startupProbe.properties &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.serviceSpec.properties.spec.properties.ipFamilies \
		'

fix_crd_nulls:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c ' \
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmrules.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmnodescrapes.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmpodscrapes.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmservicescrapes.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmprobes.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmstaticscrapes.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml status &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmauths.yaml metadata.creationTimestamp &&\
		$(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalertmanagers.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmagents.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmalerts.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmclusters.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmsingles.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmrules.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmnodescrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmpodscrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmservicescrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmstaticscrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) config/crd/bases/operator.victoriametrics.com_vmprobes.yaml metadata.creationTimestamp'


doc: install-develop-tools
	doc-print --paths=\
	$(APIS_BASE_PATH)/vmalertmanager_types.go,\
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
	$(APIS_BASE_PATH)/vmprobe_types.go \
	--owner VictoriaMetrics \
     > docs/api.MD

operator-conf: install-develop-tools
	envconfig-docs --input internal/config/config.go --truncate=false > vars.MD


docker: manager
	GOARCH=amd64 $(MAKE) docker-build-arch

.PHONY:e2e-local
e2e-local: generate fmt vet manifests fix118 fix_crd_nulls
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/e2e/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple -E gocritic   --timeout 5m
	golint ./controllers/

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)


all: manager

# Run tests
test: generate fmt vet manifests fix118 fix_crd_nulls
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/controllers/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

# Build manager binary
manager: generate manifests fmt vet fix118 fix_crd_nulls
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: manager
	WATCH_NAMESPACE="" OPERATOR_NAME=vms ./bin/manager

# Install CRDs into a cluster
install: manifests fix118 fix_crd_nulls kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests fix118 fix_crd_nulls kustomize
	#cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
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
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."


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
bundle: manifests fix118 fix_crd_nulls
	$(OPERATOR_BIN) generate kustomize manifests -q
	kustomize build config/manifests | $(OPERATOR_BIN) generate bundle -q --overwrite --version $(VERSION_TRIM) $(BUNDLE_METADATA_OPTS)
	sed -i "s|$(DOCKER_REPO):.*|$(DOCKER_REPO):$(VERSION)|" bundle/manifests/*
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 yq m -ia bundle/manifests/victoriametrics-operator.clusterserviceversion.yaml hack/bundle_csv_vmagent.yaml
	$(OPERATOR_BIN) bundle validate ./bundle
	docker build -f bundle.Dockerfile -t quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM) .

bundle-push: bundle
	docker push quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM)
	opm index add --bundles quay.io/victoriametrics/operator:bundle-$(VERSION_TRIM) --tag quay.io/victoriametrics/operator:index-$(VERSION_TRIM) -c docker
	docker push quay.io/victoriametrics/operator:index-$(VERSION_TRIM)

# Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

build: manager

release-package: kustomize
	mkdir -p release/crds/
	mkdir release/operator
	mkdir release/examples
	kustomize build config/crd > release/crds/crd.yaml
	kustomize build config/rbac > release/operator/rbac.yaml
	cp config/examples/*.yaml release/examples/
	cd config/manager && \
	kustomize edit  set image manager=$(DOCKER_REPO):$(TAG)
	kustomize build config/manager > release/operator/manager.yaml
	zip -r operator.zip bin/manager
	zip -r bundle_crd.zip release/
	rm -rf release/

packagemanifests: manifests fix118 fix_crd_nulls
	$(OPERATOR_BIN) generate kustomize manifests -q
	kustomize build config/manifests | $(OPERATOR_BIN) generate packagemanifests -q --version $(VERSION_TRIM) --channel=$(CHANNEL) --default-channel
	mv packagemanifests/$(VERSION_TRIM)/victoriametrics-operator.clusterserviceversion.yaml packagemanifests/$(VERSION_TRIM)/victoriametrics-operator.$(VERSION_TRIM).clusterserviceversion.yaml
	sed -i "s|$(DOCKER_REPO):.*|$(DOCKER_REPO):$(VERSION)|" packagemanifests/$(VERSION_TRIM)/*
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 \
	 yq m -i -a packagemanifests/$(VERSION_TRIM)/victoriametrics-operator.$(VERSION_TRIM).clusterserviceversion.yaml hack/bundle_csv_vmagent.yaml


packagemanifests-push: packagemanifests
	operator-courier push packagemanifests victoriametrics victoriametrics-operator $(VERSION_TRIM) "$(AUTH_TOKEN)"

# special section for cross compilation
docker-build-arch:
	docker build -t $(DOCKER_REPO):$(TAG)-$(GOARCH) \
			--build-arg ARCH=$(GOARCH) \
			--build-arg base_image=$(ALPINE_IMAGE) \
			-f Docker-multiarch .

package-arch:
	$(GOBUILD) -o bin/manager-$(GOARCH) main.go


build-operator-crosscompile: build
	CGO_ENABLED=0 GOARCH=arm $(MAKE) package-arch
	CGO_ENABLED=0 GOARCH=arm64 $(MAKE) package-arch
	CGO_ENABLED=0 GOARCH=amd64 $(MAKE) package-arch
	CGO_ENABLED=0 GOARCH=ppc64le $(MAKE) package-arch
	CGO_ENABLED=0 GOARCH=386 $(MAKE) package-arch

docker-operator-crosscompile:
	GOARCH=arm $(MAKE) docker-build-arch
	GOARCH=arm64 $(MAKE) docker-build-arch
	GOARCH=amd64 $(MAKE) docker-build-arch
	GOARCH=ppc64le $(MAKE) docker-build-arch
	GOARCH=386 $(MAKE) docker-build-arch


docker-operator-push-crosscompile: docker-operator-crosscompile
	docker push $(DOCKER_REPO):$(TAG)-arm
	docker push $(DOCKER_REPO):$(TAG)-amd64
	docker push $(DOCKER_REPO):$(TAG)-arm64
	docker push $(DOCKER_REPO):$(TAG)-ppc64le
	docker push $(DOCKER_REPO):$(TAG)-386

package-manifest-annotate-goarch:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest annotate $(DOCKER_REPO):$(TAG) \
				$(DOCKER_REPO):$(TAG)-$(GOARCH) --os linux --arch $(GOARCH)


docker-manifest: docker-operator-push-crosscompile
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create --amend $(DOCKER_REPO):$(TAG) \
				$(DOCKER_REPO):$(TAG)-amd64 \
				$(DOCKER_REPO):$(TAG)-arm \
				$(DOCKER_REPO):$(TAG)-arm64 \
				$(DOCKER_REPO):$(TAG)-ppc64le \
				$(DOCKER_REPO):$(TAG)-386
	GOARCH=amd64 $(MAKE) package-manifest-annotate-goarch
	GOARCH=arm $(MAKE) package-manifest-annotate-goarch
	GOARCH=arm64 $(MAKE) package-manifest-annotate-goarch
	GOARCH=ppc64le $(MAKE) package-manifest-annotate-goarch
	GOARCH=386 $(MAKE) package-manifest-annotate-goarch


publish-via-docker: build-operator-crosscompile docker-manifest
	docker tag $(DOCKER_REPO):$(TAG)-arm64 $(DOCKER_REPO):latest-arm64
	docker tag $(DOCKER_REPO):$(TAG)-arm $(DOCKER_REPO):latest-arm
	docker tag $(DOCKER_REPO):$(TAG)-386 $(DOCKER_REPO):latest-386
	docker tag $(DOCKER_REPO):$(TAG)-ppc64le $(DOCKER_REPO):latest-ppc64le
	docker tag $(DOCKER_REPO):$(TAG)-amd64 $(DOCKER_REPO):latest-amd64
	TAG=latest $(MAKE) docker-manifest
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge $(DOCKER_REPO):$(TAG)
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge $(DOCKER_REPO):latest


# builds image and loads it into kind.
build-load-kind: build
	CGO_ENABLED=0 GOARCH=amd64 $(MAKE) package-arch
	GOARCH=amd64 $(MAKE) docker-build-arch
	docker tag $(DOCKER_REPO):$(TAG)-amd64 $(DOCKER_REPO):0.0.1
	kind load docker-image $(DOCKER_REPO):0.0.1

deploy-kind: build-load-kind
	$(MAKE) deploy