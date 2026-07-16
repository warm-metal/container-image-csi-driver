module e2e

go 1.26.3

// NOTE: k8s.io/kubernetes internal test packages (test/e2e/...) are not
// importable as external Go modules. This module requires a full rewrite
// to use sigs.k8s.io/e2e-framework. The replace pins below keep the
// dependency graph consistent in the meantime.
replace (
	k8s.io/api => k8s.io/api v0.36.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.36.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.36.2
	k8s.io/apiserver => k8s.io/apiserver v0.36.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.36.2
	k8s.io/client-go => k8s.io/client-go v0.36.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.36.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.36.2
	k8s.io/code-generator => k8s.io/code-generator v0.36.2
	k8s.io/component-base => k8s.io/component-base v0.36.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.36.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.36.2
	k8s.io/cri-api => k8s.io/cri-api v0.36.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.36.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.36.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.36.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.36.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.36.2
	k8s.io/kubectl => k8s.io/kubectl v0.36.2
	k8s.io/kubelet => k8s.io/kubelet v0.36.2
	k8s.io/metrics => k8s.io/metrics v0.36.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.36.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.36.2
)

require (
	github.com/onsi/ginkgo v1.16.0
	github.com/onsi/gomega v1.11.0
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/kubernetes v1.20.5
)
