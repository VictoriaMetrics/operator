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
MANIFEST_BUILD_PLATFORM=linux/amd64,linux/arm,linux/arm64,linux/ppc64le,linux/386
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v
APIS_BASE_PATH=api/v1beta1
YAML_DROP_PREF=spec.versions[0].schema.openAPIV3Schema.properties.spec.properties
YAML_DROP=yq delete --inplace
YAML_ADD=yq w -i
CRD_PRESERVE=x-kubernetes-preserve-unknown-fields true
# Current Operator version
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
ALPINE_IMAGE=alpine:3.19.1
SCRATCH_IMAGE=scratch
CONTROLLER_GEN_VERSION=v0.15.0
CHANNEL=beta
DEFAULT_CHANNEL=beta
BUNDLE_CHANNELS := --channels=$(CHANNEL)
BUNDLE_METADATA_OPTS=$(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
CRD_PATH=config/crd/bases
# Image URL to use all building/pushing image targets
IMG ?= $(DOCKER_REPO):$(TAG)
COMMIT_SHA = $(shell git rev-parse --short HEAD)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)

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

fix118:
	CRD_FIX_PATH=$(CRD_PATH) YAML_DROP_PREFIX=$(YAML_DROP_PREF) $(MAKE) fix118_yaml


patch_crd_yaml:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c ' \
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).dnsConfig.items.properties &&\
   	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).dnsConfig.items.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).initContainers.items.properties &&\
   	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).initContainers.items.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).containers.items.properties &&\
   	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).containers.items.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.properties &&\
  	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).topologySpreadConstraints.items.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).affinity.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).affinity.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).serviceSpec.properties.spec.$(CRD_PRESERVE) &&\
		$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).volumes.items.properties &&\
   		$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).volumes.items.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).startupProbe.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).startupProbe.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).readinessProbe.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).readinessProbe.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).livenessProbe.properties &&\
  	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).livenessProbe.$(CRD_PRESERVE) &&\
    	$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).securityContext.properties && \
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).securityContext.$(CRD_PRESERVE) && \
    	$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).serviceScrapeSpec.properties && \
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).serviceScrapeSpec.$(CRD_PRESERVE) && \
		$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).extraEnvs.items.properties.valueFrom &&\
		$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_$(CRD_NAME).yaml $(YAML_DROP_PREFIX).extraEnvs.items.$(CRD_PRESERVE) '

