package shares

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

/*
A service with no ownership check cannot mint links.

The worker constructs one this way — sweeping dead rows needs no owner —
and that construction must not be one call away from issuing bearer
credentials to any chart in the database.

Fails closed rather than skipping the check, which is the difference
between "this service cannot create shares" and "this service creates
shares for anybody".
*/
func TestCreatingWithoutAnOwnershipCheckIsRefused(t *testing.T) {
	svc := NewService(nil, nil, nil)

	_, err := svc.Create(context.Background(), uuid.New(), uuid.New(), 0)
	if err == nil {
		t.Fatal("a service with no ownership check created a share link. The worker " +
			"builds one this way for sweeping; it must not be able to mint " +
			"credentials to charts it cannot verify the caller owns")
	}
	if !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("refused with %q; the message should say why, so somebody hitting "+
			"this in a log knows it is a wiring mistake rather than a user error", err)
	}
}
