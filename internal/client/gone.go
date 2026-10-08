package client

import (
	"context"
	"errors"
	"fmt"
)

// NodePing never answers 404 for an ID it does not have. Reading a check
// returns 200 {"error": "Error fetching check."}, reading a contact or
// contact group returns 200 {}, and deleting any of them again returns 200
// {"error": "Unable to find ..."}. A failure could look the same, and taking
// one for "gone" would drop a resource from state and plan a duplicate, so
// such an answer means not found only when the ID is missing from the list
// as well.

// errNoID marks a read that returned an object without an ID: NodePing's
// answer for a contact or contact group it does not have.
var errNoID = errors.New("NodePing returned an object without an ID")

// mayBeGone reports whether err is an answer NodePing gives for an ID it does
// not have.
func mayBeGone(err error) bool {
	if errors.Is(err, errNoID) {
		return true
	}
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && (apiErr.IsNotFound() || apiErr.StatusCode < 400)
}

// confirmGone returns a NotFoundError when err may mean the ID is gone and
// list does not have it either, and err otherwise.
func confirmGone[T any](ctx context.Context, list func(context.Context) (map[string]T, error), resourceType, id string, err error) error {
	if !mayBeGone(err) {
		return err
	}
	all, listErr := list(ctx)
	if listErr != nil {
		return fmt.Errorf("%w (listing to tell whether the %s is gone failed too: %w)", err, resourceType, listErr)
	}
	if _, ok := all[id]; ok {
		return err
	}
	return &NotFoundError{ResourceType: resourceType, ResourceID: id}
}
