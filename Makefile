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

.PHONY: image
image:
	kubectl dev build -t docker.io/warmmetal/csi-image:v0.2.0
	kubectl dev build -t docker.io/warmmetal/csi-image:latest