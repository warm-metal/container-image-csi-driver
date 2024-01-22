VERSION ?= v1.1.0

IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= buildx
REGISTRY ?= docker.io/warmmetal

export IMG = $(REGISTRY)/csi-image:$(VERSION)

# cgo is required to build containers/storage
# For ubuntu, install libbtrfs-dev and libdevmapper-dev before building
.PHONY: build
build:
	go fmt ./...
	go vet ./...
	go build -o _output/csi-image-plugin ./cmd/plugin

.PHONY: sanity
sanity:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build -t local.test/csi-driver-image-test:sanity test/sanity
	kubectl delete --ignore-not-found -f test/sanity/manifest.yaml
	kubectl apply --wait -f test/sanity/manifest.yaml
	kubectl -n cliapp-system wait --for=condition=complete job/csi-driver-image-sanity-test

.PHONY: e2e
e2e:
	cd ./test/e2e && KUBECONFIG=~/.kube/config go run .

.PHONY: integration
integration:
	./test/integration/test-backward-compatability.sh
	./test/integration/test-restart-ds-containerd.sh
	./test/integration/test-restart-ds-crio.sh
	./test/integration/test-restart-runtime-containerd.sh
	./test/integration/test-restart-runtime-crio.sh

.PHONY: image
image:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=linux/amd64 -t $(REGISTRY)/container-image-csi-driver:$(VERSION) --push .

.PHONY: local
local:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build -t $(REGISTRY)/csi-image:$(VERSION) --load .

.PHONY: test-deps
test-deps:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=linux/amd64 -t $(REGISTRY)/container-image-csi-driver-test:simple-fs -f hack/integration-test-image/container-image-csi-driver-test:simple-fs.dockerfile --push hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=linux/amd64 -t $(REGISTRY)/container-image-csi-driver-test:stat-fs -f hack/integration-test-image/container-image-csi-driver-test:stat-fs.dockerfile --push hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=linux/amd64 -t $(REGISTRY)/container-image-csi-driver-test:check-fs -f hack/integration-test-image/container-image-csi-driver-test:check-fs.dockerfile --push hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=linux/amd64 -t $(REGISTRY)/container-image-csi-driver-test:write-check -f hack/integration-test-image/container-image-csi-driver-test:write-check.dockerfile --push hack/integration-test-image

.PHONY: install-util
install-util:
	GOOS=linux CGO_ENABLED="0" go build \
	  -ldflags "-X main.Version=$(VERSION) -X main.Registry=$(REGISTRY)" \
	  -o _output/warm-metal-csi-image-install ./cmd/install
