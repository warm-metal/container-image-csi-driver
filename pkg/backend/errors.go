package backend

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsCancelled reports whether err represents an operation that was cancelled
// or exceeded its deadline, in either of the two shapes observed in
// production:
//
//  1. A plain (possibly %w-wrapped) context error, e.g. err or one of its
//     wrapped causes is exactly context.Canceled/context.DeadlineExceeded.
//  2. A gRPC status error with code Canceled/DeadlineExceeded, e.g.
//     containerd's own internal gRPC client surfaces a cancelled unpack as
//     "rpc error: code = Canceled desc = context canceled" rather than a bare
//     context.Canceled.
//
// Both checks are required: grpc-go's *status.Error does not implement
// Unwrap() and its Is() method only matches another *status.Error with an
// identical proto, so errors.Is(err, context.Canceled) returns false for a
// gRPC status error even when wrapped with fmt.Errorf("...: %w", err).
// status.Code(err), on the other hand, resolves correctly through arbitrarily
// deep %w-wrapped chains (via errors.As internally) but does not recognize a
// bare, non-gRPC context.Canceled/context.DeadlineExceeded error. See
// KCS-2831.
func IsCancelled(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}
