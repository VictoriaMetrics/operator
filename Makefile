# Go parameters
GOCMD=GO111MODULE=on go
VERSION=$($CI_BUILD_TAG)
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
DOCKER_REPO=quay.io/f41gh7/vm-operator
TAG="master"
E2E_IMAGE ?= latest
CSV_VERSION ?= 0.0.1
QUAY_TOKEN=$(REPO_TOKEN)
TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v

.PHONY: build

all: build

install-golint:
	which golint || GO111MODULE=off go get -u golang.org/x/lint/golint

install-develop-tools: install-golint
	which operator-courier || pip install operator-courier

report:
	$(GOCMD) tool cover -html=coverage.txt

gen:
	$(OPERATOR_BIN) generate crds
	$(OPERATOR_BIN) generate k8s

olm:
	mkdir temp/crds -p || true
	cp deploy/crds/victoriametrics.com_vm* temp/crds/
	$(OPERATOR_BIN) generate csv --operator-name=victoria-metrics-operator \
	                             --csv-version $(CSV_VERSION)\
	                             --apis-dir=pkg/apis/victoriametrics/ \
	                             --crd-dir=temp/crds \
	                             --make-manifests=false
	$(OPERATOR_BIN) generate csv --operator-name=victoria-metrics-operator \
	                             --csv-version $(CSV_VERSION)\
	                             --apis-dir=pkg/apis/victoriametrics/ \
	                             --crd-dir=temp/crds \
	                             --output-dir=temp/victoria-metrics-operator/$(CSV_VERSION) \
	                             --make-manifests=true
	cp temp/victoria-metrics-operator/$(CSV_VERSION)/manifests/victoriametrics* deploy/olm-catalog/victoria-metrics-operator/$(CSV_VERSION)/

olm-publish:
	operator-courier verify deploy/olm-catalog/victoria-metrics-operator/
	operator-courier push deploy/olm-catalog/victoria-metrics-operator/ \
	                      f41gh7 \
	                      victoria-metrics-operator \
	                      $(CSV_VERSION) \
	                      '$(QUAY_TOKEN)'


build-app: fmt
	$(GOBUILD)  -o $(BINARY_NAME) -v $(MAIN_DIR)


doc:
	$(GOBUILD) -o doc-print $(DOC_GEN_DIR)
	./doc-print api \
	          pkg/apis/victoriametrics/v1beta1/alertmanager_types.go \
	          pkg/apis/victoriametrics/v1beta1/vmagent_types.go pkg/apis/victoriametrics/v1beta1/additional.go \
	          pkg/apis/victoriametrics/v1beta1/vmalert_types.go pkg/apis/victoriametrics/v1beta1/vmsingle_types.go \
	          pkg/apis/monitoring/v1/prometheusrule_types.go pkg/apis/monitoring/v1/servicemonitor_types.go \
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
	operator-sdk test local ./e2e/ --up-local --operator-namespace="default" --verbose --image=$(DOCKER_REPO):$(E2E_IMAGE)

.PHONY:e2e
e2e:
	operator-sdk test local ./e2e/ --operator-namespace="default" --verbose --image=$(DOCKER_REPO):$(E2E_IMAGE)

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple   --timeout 5m
	golint ./pkg

.PHONY:clean
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

.PHONY: run
run: build
	WATCH_NAMESPACE="" OPERATOR_NAME=vms ./$(BINARY_NAME)
