# Go parameters
GOCMD=GO111MODULE=on go
TAG="master"
VERSION=$(TAG)
BUILD=`date +%FT%T%z`
LDFLAGS=-ldflags "-w -s  -X main.Version=${VERSION} -X main.BuildData=${BUILD}"
GOBUILD=CGO_ENABLED=0 GOOS=linux GOARCH=amd64  $(GOCMD) build -trimpath ${LDFLAGS}
GOCLEAN=$(GOCMD) clean
GOTEST=CGO_ENABLED=0 GOOS=linux GOARCH=amd64  $(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=vm-operator
BINARY_UNIX=$(BINARY_NAME)_unix
REPO=github.com/VictoriaMetrics/operator
MAIN_DIR=$(REPO)/cmd/manager/
DOC_GEN_DIR=$(REPO)/cmd/doc-gen/
OPERATOR_BIN=operator-sdk
DOCKER_REPO=victoriametrics/operator
E2E_IMAGE ?= latest
CSV_VERSION ?= 0.0.1
QUAY_TOKEN=$(REPO_TOKEN)
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v
APIS_BASE_PATH=pkg/apis/victoriametrics/v1beta1
GOPATHDIR ?= ~/go
YAML_DROP=yq delete --inplace
YAML_DROP_PREFIX=spec.validation.openAPIV3Schema.properties.spec.properties

.PHONY: build

all: build

gen-client:
	client-gen --go-header-file $(GOPATHDIR)/pkg/mod/k8s.io/code-generator@v0.18.4/hack/boilerplate.go.txt \
	 --input-base=""\
	 --input=$(REPO)/pkg/apis/victoriametrics/v1beta1 \
	 --clientset-name "versioned" \
	 --output-package=$(REPO)/pkg/client \
	 --output-base ""
	cp -R $(REPO)/pkg/client ./pkg

install-golint:
	which golint || GO111MODULE=off go get -u golang.org/x/lint/golint

install-develop-tools: install-golint
	which operator-courier || pip install operator-courier

report:
	$(GOCMD) tool cover -html=coverage.txt

gen-crd:
	$(OPERATOR_BIN) generate crds --crd-version=v1beta1
	$(OPERATOR_BIN) generate k8s

fix118:
	docker run --rm -v "${PWD}":/workdir mikefarah/yq /bin/sh -c " \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmalertmanagers_crd.yaml $(YAML_DROP_PREFIX).initContainers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmalertmanagers_crd.yaml $(YAML_DROP_PREFIX).containers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmalerts_crd.yaml $(YAML_DROP_PREFIX).initContainers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmalerts_crd.yaml $(YAML_DROP_PREFIX).containers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmsingles_crd.yaml $(YAML_DROP_PREFIX).initContainers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmsingles_crd.yaml $(YAML_DROP_PREFIX).containers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmagents_crd.yaml $(YAML_DROP_PREFIX).initContainers.items.properties && \
		$(YAML_DROP) deploy/crds/victoriametrics.com_vmagents_crd.yaml $(YAML_DROP_PREFIX).containers.items.properties "


gen: gen-crd fix118

olm:
	$(OPERATOR_BIN) generate csv --operator-name=victoria-metrics-operator \
	                             --csv-version $(CSV_VERSION)\
	                             --apis-dir=pkg/apis/victoriametrics/ \
	                             --make-manifests=false \
	                             --update-crds

olm-verify:
	operator-courier verify deploy/olm-catalog/victoria-metrics-operator/


build-app: fmt
	$(GOBUILD)  -o $(BINARY_NAME) -v $(MAIN_DIR)


doc:
	$(GOBUILD) -o doc-print $(DOC_GEN_DIR)
	./doc-print api \
	         $(APIS_BASE_PATH)/alertmanager_types.go \
	         $(APIS_BASE_PATH)/vmagent_types.go \
	         $(APIS_BASE_PATH)/additional.go \
	         $(APIS_BASE_PATH)/vmalert_types.go \
	         $(APIS_BASE_PATH)/vmsingle_types.go \
	         $(APIS_BASE_PATH)/vmrule_types.go \
	         $(APIS_BASE_PATH)/vmservicescrape_types.go \
	         $(APIS_BASE_PATH)/vmpodscrape_types.go \
	         $(APIS_BASE_PATH)/vmprometheusconvertor_types.go \
	           > docs/api.MD


fmt:
	gofmt -l -w -s ./pkg
	gofmt -l -w -s ./cmd

build: gen build-app

docker: build-app
	docker build -t $(DOCKER_REPO) . -f cmd/manager/Dockerfile

.PHONY:test
test:
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) $(REPO)/pkg/...
	$(GOCMD) tool cover -func coverage.txt  | grep total

.PHONY:e2e-local
e2e-local:
	$(OPERATOR_BIN) test local ./e2e/ --up-local --operator-namespace="default" --verbose

.PHONY:e2e
e2e:
	$(OPERATOR_BIN) test local ./e2e/ --operator-namespace="default" --verbose --image=$(DOCKER_REPO):$(E2E_IMAGE)

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple   --timeout 5m --skip-dirs 'pkg/client'
	golint ./pkg/

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

.PHONY: run
run: build
	WATCH_NAMESPACE="" OPERATOR_NAME=vms ./$(BINARY_NAME)
