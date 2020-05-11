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
MAIN_DIR=github.com/VictoriaMetrics/operator/cmd/manager/
OPERATOR_BIN=operator-sdk
DOCKER_REPO="quay.io/f41gh7/vm-operator"
TAG="master"

TEST_ARGS=$(GOCMD) test -covermode=atomic -coverprofile=coverage.txt -v

.PHONY: build

all: build

report:
	$(GOCMD) tool cover -html=coverage.txt

gen:
	$(OPERATOR_BIN) generate crds
	$(OPERATOR_BIN) generate k8s

build-app:
	$(GOBUILD)  -o $(BINARY_NAME) -v $(MAIN_DIR)

build: gen build-app

docker: build-app
	docker build -t $(DOCKER_REPO) . -f cmd/manager/Dockerfile

test:
	echo 'mode: atomic' > coverage.txt  && \
	$(TEST_ARGS) ./...
	$(GOCMD) tool cover -func coverage.txt  | grep total

lint:
	golangci-lint run --exclude '(SA1019):' -E typecheck -E gosimple   --timeout 2m

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)

run: build
	WATCH_NAMESPACE="" OPERATOR_NAME=vms ./$(BINARY_NAME)
