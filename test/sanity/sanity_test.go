package main

import (
	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
	"testing"
)

func TestCSIImage(t *testing.T) {
	config := sanity.NewTestConfig()
	config.Address = "/csi/csi.sock"
	sanity.Test(t, config)
}
