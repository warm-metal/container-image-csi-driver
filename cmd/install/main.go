package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

var (
	Registry = "docker.io/warmmetal"
	Version  = "unset"

	showVersion  = pflag.Bool("version", false, "Show the version number.")
	namespace    = pflag.String("namespace", "kube-system", "Specify the namespace to be installed in.")
	daemonSecret = pflag.StringSlice("pull-image-secret-for-daemonset", nil,
		"PullImageSecrets set to the driver to mount private images. It must be created in the same namespace with the daemonset.")
	printDetectedInstead = pflag.Bool("print-detected-instead", false, "Print detected configuration instead of manifests")
	enableCache          = pflag.Bool("enable-daemon-image-credential-cache", true,
		"Set --enable-daemon-image-credential-cache to the driver daemon")
)

func main() {
	pflag.Parse()
	if *showVersion {
		fmt.Fprintln(os.Stderr, Version)
		return
	}

	t := template.Must(template.New("driverDS").Parse(dsTemplate))
	conf := detectKubeletProcess()
	if conf == nil {
		return
	}

	conf.EnableCache = *enableCache
	conf.Image = fmt.Sprintf("%s/container-image-csi-driver:%s", Registry, Version)

	vols := detectImageSvcVolumes(conf.ImageSocketPath)
	if len(vols) == 0 {
		return
	}

	if conf.Runtime == CriO {
		criVols := fetchCriOVolumes(conf.RuntimeSocketPath)
		if len(criVols) == 0 {
			return
		}

		vols = append(vols, criVols...)
	}

	conf.RuntimeVolumes = make([]corev1.Volume, 0, len(vols))
	for _, v := range vols {
		if len(conf.RuntimeVolumes) == 0 {
			conf.RuntimeVolumes = append(conf.RuntimeVolumes, v)
			continue
		}

		occuppied := false
		for i, vol := range conf.RuntimeVolumes {
			if strings.HasPrefix(v.HostPath.Path, vol.HostPath.Path) {
				occuppied = true
				break
			}

			if strings.HasPrefix(vol.HostPath.Path, v.HostPath.Path) {
				occuppied = true
				conf.RuntimeVolumes[i] = v
				break
			}
		}

		if !occuppied {
			conf.RuntimeVolumes = append(conf.RuntimeVolumes, v)
		}
	}

	if *printDetectedInstead {
		fmt.Println(conf)
		return
	}

	mntProp := corev1.MountPropagationBidirectional
	conf.RuntimeVolumeMounts = make([]corev1.VolumeMount, 0, len(vols))
	for i := range conf.RuntimeVolumes {
		vol := &conf.RuntimeVolumes[i]
		if vol.HostPath.Type != nil && *vol.HostPath.Type == corev1.HostPathDirectory {
			conf.RuntimeVolumeMounts = append(conf.RuntimeVolumeMounts, corev1.VolumeMount{
				Name:             vol.Name,
				MountPath:        vol.HostPath.Path,
				MountPropagation: &mntProp,
			})
		} else {
			conf.RuntimeVolumeMounts = append(conf.RuntimeVolumeMounts, corev1.VolumeMount{
				Name:      vol.Name,
				MountPath: vol.HostPath.Path,
			})
		}
	}

	manifest := &bytes.Buffer{}
	if err := t.Execute(manifest, conf); err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	}

	ds := appsv1.DaemonSet{}
	if err := yaml.Unmarshal(manifest.Bytes(), &ds); err != nil {
		fmt.Fprintf(os.Stderr, manifest.String())
		panic(err)
	}

	ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes, conf.RuntimeVolumes...)
	ds.Spec.Template.Spec.Containers[1].VolumeMounts = append(
		ds.Spec.Template.Spec.Containers[1].VolumeMounts, conf.RuntimeVolumeMounts...,
	)
	out, err := yaml.Marshal(&ds)
	if err != nil {
		panic(err)
	}

	saManifests := defaultSAManifests
	roleManifests := defaultRBACRoleManifests
	if len(*daemonSecret) > 0 {
		role := rbacv1.Role{}
		if err := yaml.Unmarshal([]byte(defaultRBACRoleManifests), &role); err != nil {
			panic(err)
		}

		role.Rules = append(role.Rules, rbacv1.PolicyRule{
			Verbs:         []string{"get"},
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: *daemonSecret,
		})

		updated, err := yaml.Marshal(&role)
		if err != nil {
			panic(err)
		}

		roleManifests = string(updated)

		sa := corev1.ServiceAccount{}
		if err := yaml.Unmarshal([]byte(defaultSAManifests), &sa); err != nil {
			panic(err)
		}
		for _, secret := range *daemonSecret {
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
		}

		updated, err = yaml.Marshal(&sa)
		if err != nil {
			panic(err)
		}
		saManifests = string(updated)
	}

	allManifests := staticManifests + saManifests + "\n---\n" + roleManifests + rbacRoleBindingManifests + string(out)
	if *namespace != "kube-system" {
		allManifests = strings.ReplaceAll(allManifests, "kube-system", *namespace)
	}
	fmt.Print(allManifests)
}

