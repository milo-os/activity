package auditlog

import (
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.miloapis.com/activity/internal/storage"
)

func TestConvertToStructuredError_StaffNotReadyIs503WithRetryAfter(t *testing.T) {
	r := &QueryStorage{}
	err := r.convertToStructuredError(nil, fmt.Errorf("build: %w", storage.ErrStaffRedactionNotReady))
	status, ok := err.(*apierrors.StatusError)
	if !ok || status.ErrStatus.Code != 503 || status.ErrStatus.Details == nil || status.ErrStatus.Details.RetryAfterSeconds == 0 {
		t.Fatalf("error = %v, want 503 with Retry-After", err)
	}
}
