module github.com/warm-metal/csi-driver-image

go 1.16

require (
	github.com/BurntSushi/toml v1.2.0
	github.com/Microsoft/go-winio v0.6.0 // indirect
	github.com/container-storage-interface/spec v1.6.0
	github.com/containerd/cgroups v1.0.4 // indirect
	github.com/containerd/containerd v1.6.8
	github.com/containerd/continuity v0.3.0 // indirect
	github.com/containers/storage v1.43.0
	github.com/klauspost/compress v1.15.11 // indirect
	github.com/mitchellh/go-ps v1.0.0
	github.com/moby/sys/signal v0.7.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc2
	github.com/opencontainers/selinux v1.10.2 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/warm-metal/csi-drivers v0.5.0-alpha.0.0.20210404173852-9ec9cb097dd2
	golang.org/x/net v0.0.0-20221004154528-8021a29435af // indirect
	golang.org/x/sync v0.0.0-20220929204114-8fcdb60fdcc0 // indirect
	golang.org/x/sys v0.0.0-20221010170243-090e33056c14 // indirect
	google.golang.org/genproto v0.0.0-20221010155953-15ba04fc1c0e // indirect
	google.golang.org/grpc v1.50.0
	k8s.io/api v0.25.2
	k8s.io/apimachinery v0.25.2
	k8s.io/client-go v0.25.2
	k8s.io/cri-api v0.25.2
	k8s.io/klog/v2 v2.70.1
	k8s.io/kubernetes v1.25.2
	k8s.io/utils v0.0.0-20220728103510-ee6ede2d64ed
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	k8s.io/api => k8s.io/api v0.25.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.25.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.25.2
	k8s.io/apiserver => k8s.io/apiserver v0.25.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.25.2
	k8s.io/client-go => k8s.io/client-go v0.25.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.25.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.25.2
	k8s.io/code-generator => k8s.io/code-generator v0.25.2
	k8s.io/component-base => k8s.io/component-base v0.25.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.25.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.25.2
	k8s.io/cri-api => k8s.io/cri-api v0.25.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.25.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.25.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.25.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.25.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.25.2
	k8s.io/kubectl => k8s.io/kubectl v0.25.2
	k8s.io/kubelet => k8s.io/kubelet v0.25.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.25.2
	k8s.io/metrics => k8s.io/metrics v0.25.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.25.2
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.25.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.25.2
)
