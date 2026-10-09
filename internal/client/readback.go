package client

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// NodePing sometimes answers an update of a check with the check as it was
// before the update: its old modified and old values, although it applied the
// update. A read a few seconds later shows it (finding 41: 1 in 33 full
// updates more than 4 s after the create; 5 in ~180 sent 2 s apart).
// ReadCheckBack reads such a check again until it does.

// defaultReadBackWaits are the waits before each read of ReadCheckBack: 15 s
// in all.
var defaultReadBackWaits = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

// UpdateNotShownError is ReadCheckBack's error when no read showed the update.
type UpdateNotShownError struct {
	ID     string
	Reads  int
	Waited time.Duration
}

func (e *UpdateNotShownError) Error() string {
	return fmt.Sprintf("check %s read as it was before the update %d times over %s", e.ID, e.Reads, e.Waited)
}

// ReadCheckBack reads the check id again after each of the client's
// read-back waits, until shows accepts an answer, and returns that answer. It
// is for a check whose update NodePing answered with the check as it was
// before. If no answer is accepted, the error is an UpdateNotShownError; a
// read that fails ends it with the read's error.
//
// Every read waits for the rate limiter and is retried like any other.
func (c *Client) ReadCheckBack(ctx context.Context, id string, shows func(*Check) bool) (*Check, error) {
	var waited time.Duration
	for i, wait := range c.readBackWaits {
		tflog.Debug(ctx, "Reading a check again: NodePing answered its update with the check as it was before", map[string]interface{}{
			"id":      id,
			"attempt": i + 1,
			"wait":    wait.String(),
		})

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		waited += wait

		check, err := c.GetCheck(ctx, id)
		if err != nil {
			return nil, err
		}
		if shows(check) {
			return check, nil
		}
	}
	return nil, &UpdateNotShownError{ID: id, Reads: len(c.readBackWaits), Waited: waited}
}
