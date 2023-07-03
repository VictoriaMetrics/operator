cr-docker-build-arch:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	docker buildx build -t $(DOCKER_REPO):config-reloader-$(TAG)-$(GOARCH) \
			--platform=linux/$(GOARCH) \
			--build-arg base_image=$(ALPINE_IMAGE) \
			-f internal/config-reloader/Dockerfile \
			--load \
			.

cr-package-arch:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/config-reloader-$(GOARCH) github.com/VictoriaMetrics/operator/internal/config-reloader/


cr-build-operator-crosscompile:
	CGO_ENABLED=0 GOARCH=arm $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=arm64 $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=amd64 $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=ppc64le $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=386 $(MAKE) cr-package-arch

docker-config-reloader-manifest-build-and-push:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	! ( docker buildx ls | grep config-reloader-builder ) && docker buildx create --use --platform=$(MANIFEST_BUILD_PLATFORM) --name config-reloader-builder ;\
	docker buildx build \
		--builder config-reloader-builder \
		-t $(DOCKER_REPO):config-reloader-$(TAG) \
		-t $(DOCKER_REPO):config-reloader-latest \
		--platform=$(MANIFEST_BUILD_PLATFORM) \
		--build-arg base_image=$(ALPINE_IMAGE) \
		-f internal/config-reloader/Dockerfile \
		--push \
		.

publish-via-docker-config-reloader: cr-build-operator-crosscompile docker-config-reloader-manifest-build-and-push

config-reloader-local:
	GOARCH=amd64 CGO_ENABLED=0 $(MAKE) cr-package-arch
	GOARCH=amd64 CGO_ENABLED=0 $(MAKE) cr-docker-build-arch
