package httpapi

import (
 "context"
 "net/http"
 "testing"
)

func TestCancelledRequestUsesCancellationStatus(t *testing.T) {
 if got := statusFor(context.Canceled); got != 499 { t.Fatalf("cancelled status = %d, want 499", got) }; if got := statusFor(context.DeadlineExceeded); got != http.StatusGatewayTimeout { t.Fatalf("deadline status = %d", got) }
}
