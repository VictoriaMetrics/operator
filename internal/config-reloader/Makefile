cr-docker-build-arch:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	docker buildx build -t $(DOCKER_REPO):config-reloader-$(TAG)-$(GOARCH) \
			--platform=linux/$(GOARCH) \
			--build-arg BASE_IMAGE=$(firstword $(BASE_IMAGES)) \
			--build-arg APP_NAME=config-reloader \
			-f internal/config-reloader/Dockerfile \
			--load \
			.

cr-package-arch:
	CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} $(GOBUILD) -o bin/config-reloader-$(GOARCH) github.com/VictoriaMetrics/operator/internal/config-reloader/


cr-build-operator-crosscompile:
	$(eval TARGET_PLATFORMS := $(subst $(COMMA), ,$(TARGET_PLATFORM))) \
        $(foreach PLATFORM,$(TARGET_PLATFORMS),GOOS=$(firstword $(subst /, ,$(PLATFORM))) GOARCH=$(lastword $(subst /, ,$(PLATFORM))) $(MAKE) cr-package-arch;)

docker-config-reloader-manifest-build-and-push:
	export DOCKER_CLI_EXPERIMENTAL=enabled ;\
	! ( docker buildx ls | grep config-reloader-builder ) && docker buildx create --use --platform=$(TARGET_PLATFORM) --name config-reloader-builder ;\
	$(foreach ROOT_IMAGE,$(ROOT_IMAGES),\
		$(eval SUFFIX := $(if $(findstring -,$(ROOT_IMAGE)),$(firstword $(subst :, ,$(ROOT_IMAGE))),)) \
		docker buildx build \
			--builder config-reloader-builder \
			-t $(DOCKER_REPO):config-reloader-$(TAG)$(SUFFIX) \
			-t $(DOCKER_REPO):config-reloader-latest$(SUFFIX) \
			--platform=$(TARGET_PLATFORM) \
			--build-arg ROOT_IMAGE=$(subst -,,$(ROOT_IMAGE)) \
			--build-arg APP_NAME=config-reloader \
			--push .;)

publish-via-docker-config-reloader: cr-build-operator-crosscompile docker-config-reloader-manifest-build-and-push

config-reloader-local:
	GOARCH=amd64 $(MAKE) cr-package-arch
	GOARCH=amd64 $(MAKE) cr-docker-build-arch
