.PHONY: sanity
sanity:
	kubectl dev build -t local.test/csi-driver-image-test:sanity test/sanity
	kubectl delete -f test/sanity/manifest.yaml
	kubectl apply --wait -f test/sanity/manifest.yaml
	kubectl -n cliapp-system wait --for=condition=complete job/csi-driver-image-sanity-test

.PHONY: image
image:
	kubectl dev build -t docker.io/warmmetal/csi-image:v0.1.0