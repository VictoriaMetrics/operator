cr-docker-build-arch:
	docker build -t $(DOCKER_REPO):config-reloader-$(TAG)-$(GOARCH) \
			--build-arg ARCH=$(GOARCH) \
			--build-arg base_image=$(ALPINE_IMAGE) \
			-f internal/config-reloader/Dockerfile .

cr-package-arch:
	GOOS=linux $(GOBUILD) -o bin/config-reloader-$(GOARCH) github.com/VictoriaMetrics/operator/internal/config-reloader/


cr-build-operator-crosscompile:
	CGO_ENABLED=0 GOARCH=arm $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=arm64 $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=amd64 $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=ppc64le $(MAKE) cr-package-arch
	CGO_ENABLED=0 GOARCH=386 $(MAKE) cr-package-arch

docker-config-reloader-crosscompile: cr-build-operator-crosscompile
	GOARCH=arm $(MAKE) cr-docker-build-arch
	GOARCH=arm64 $(MAKE) cr-docker-build-arch
	GOARCH=amd64 $(MAKE) cr-docker-build-arch
	GOARCH=ppc64le $(MAKE) cr-docker-build-arch
	GOARCH=386 $(MAKE) cr-docker-build-arch


docker-config-reloader-push-crosscompile: docker-config-reloader-crosscompile
	docker push $(DOCKER_REPO):config-reloader-$(TAG)-arm
	docker push $(DOCKER_REPO):config-reloader-$(TAG)-amd64
	docker push $(DOCKER_REPO):config-reloader-$(TAG)-arm64
	docker push $(DOCKER_REPO):config-reloader-$(TAG)-ppc64le
	docker push $(DOCKER_REPO):config-reloader-$(TAG)-386

cr-package-manifest-annotate-goarch:
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest annotate $(DOCKER_REPO):config-reloader-$(TAG) \
				$(DOCKER_REPO):config-reloader-$(TAG)-$(GOARCH) --os linux --arch $(GOARCH)


cr-docker-manifest: docker-config-reloader-push-crosscompile
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest create --amend $(DOCKER_REPO):config-reloader-$(TAG) \
				$(DOCKER_REPO):config-reloader-$(TAG)-amd64 \
				$(DOCKER_REPO):config-reloader-$(TAG)-arm \
				$(DOCKER_REPO):config-reloader-$(TAG)-arm64 \
				$(DOCKER_REPO):config-reloader-$(TAG)-ppc64le \
				$(DOCKER_REPO):config-reloader-$(TAG)-386
	GOARCH=amd64 $(MAKE) cr-package-manifest-annotate-goarch
	GOARCH=arm $(MAKE) cr-package-manifest-annotate-goarch
	GOARCH=arm64 $(MAKE) cr-package-manifest-annotate-goarch
	GOARCH=ppc64le $(MAKE) cr-package-manifest-annotate-goarch
	GOARCH=386 $(MAKE) cr-package-manifest-annotate-goarch


publish-via-docker-config-reloader: cr-docker-manifest
	docker tag $(DOCKER_REPO):config-reloader-$(TAG)-arm64 $(DOCKER_REPO):config-reloader-latest-arm64
	docker tag $(DOCKER_REPO):config-reloader-$(TAG)-arm $(DOCKER_REPO):config-reloader-latest-arm
	docker tag $(DOCKER_REPO):config-reloader-$(TAG)-386 $(DOCKER_REPO):config-reloader-latest-386
	docker tag $(DOCKER_REPO):config-reloader-$(TAG)-ppc64le $(DOCKER_REPO):config-reloader-latest-ppc64le
	docker tag $(DOCKER_REPO):config-reloader-$(TAG)-amd64 $(DOCKER_REPO):config-reloader-latest-amd64
	TAG=latest $(MAKE) cr-docker-manifest
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge $(DOCKER_REPO):config-reloader-$(TAG)
	DOCKER_CLI_EXPERIMENTAL=enabled docker manifest push --purge $(DOCKER_REPO):config-reloader-latest


config-reloader-local:
	GOARCH=amd64 CGO_ENABLED=0 $(MAKE) cr-package-arch
	GOARCH=amd64 CGO_ENABLED=0 $(MAKE) cr-docker-build-arch
