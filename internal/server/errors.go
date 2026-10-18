package server

import (
	"errors"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// toConnect maps a Kubernetes API error to a Connect error. The message of the API server stays, because
// it tells the user what to fix (for example a validation error).
func toConnect(err error) error {
	if err == nil {
		return nil
	}
	if ce, ok := errors.AsType[*connect.Error](err); ok {
		return ce
	}
	code := connect.CodeInternal
	switch {
	case apierrors.IsNotFound(err):
		code = connect.CodeNotFound
	case apierrors.IsForbidden(err):
		code = connect.CodePermissionDenied
	case apierrors.IsUnauthorized(err):
		code = connect.CodeUnauthenticated
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		code = connect.CodeInvalidArgument
	case apierrors.IsAlreadyExists(err):
		code = connect.CodeAlreadyExists
	case apierrors.IsConflict(err):
		code = connect.CodeAborted
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err), apierrors.IsTooManyRequests(err):
		code = connect.CodeUnavailable
	}
	return connect.NewError(code, err)
}
