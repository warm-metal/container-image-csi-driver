VERSION ?= v2.1.5

IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= buildx
REGISTRY ?= docker.io/warmmetal
PLATFORM ?= linux/amd64,linux/arm64
BUILDER_NAME ?= container-image-csi-builder

export IMG = $(REGISTRY)/container-image-csi-driver:$(VERSION)

# cgo is required to build containers/storage
# For ubuntu, install libbtrfs-dev and libdevmapper-dev before building
.PHONY: build
build:
	go fmt ./...
	go vet ./...
	go build -o _output/container-image-csi-driver ./cmd/plugin

.PHONY: sanity
sanity:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build -t local.test/container-image-csi-driver-test:sanity test/sanity
	kubectl delete --ignore-not-found -f test/sanity/manifest.yaml
	kubectl apply --wait -f test/sanity/manifest.yaml
	kubectl -n cliapp-system wait --for=condition=complete job/container-image-csi-driver-sanity-test

.PHONY: e2e
e2e:
	cd ./test/e2e && KUBECONFIG=~/.kube/config go run .

# to run unit tests
# PHONY signifies the make recipe is not a file
# more info: https://stackoverflow.com/questions/2145590/what-is-the-purpose-of-phony-in-a-makefile
.PHONY: unit-tests
unit-tests:
	# -count=1 forces re-test everytime (instead of caching the results)
	# more info: https://stackoverflow.com/questions/48882691/force-retesting-or-disable-test-caching
	go test -count=1 ./cmd/plugin

.PHONY: integration
integration:
	./test/integration/test-backward-compatability.sh
	./test/integration/test-restart-ds-containerd.sh
	./test/integration/test-restart-ds-crio.sh
	./test/integration/test-restart-runtime-containerd.sh
	./test/integration/test-restart-runtime-crio.sh

# Setup docker buildx builder for multi-arch builds
.PHONY: buildx-setup
buildx-setup:
	@echo "Setting up docker buildx builder: $(BUILDER_NAME)"
	@if ! $(IMAGE_BUILDER) buildx ls | grep -q $(BUILDER_NAME); then \
		echo "Creating new buildx builder..."; \
		$(IMAGE_BUILDER) buildx create \
			--name $(BUILDER_NAME) \
			--driver docker-container \
			--bootstrap \
			--use; \
	else \
		echo "Builder $(BUILDER_NAME) already exists, using it..."; \
		$(IMAGE_BUILDER) buildx use $(BUILDER_NAME); \
	fi
	@$(IMAGE_BUILDER) buildx inspect $(BUILDER_NAME) --bootstrap

# Remove docker buildx builder
.PHONY: buildx-remove
buildx-remove:
	@echo "Removing docker buildx builder: $(BUILDER_NAME)"
	@if $(IMAGE_BUILDER) buildx ls | grep -q $(BUILDER_NAME); then \
		$(IMAGE_BUILDER) buildx rm $(BUILDER_NAME); \
		echo "Builder $(BUILDER_NAME) removed"; \
	else \
		echo "Builder $(BUILDER_NAME) does not exist"; \
	fi

.PHONY: image
image: buildx-setup
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) -t $(REGISTRY)/container-image-csi-driver:$(VERSION) --push .

.PHONY: local
local: buildx-setup
	@echo "Note: --load only supports single platform. Building for native architecture only."
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build -t $(REGISTRY)/container-image-csi-driver:$(VERSION) --load .

.PHONY: test-deps
test-deps: buildx-setup
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:simple-fs -f hack/integration-test-image/container-image-csi-driver-test:simple-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:stat-fs -f hack/integration-test-image/container-image-csi-driver-test:stat-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:check-fs -f hack/integration-test-image/container-image-csi-driver-test:check-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:write-check -f hack/integration-test-image/container-image-csi-driver-test:write-check.dockerfile hack/integration-test-image

.PHONY: install-util
install-util:
	GOOS=linux CGO_ENABLED="0" go build \
	  -ldflags "-X main.Version=$(VERSION) -X main.Registry=$(REGISTRY)" \
	  -o _output/container-image-csi-driver-install ./cmd/install
