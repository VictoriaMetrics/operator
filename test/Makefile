##@ E2E testing
# Targets here require docker, a Kind cluster, or e2e test tooling.
# Kept in a separate file so changes to e2e tool versions do not retrigger
# the full CI workflow for ordinary source-code changes.
# This file is included by the root Makefile.

E2E_TESTS_CONCURRENCY ?= $(shell getconf _NPROCESSORS_ONLN)
E2E_TARGET ?= ./test/e2e/...

## Tool Binaries
KIND = $(LOCALBIN)/kind-$(KIND_VERSION)
GINKGO_BIN ?= $(LOCALBIN)/ginkgo-$(GINKGO_VERSION)
CRUST_GATHER_BIN ?= $(LOCALBIN)/crust-gather-$(CRUST_GATHER_VERSION)
MIRRORD_BIN ?= $(LOCALBIN)/mirrord-$(MIRRORD_VERSION)

## Tool Versions
KIND_VERSION ?= v0.32.0
GINKGO_VERSION ?= v2.30.0
CRUST_GATHER_VERSION ?= v0.15.2
MIRRORD_VERSION ?= 3.216.0

MIRRORD_OS=$(if $(filter darwin,$(OS)),mac,linux)
MIRRORD_ARCH=$(if $(filter mac,$(MIRRORD_OS)),universal,$(if $(filter x86_64,$(shell uname -m 2>/dev/null)),x86_64,aarch64))

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: load-kind ginkgo crust-gather mirrord
	env CGO_ENABLED=1 OPERATOR_IMAGE=$(OPERATOR_IMAGE) CONFIG_RELOADER_IMAGE=$(CONFIG_RELOADER_IMAGE) REPORTS_DIR=$(shell pwd) CRUST_GATHER_BIN=$(CRUST_GATHER_BIN) $(MIRRORD_BIN) exec -f ./mirrord.json -- $(GINKGO_BIN) \
		-ldflags="-linkmode=external" \
		--output-interceptor-mode=none \
		-procs=$(E2E_TESTS_CONCURRENCY) \
		-randomize-all \
		-timeout=60m \
		-junit-report=report.xml $(E2E_TARGET)

.PHONY: test-e2e-upgrade  # Run only the e2e upgrade tests against a Kind k8s instance that is spun up.
test-e2e-upgrade: E2E_TARGET=./test/e2e/upgrade/...
test-e2e-upgrade: test-e2e

# builds image and loads it into kind.
ensure-kind-cluster: kind
	if [ "`$(KIND) get clusters`" != "kind" ]; then \
		$(KIND) create cluster --config=./kind.yaml; \
	else \
		$(KUBECTL) cluster-info --context kind-kind; \
	fi

load-kind: docker-build docker-build-config-reloader ensure-kind-cluster
	if [ "$(CONTAINER_TOOL)" != "podman" ]; then \
		$(KIND) load docker-image $(REGISTRY)/$(ORG)/$(REPO):$(TAG); \
		$(KIND) load docker-image $(CONFIG_RELOADER_IMAGE); \
	fi

deploy-kind: OVERLAY=config/base-with-webhook
deploy-kind: load-kind deploy

# deploy-kind-no-build skips docker-build/load — use when image is already loaded (e.g. from test-e2e's load-kind dep)
deploy-kind-no-build: OVERLAY=config/base-with-webhook
deploy-kind-no-build: ensure-kind-cluster deploy

undeploy-kind: OVERLAY=config/base-with-webhook-no-crd
undeploy-kind: ensure-kind-cluster undeploy

.PHONY: allure-report
allure-report:
	@[ -d ./allure-results ] && npx allure awesome --single-file ./allure-results -o ./allure-report || echo "allure-results dir not found, skipping report generation"

.PHONY: ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO_BIN),github.com/onsi/ginkgo/v2/ginkgo,$(GINKGO_VERSION))

.PHONY: kind
kind: $(KIND)
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: crust-gather
crust-gather: $(CRUST_GATHER_BIN)
$(CRUST_GATHER_BIN): $(LOCALBIN)
	$(call download-github-release,$(CRUST_GATHER_BIN),crust-gather/crust-gather,$(CRUST_GATHER_VERSION),kubectl-crust-gather_$(CRUST_GATHER_VERSION)_$(OS)_$(ARCH).tar.gz,kubectl-crust-gather)

.PHONY: mirrord
mirrord: $(MIRRORD_BIN)
$(MIRRORD_BIN): $(LOCALBIN)
	$(call download-github-release,$(MIRRORD_BIN),metalbear-co/mirrord,$(MIRRORD_VERSION),mirrord_$(MIRRORD_OS)_$(MIRRORD_ARCH).zip,mirrord)