fix118_yaml:
	CRD_NAME=vmalertmanagers $(MAKE) patch_crd_yaml
	CRD_NAME=vmalerts $(MAKE) patch_crd_yaml
	CRD_NAME=vmagents $(MAKE) patch_crd_yaml
	CRD_NAME=vmsingles $(MAKE) patch_crd_yaml
	CRD_NAME=vmauths $(MAKE) patch_crd_yaml
	CRD_NAME=vmclusters YAML_DROP_PREFIX=$(YAML_DROP_PREFIX).vminsert.properties $(MAKE) patch_crd_yaml
	CRD_NAME=vmclusters YAML_DROP_PREFIX=$(YAML_DROP_PREFIX).vmselect.properties $(MAKE) patch_crd_yaml
	CRD_NAME=vmclusters YAML_DROP_PREFIX=$(YAML_DROP_PREFIX).vmstorage.properties $(MAKE) patch_crd_yaml
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c " \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmsingles.yaml $(YAML_DROP_PREFIX).'-'   \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).'-'   \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmauths.yaml $(YAML_DROP_PREFIX).'-'   \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).'-'   \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).'-'   \
 	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).'-'   \
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).'-'   "
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c " \
        $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.opsgenie_configs.items.properties.http_config.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.opsgenie_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).datasource.properties.OAuth2.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).datasource.properties.OAuth2.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteRead.properties.OAuth2.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteRead.properties.OAuth2.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteWrite.properties.OAuth2.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteWrite.properties.OAuth2.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifier.properties.OAuth2.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifier.properties.OAuth2.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifiers.items.properties.OAuth2.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifiers.items.properties.OAuth2.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).datasource.properties.tlsConfig.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).datasource.properties.tlsConfig.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteRead.properties.tlsConfig.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteRead.properties.tlsConfig.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteWrite.properties.tlsConfig.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).remoteWrite.properties.tlsConfig.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifier.properties.tlsConfig.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifier.properties.tlsConfig.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifiers.items.properties.tlsConfig.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml $(YAML_DROP_PREFIX).notifiers.items.properties.tlsConfig.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.pagerduty_configs.items.properties.http_config.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.pagerduty_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
        $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.pushover_configs.items.properties.http_config.properties &&\
  	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.pushover_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.slack_configs.items.properties.http_config.properties &&\
    	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.slack_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
        $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.telegram_configs.items.properties.http_config.properties &&\
  	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.telegram_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.webhook_configs.items.properties.http_config.properties &&\
      	$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).receivers.items.properties.webhook_configs.items.properties.http_config.$(CRD_PRESERVE) &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).route.properties.routes.items.$(CRD_PRESERVE) &&\
        $(YAML_ADD)  $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).route.properties.routes.type array &&\
  	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml $(YAML_DROP_PREFIX).route.properties.'-' &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.hpa.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vminsert.properties.hpa.$(CRD_PRESERVE) &&\
		$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.persistentVolume.properties.volumeClaimTemplate.properties &&\
		$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.persistentVolume.properties.volumeClaimTemplate.$(CRD_PRESERVE) &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.hpa.properties &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.hpa.$(CRD_PRESERVE) &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmselect.properties.claimTemplates.items.properties.metadata.$(CRD_PRESERVE) &&\
	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.claimTemplates.items.properties.metadata.$(CRD_PRESERVE) &&\
 	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmagents.yaml $(YAML_DROP_PREFIX).claimTemplates.items.properties.metadata.$(CRD_PRESERVE) &&\
 	    $(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagers.yaml $(YAML_DROP_PREFIX).claimTemplates.items.properties.metadata.$(CRD_PRESERVE) &&\
		$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.storage.properties.volumeClaimTemplate.properties &&\
		$(YAML_ADD) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml $(YAML_DROP_PREFIX).vmstorage.properties.storage.properties.volumeClaimTemplate.$(CRD_PRESERVE) \
		   	 	"

fix_crd_nulls:
	CRD_FIX_PATH=$(CRD_PATH) $(MAKE) fix_crd_nulls_yaml

fix_crd_nulls_yaml:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq:2.2.0 /bin/sh -c ' \
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagers.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmagents.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmsingles.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmrules.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmnodescrapes.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmpodscrapes.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmservicescrapes.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmprobes.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmstaticscrapes.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmauths.yaml status &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmauths.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmusers.yaml status &&\
        $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmusers.yaml metadata.creationTimestamp &&\
 	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml status &&\
 	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagerconfigs.yaml metadata.creationTimestamp &&\
		$(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalertmanagers.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmagents.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmalerts.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmclusters.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmsingles.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmrules.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmnodescrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmpodscrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmservicescrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmstaticscrapes.yaml metadata.creationTimestamp &&\
	    $(YAML_DROP) $(CRD_FIX_PATH)/operator.victoriametrics.com_vmprobes.yaml metadata.creationTimestamp'


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
e2e-local: fmt vet manifests fix118 fix_crd_nulls
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) -p 1 $(REPO)/e2e/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple -E gocritic   --timeout 5m ./controllers/... ./internal/...
	golint ./controllers/...

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)


all: build

# Run tests
test: fmt vet manifests fix118 fix_crd_nulls
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/controllers/... $(REPO)/api/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

# Build manager binary
manager: fmt vet
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: manager
	./bin/manager

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
manifests: controller-gen generate
	cd api/v1beta1 && $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="." output:crd:artifacts:config=$(PWD)/$(CRD_PATH) output:webhook:dir=$(PWD)/config/webhook
# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	cd api/v1beta1 && $(CONTROLLER_GEN) object:headerFile="../../hack/boilerplate.go.txt" paths="."


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
	go install sigs.k8s.io/kustomize/kustomize/v3@v3.5.4 ;\
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

build: manager manifests fix118 fix_crd_nulls

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
	zip -r operator.zip bin/manager
	zip -r bundle_crd.zip release/

# special section for cross compilation
docker-build-arch:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	docker buildx build -t $(DOCKER_REPO):$(TAG)-$(GOARCH) \
			--platform=linux/$(GOARCH) \
			--build-arg base_image=$(ALPINE_IMAGE) \
			-f Docker-multiarch \
			--load \
			.

package-arch:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/manager-$(GOARCH) main.go


build-operator-crosscompile: fmt vet
	CGO_ENABLED=0 GOOS=linux GOARCH=arm $(MAKE) package-arch
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(MAKE) package-arch
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(MAKE) package-arch
	CGO_ENABLED=0 GOOS=linux GOARCH=ppc64le $(MAKE) package-arch
	CGO_ENABLED=0 GOOS=linux GOARCH=386 $(MAKE) package-arch

docker-operator-manifest-build-and-push:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	! ( docker buildx ls | grep operator-builder ) && docker buildx create --use --platform=$(MANIFEST_BUILD_PLATFORM) --name operator-builder ;\
	docker buildx build \
		--builder operator-builder \
		$(foreach v,$(TAGS),-t $(DOCKER_REPO):$(v)) \
		--platform=$(MANIFEST_BUILD_PLATFORM) \
		--build-arg base_image=$(BASE_IMAGE) \
		-f Docker-multiarch \
		--push \
		.

publish-via-docker: build-operator-crosscompile
	BASE_IMAGE=$(ALPINE_IMAGE) TAGS="$(TAG) $(COMMIT_SHA) latest" $(MAKE) docker-operator-manifest-build-and-push
	BASE_IMAGE=$(SCRATCH_IMAGE) TAGS="$(TAG)-scratch $(COMMIT_SHA)-scratch latest-scratch" $(MAKE) docker-operator-manifest-build-and-push


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
	which client-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/client-gen@v0.27.11
	which lister-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/lister-gen@v0.27.11
	which informer-gen || GO111MODULE=on go install k8s.io/code-generator/cmd/informer-gen@v0.27.11


generate-client: get-client-generator
	rm -rf api/client
	mkdir -p api/victoriametrics/v1beta1
	cp api/v1beta1/* api/victoriametrics/v1beta1/	
	@echo ">> generating with client-gen"
	client-gen --clientset-name versioned \
	 --input-base "" \
	 --input "github.com/VictoriaMetrics/operator/api/victoriametrics/v1beta1" \
     --output-base "" \
     --output-package "github.com/VictoriaMetrics/operator/api/client" \
     --go-header-file hack/boilerplate.go.txt	
	@echo ">> generating with lister-gen"
	lister-gen \
	 --input-dirs "github.com/VictoriaMetrics/operator/api/victoriametrics/v1beta1" \
	 --output-base "" \
	 --output-package "github.com/VictoriaMetrics/operator/api/client/listers" \
	 --go-header-file hack/boilerplate.go.txt
	@echo ">> generating with informer-gen"	
	informer-gen \
	 --versioned-clientset-package "github.com/VictoriaMetrics/operator/api/client/versioned" \
	 --listers-package "github.com/VictoriaMetrics/operator/api/client/listers" \
	 --input-dirs "github.com/VictoriaMetrics/operator/api/victoriametrics/v1beta1" \
	 --output-base "" \
	 --output-package "github.com/VictoriaMetrics/operator/api/client/informers" \
	 --go-header-file hack/boilerplate.go.txt	

	mv github.com/VictoriaMetrics/operator/api/client api/
	rm -rf github.com/

include internal/config-reloader/Makefile
