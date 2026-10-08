package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// fakeImageService answers ImageFsInfo with err; the other methods are not used by Probe.
type fakeImageService struct {
	cri.ImageServiceClient
	err      error
	deadline bool
}

func (f *fakeImageService) ImageFsInfo(ctx context.Context, _ *cri.ImageFsInfoRequest, _ ...grpc.CallOption) (*cri.ImageFsInfoResponse, error) {
	_, f.deadline = ctx.Deadline()
	if f.err != nil {
		return nil, f.err
	}
	return &cri.ImageFsInfoResponse{}, nil
}

func TestProbeWithoutRuntimeCheck(t *testing.T) {
	resp, err := NewIdentityServer("v").Probe(context.Background(), &csi.ProbeRequest{})
	assert.NoError(t, err)
	assert.True(t, resp.GetReady().GetValue())
}

func TestProbeRuntimeReachable(t *testing.T) {
	svc := &fakeImageService{}
	resp, err := NewNodeIdentityServer("v", svc).Probe(context.Background(), &csi.ProbeRequest{})
	assert.NoError(t, err)
	assert.True(t, resp.GetReady().GetValue())
	assert.True(t, svc.deadline, "the runtime check must be bounded by a timeout")
}

func TestProbeRuntimeUnreachable(t *testing.T) {
	svc := &fakeImageService{err: status.Error(codes.Unavailable,
		`connection error: desc = "transport: Error while dialing: dial unix /run/containerd/containerd.sock: connect: connection refused"`)}
	resp, err := NewNodeIdentityServer("v", svc).Probe(context.Background(), &csi.ProbeRequest{})
	assert.Nil(t, resp)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "connection refused")
}

func TestProbeRuntimeTimeout(t *testing.T) {
	svc := &fakeImageService{err: errors.New("context deadline exceeded")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := NewNodeIdentityServer("v", svc).Probe(ctx, &csi.ProbeRequest{})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
