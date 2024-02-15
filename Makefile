VERSION ?= v1.1.0

IMAGE_BUILDER ?= docker
IMAGE_BUILD_CMD ?= buildx
REGISTRY ?= docker.io/warmmetal
PLATFORM ?= linux/amd64
KIND ?= kind

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

.PHONY: image
image:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) -t $(REGISTRY)/csi-image:$(VERSION) --push .

.PHONY: local
local:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) -t $(REGISTRY)/csi-image:$(VERSION) --load .

# builds and loads csi-image into kind registry
.PHONY: local-kind-load
local-kind-load:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) -t $(REGISTRY)/csi-image:$(VERSION) .
	kind load docker-image $(REGISTRY)/csi-image:$(VERSION) --name $(KIND)

# installs/upgrades csi-driver into kind via helm
.PHONY: local-kind-install
local-kind-install:
	helm upgrade --install container-image-csi-driver charts/warm-metal-csi-driver -n kube-system -f charts/warm-metal-csi-driver/values.yaml --set csiPlugin.image.tag=$(VERSION) --wait --debug

# removes all images which were directly pushed to kind registry
.PHONY: local-kind-flush
local-kind-flush:
	docker exec -e CONTAINERD_NAMESPACE=k8s.io kind-control-plane bash -c "crictl images -o json | jq '.images[] | select((.repoTags == []) or (.repoTags[] | contains(\"docker.io/warmmetal/container-image-csi-driver-test:simple-fs\"))) | .id' -r > '/tmp/TMPFILE';cat /tmp/TMPFILE | awk '{print \"crictl rmi \" \$$1}' | sh;systemctl restart containerd"

.PHONY: test-deps
test-deps:
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:simple-fs -f hack/integration-test-image/container-image-csi-driver-test:simple-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:stat-fs -f hack/integration-test-image/container-image-csi-driver-test:stat-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:check-fs -f hack/integration-test-image/container-image-csi-driver-test:check-fs.dockerfile hack/integration-test-image
	$(IMAGE_BUILDER) $(IMAGE_BUILD_CMD) build --platform=$(PLATFORM) --push -t $(REGISTRY)/container-image-csi-driver-test:write-check -f hack/integration-test-image/container-image-csi-driver-test:write-check.dockerfile hack/integration-test-image

.PHONY: install-util
install-util:
	GOOS=linux CGO_ENABLED="0" go build \
	  -ldflags "-X main.Version=$(VERSION) -X main.Registry=$(REGISTRY)" \
	  -o _output/warm-metal-csi-image-install ./cmd/install