type ContainerRuntime string

const (
	Containerd = ContainerRuntime("containerd")
	CriO       = ContainerRuntime("cri-o")
)

type driverConfig struct {
	Image               string
	KubeletRoot         string
	Runtime             ContainerRuntime
	RuntimeSocketPath   string
	ImageSocketPath     string
	RuntimeVolumes      []corev1.Volume
	RuntimeVolumeMounts []corev1.VolumeMount
	EnableCache         bool
}

func (d driverConfig) String() string {
	b := &strings.Builder{}
	b.Grow(16*5 + 128*len(d.RuntimeVolumes))
	fmt.Fprintln(b, `Kubelet Root  : `, d.KubeletRoot)
	fmt.Fprintln(b, `Runtime       : `, d.Runtime)
	fmt.Fprintln(b, `Runtime Socket: `, d.RuntimeSocketPath)
	fmt.Fprintln(b, `Image Socket  : `, d.ImageSocketPath)
	fmt.Fprintln(b, `EnableCache   : `, d.EnableCache)
	fmt.Fprintln(b, `Host Paths    :`)

	for _, v := range d.RuntimeVolumes {
		fmt.Fprintln(b, v.HostPath.Path)
	}

	return b.String()
}

const staticManifests = `---
apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: container-image.csi.k8s.io
spec:
  attachRequired: false
  podInfoOnMount: true
  volumeLifecycleModes:
    - Persistent
    - Ephemeral
---
`

const defaultSAManifests = `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: container-image-csi-driver
  namespace: kube-system
---
`

const defaultRBACRoleManifests = `---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: container-image-csi-driver
  namespace: kube-system
rules:
  - apiGroups:
      - ""
    resourceNames:
      - container-image-csi-driver
    resources:
      - serviceaccounts
    verbs:
      - get
---
`

const rbacRoleBindingManifests = `---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: container-image-csi-driver
  namespace: kube-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: container-image-csi-driver
subjects:
  - kind: ServiceAccount
    name: container-image-csi-driver
    namespace: kube-system
---
`

const dsTemplate = `---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: container-image-csi-driver
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: container-image-csi-driver
  template:
    metadata:
      labels:
        app: container-image-csi-driver
    spec:
      hostNetwork: false
      serviceAccountName: container-image-csi-driver
      containers:
        - name: node-driver-registrar
          image: quay.io/k8scsi/csi-node-driver-registrar:v1.1.0
          imagePullPolicy: IfNotPresent
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "rm -rf /registration/container-image.csi.k8s.io /registration/container-image.csi.k8s.io-reg.sock"]
          args:
            - --csi-address=/csi/csi.sock
            - --kubelet-registration-path={{.KubeletRoot}}/plugins/container-image.csi.k8s.io/csi.sock
          env:
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  apiVersion: v1
                  fieldPath: spec.nodeName
          volumeMounts:
            - mountPath: /csi
              name: socket-dir
            - mountPath: /registration
              name: registration-dir
        - name: plugin
          image: {{.Image}}
          imagePullPolicy: IfNotPresent
          args:
            - "--endpoint=$(CSI_ENDPOINT)"
            - "--node=$(KUBE_NODE_NAME)"
            - "--runtime-addr=$(CRI_ADDR)"
            - "--enable-daemon-image-credential-cache={{.EnableCache}}"
          env:
            - name: CSI_ENDPOINT
              value: unix:///csi/csi.sock
            - name: CRI_ADDR
              value: {{.Runtime}}://{{.RuntimeSocketPath}}
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  apiVersion: v1
                  fieldPath: spec.nodeName
          securityContext:
            privileged: true
          volumeMounts:
            - mountPath: /csi
              name: socket-dir
            - mountPath: {{.KubeletRoot}}/pods
              mountPropagation: Bidirectional
              name: mountpoint-dir
            - mountPath: {{.RuntimeSocketPath}}
              name: runtime-socket
      volumes:
        - hostPath:
            path: {{.KubeletRoot}}/plugins/container-image.csi.k8s.io
            type: DirectoryOrCreate
          name: socket-dir
        - hostPath:
            path: {{.KubeletRoot}}/pods
            type: DirectoryOrCreate
          name: mountpoint-dir
        - hostPath:
            path: {{.KubeletRoot}}/plugins_registry
            type: Directory
          name: registration-dir
        - hostPath:
            path: {{.RuntimeSocketPath}}
            type: Socket
          name: runtime-socket
---`
