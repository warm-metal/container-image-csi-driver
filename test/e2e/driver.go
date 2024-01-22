package main

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	"k8s.io/kubernetes/test/e2e/storage/testpatterns"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
)

type driver struct{}

func (d driver) GetVolume(config *testsuites.PerTestConfig, volumeNumber int) (attributes map[string]string, shared bool, readOnly bool) {
	return map[string]string{"image": "docker.io/warmmetal/container-image-csi-driver-test:simple-fs"}, true, false
}

func (d driver) GetCSIDriverName(config *testsuites.PerTestConfig) string {
	return "csi-image.warm-metal.tech"
}

func (d driver) GetPersistentVolumeSource(readOnly bool, fsType string, testVolume testsuites.TestVolume) (*v1.PersistentVolumeSource, *v1.VolumeNodeAffinity) {
	if !readOnly {
		return nil, nil
	}

	return &v1.PersistentVolumeSource{
			CSI: &v1.CSIPersistentVolumeSource{
				Driver:       "csi-image.warm-metal.tech",
				VolumeHandle: "docker.io/warmmetal/container-image-csi-driver-test:simple-fs",
				ReadOnly:     true,
			},
		}, &v1.VolumeNodeAffinity{
			Required: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/os",
								Operator: v1.NodeSelectorOpIn,
								Values:   []string{"linux"},
							},
						},
					},
				},
			},
		}
}

func (d driver) GetVolumeSource(readOnly bool, fsType string, testVolume testsuites.TestVolume) *v1.VolumeSource {
	ro := readOnly
	return &v1.VolumeSource{
		CSI: &v1.CSIVolumeSource{
			Driver:   "csi-image.warm-metal.tech",
			ReadOnly: &ro,
			VolumeAttributes: map[string]string{
				"image": "docker.io/warmmetal/container-image-csi-driver-test:simple-fs",
			},
		},
	}
}

type imageVol struct{}

func (i imageVol) DeleteVolume() {
}

func (d driver) CreateVolume(config *testsuites.PerTestConfig, volumeType testpatterns.TestVolType) testsuites.TestVolume {
	return &imageVol{}
}

func (d driver) GetDriverInfo() *testsuites.DriverInfo {
	return &testsuites.DriverInfo{
		Name: "csi-image.warm-metal.tech",
		Capabilities: map[testsuites.Capability]bool{
			testsuites.CapExec:             true,
			testsuites.CapMultiPODs:        true,
			testsuites.CapPersistence:      true,
			testsuites.CapSingleNodeVolume: true,
			testsuites.CapPVCDataSource:    true,
		},
		SupportedFsType:     sets.NewString(""),
		RequiredAccessModes: []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany},
	}
}

func (d driver) SkipUnsupportedTest(pattern testpatterns.TestPattern) {
	supported := false
	switch pattern.VolType {
	case "",
		testpatterns.CSIInlineVolume,
		testpatterns.PreprovisionedPV,
		testpatterns.GenericEphemeralVolume:
		supported = true
	}

	if pattern.VolMode == v1.PersistentVolumeBlock {
		supported = false
	}

	if !supported {
		e2eskipper.Skipf("Driver %q does not support tests %q-%q-%q - skipping",
			"csi-image.warm-metal.tech", pattern.Name, pattern.VolType, pattern.VolMode)
	}
}

func (d *driver) PrepareTest(f *framework.Framework) (*testsuites.PerTestConfig, func()) {
	return &testsuites.PerTestConfig{
		Driver:    d,
		Prefix:    "csi-image",
		Framework: f,
	}, func() {}
}
