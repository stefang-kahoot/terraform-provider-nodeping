package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
// the list of its type does not have it either, and err otherwise. The list
// comes from lists, shared with the other confirmations of the type.
func confirmGone[T any](ctx context.Context, lists *listCache, list func(context.Context) (map[string]T, error), resourceType, id string, err error) error {
	if !mayBeGone(err) {
		return err
	}
	fetch := func(ctx context.Context) (map[string]struct{}, error) {
		all, listErr := list(ctx)
		if listErr != nil {
			return nil, listErr
		}
		ids := make(map[string]struct{}, len(all))
		for listedID := range all {
			ids[listedID] = struct{}{}
		}
		return ids, nil
	}

	// Lists numbered up to answered may have been sent before err was.
	answered := lists.sent()
	ids, n, listErr := lists.get(ctx, 0, fetch)
	if _, listed := ids[id]; listErr == nil && listed && n <= answered {
		// The ID may have been deleted since, so it takes a list sent after
		// err to tell that it is still there.
		ids, _, listErr = lists.get(ctx, answered, fetch)
	}
	if listErr != nil {
		return fmt.Errorf("%w (listing to tell whether the %s is gone failed too: %w)", err, resourceType, listErr)
	}
	if _, ok := ids[id]; ok {
		return err
	}
	return &NotFoundError{ResourceType: resourceType, ResourceID: id}
}

// listCache shares one list of an object type among the confirmations that
// something is gone, so that a refresh after N objects were deleted in the
// web interface lists once, not N times. Its zero value is ready to use.
//
// The danger is a list taken before an object was created: it lacks the
// object, and confirming with it a read of the object that looks gone would
// drop the object from state, so that the next apply creates a duplicate.
// So every create of the type drops the list once it returns (created), and a
// confirmation that starts after that never gets a list sent before it. That
// is enough because a confirmation is only ever about an ID Terraform already
// had when it started: one in state or configuration, which existed before
// this client was configured, or one a create through this client returned,
// which dropped every list sent before it. An object that something else
// (the web interface, another provider configuration) creates after a list
// is in no state this client manages.
//
// The other way round, an ID in a list may have been deleted since, which
// confirmGone settles with a list sent after the answer it confirms.
type listCache struct {
	mu   sync.Mutex
	n    uint64    // lists sent so far
	last *listCall // the latest list still good to use, in flight or answered
}

// listCall is one list request, shared by every confirmation waiting for it.
type listCall struct {
	n    uint64        // which list it is, counting from 1
	done chan struct{} // closed once the fields below are set
	ids  map[string]struct{}
	err  error
	// abandoned means the list failed because the confirmation that sent it
	// gave up, not because of NodePing.
	abandoned bool
}

// sent returns how many lists have been sent so far.
func (l *listCache) sent() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

// created drops the list after a create of its type: it may lack the new
// object.
func (l *listCache) created() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = nil
}

// get returns the IDs in a list numbered above after, and its number. That is
// the latest list if its number is above after, whether answered or still in
// flight, and otherwise a new one sent with fetch.
func (l *listCache) get(ctx context.Context, after uint64, fetch func(context.Context) (map[string]struct{}, error)) (map[string]struct{}, uint64, error) {
	for {
		l.mu.Lock()
		call := l.last
		send := call == nil || call.n <= after
		if send {
			l.n++
			call = &listCall{n: l.n, done: make(chan struct{})}
			l.last = call
		}
		l.mu.Unlock()

		if send {
			l.send(ctx, call, fetch)
		} else {
			select {
			case <-call.done:
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
		if call.err == nil {
			return call.ids, call.n, nil
		}
		if !call.abandoned || ctx.Err() != nil {
			return nil, 0, call.err
		}
		// The confirmation that sent it gave up, and this one has not.
	}
}

// send fetches the list of call and hands it to everyone waiting for it. A
// failed list is not kept.
func (l *listCache) send(ctx context.Context, call *listCall, fetch func(context.Context) (map[string]struct{}, error)) {
	call.ids, call.err = fetch(ctx)
	if call.err != nil {
		call.abandoned = ctx.Err() != nil
		l.mu.Lock()
		if l.last == call {
			l.last = nil
		}
		l.mu.Unlock()
	}
	close(call.done)
}
