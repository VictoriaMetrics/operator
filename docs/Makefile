UID := "$(shell id -u)"
PLATFORM := $(shell uname -m)
DOCKER_PLATFORM := "linux/$(if $(findstring $(PLATFORM),arm64),arm64,amd64)"

REPODIR := $(dir $(realpath $(lastword $(MAKEFILE_LIST))))
WORKDIR := $(REPODIR)/..

CONTAINER_TOOL ?= docker

# These commands must be run from the repository root


docs-image:
	if [ ! -d $(WORKDIR)/vmdocs ]; then \
		git clone --depth 1 git@github.com:VictoriaMetrics/vmdocs $(WORKDIR)/vmdocs; \
	fi; \
	cd $(WORKDIR)/vmdocs && \
	git checkout main && \
	git pull origin main && \
	cd .. && \
	docker build \
		-t vmdocs-docker-package \
		--build-arg UID=$(UID) \
		--platform $(DOCKER_PLATFORM) \
		vmdocs

docs-debug: docs docs-image
	$(CONTAINER_TOOL) run \
		--rm \
		--name vmdocs \
		-p 1313:1313 \
		-v ./docs:/opt/docs/content/operator vmdocs-docker-package

docs-images-to-webp: docs-image
	$(CONTAINER_TOOL) run \
		--rm \
		--platform $(DOCKER_PLATFORM) \
		--entrypoint /usr/bin/find \
		--name vmdocs \
		-v ./docs:/opt/docs/content/operator vmdocs-docker-package \
			content/operator \
				-regex ".*\.\(png\|jpg\|jpeg\)" \
				-exec sh -c 'cwebp -preset drawing -m 6 -o $$(echo {} | cut -f-1 -d.).webp {} && rm -rf {}' {} \;
