package main

import (
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	"k8s.io/kubernetes/test/e2e/storage/testpatterns"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
)

type driver struct {
}

func (d driver) GetVolume(config *testsuites.PerTestConfig, volumeNumber int) (attributes map[string]string, shared bool, readOnly bool) {
	return map[string]string{"image": "docker.io/warmmetal/csi-image-test:simple-fs"}, true, false
}

func (d driver) GetCSIDriverName(config *testsuites.PerTestConfig) string {
	return "csi-image.warm-metal.tech"
}

func (d driver) GetPersistentVolumeSource(readOnly bool, fsType string, testVolume testsuites.TestVolume) (*v1.PersistentVolumeSource, *v1.VolumeNodeAffinity) {
	return &v1.PersistentVolumeSource{
			CSI: &v1.CSIPersistentVolumeSource{
				Driver:       "csi-image.warm-metal.tech",
				VolumeHandle: "docker.io/warmmetal/csi-image-test:simple-fs",
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
	return &v1.VolumeSource{
		CSI: &v1.CSIVolumeSource{
			Driver: "csi-image.warm-metal.tech",
			VolumeAttributes: map[string]string{
				"image": "docker.io/warmmetal/csi-image-test:simple-fs",
			},
		},
	}
}

type imageVol struct {
}

func (i imageVol) DeleteVolume() {
}

func (d driver) CreateVolume(config *testsuites.PerTestConfig, volumeType testpatterns.TestVolType) testsuites.TestVolume {
	return &imageVol{}
}

func (d driver) GetDriverInfo() *testsuites.DriverInfo {
	return &testsuites.DriverInfo{
		Name: "csi-image.warm-metal.tech",
		Capabilities: map[testsuites.Capability]bool{
			testsuites.CapExec:          true,
			testsuites.CapMultiPODs:     true,
			testsuites.CapPVCDataSource: true,
		},
		SupportedFsType: sets.NewString(""),
	}
}

func (d driver) SkipUnsupportedTest(pattern testpatterns.TestPattern) {
	supported := false
	switch pattern.VolType {
	case "",
		testpatterns.CSIInlineVolume,
		testpatterns.PreprovisionedPV:
		supported = true
	}
	if !supported {
		e2eskipper.Skipf("Driver %q does not support volume type %q - skipping", "csi-image.warm-metal.tech", pattern.VolType)
	}
}

func (d *driver) PrepareTest(f *framework.Framework) (*testsuites.PerTestConfig, func()) {
	return &testsuites.PerTestConfig{
		Driver:    d,
		Prefix:    "csi-image",
		Framework: f,
	}, func() {}
}
