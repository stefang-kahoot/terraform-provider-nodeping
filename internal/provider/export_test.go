package provider

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

// NewWithReadBackWaits is New with the waits before each read of a check
// whose update NodePing answered with the check as it was before, so that a
// test need not wait as long as NodePing may take to show an update.
func NewWithReadBackWaits(version string, waits ...time.Duration) func() provider.Provider {
	return func() provider.Provider {
		return &NodePingProvider{
			version:       version,
			readBackWaits: waits,
		}
	}
}
