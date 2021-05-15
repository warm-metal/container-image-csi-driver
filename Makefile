.PHONY: build
build:
	go vet ./...
	go build -o _output/csi-image-plugin ./cmd/plugin

.PHONY: sanity
sanity:
	kubectl dev build -t local.test/csi-driver-image-test:sanity test/sanity
	kubectl delete --ignore-not-found -f test/sanity/manifest.yaml
	kubectl apply --wait -f test/sanity/manifest.yaml
	kubectl -n cliapp-system wait --for=condition=complete job/csi-driver-image-sanity-test

.PHONY: e2e
e2e:
	cp $(shell minikube ssh-key)* test/e2e/
	kubectl dev build -t local.test/csi-driver-image-test:e2e test/e2e
	kubectl delete --ignore-not-found -f test/e2e/manifest.yaml
	kubectl apply --wait -f test/e2e/manifest.yaml
	kubectl -n cliapp-system wait --timeout=30m --for=condition=complete job/csi-driver-image-e2e-test

.PHONY: integration
integration:
	./test/integration/test-in-minikube-docker.sh
	./test/integration/test-in-minikube-containerd.sh
	./test/integration/test-in-minikube-cri-o.sh
	./test/integration/test-backward-compatability.sh
	./test/integration/test-restart-ds-containerd.sh
	./test/integration/test-restart-ds-crio.sh

.PHONY: image
image:
	kubectl dev build -t docker.io/warmmetal/csi-image:v0.5.0 --push

.PHONY: local
local:
	kubectl dev build -t docker.io/warmmetal/csi-image:v0.5.0

.PHONY: test-deps
test-deps:
	kubectl dev build --push -t docker.io/warmmetal/csi-image-test:stat-fs -f csi-image-test:stat-fs.dockerfile hack/integration-test-image
	kubectl dev build --push -t docker.io/warmmetal/csi-image-test:check-fs -f csi-image-test:check-fs.dockerfile hack/integration-test-image
