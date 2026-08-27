// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package fanout_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/canonical/gomock/gomock"
	"github.com/juju/errors"
	"github.com/juju/tc"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cmd/modelcmd/fanout"
	"github.com/juju/juju/cmd/modelcmd/mocks"
)

type fanoutSuite struct{}

func TestFanoutSuite(t *testing.T) {
	tc.Run(t, &fanoutSuite{})
}

// newStore returns a MemStore populated with the given controllers and
// per-controller accounts.
func newStore(controllers map[string]jujuclient.ControllerDetails, accounts map[string]jujuclient.AccountDetails) *jujuclient.MemStore {
	store := jujuclient.NewMemStore()
	for name, details := range controllers {
		store.Controllers[name] = details
	}
	for name, account := range accounts {
		store.Accounts[name] = account
	}
	return store
}

// newConn returns a MockConnection whose Close is expected exactly once.
// gomock verifies the Close expectation when the controller returned by
// gomock.NewController finishes.
func newConn(c *tc.C) *mocks.MockConnection {
	ctrl := gomock.NewController(c)
	conn := mocks.NewMockConnection(ctrl)
	conn.EXPECT().Close().Return(nil).Times(1)
	return conn
}

func (s *fanoutSuite) TestRunAllSucceed(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	var callCount int64
	opener := func(_ context.Context, _ string) (api.Connection, error) {
		return newConn(c), nil
	}
	worker := func(_ context.Context, _ api.Connection, controllerName string, account jujuclient.AccountDetails) (string, error) {
		atomic.AddInt64(&callCount, 1)
		return account.User + "@" + controllerName, nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b"})

	// Results are in the order requested.
	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].ControllerName, tc.Equals, "ctrl-a")
	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[0].Data, tc.Equals, "admin@ctrl-a")
	c.Assert(results[1].ControllerName, tc.Equals, "ctrl-b")
	c.Assert(results[1].Err, tc.IsNil)
	c.Assert(results[1].Data, tc.Equals, "bob@ctrl-b")
	c.Assert(atomic.LoadInt64(&callCount), tc.Equals, int64(2))
}

func (s *fanoutSuite) TestRunOneControllerFailsOthersSucceed(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
		"ctrl-c": {ControllerUUID: "uuid-c"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
		"ctrl-c": {User: "carol"},
	}
	store := newStore(controllers, accounts)

	opener := func(_ context.Context, controllerName string) (api.Connection, error) {
		if controllerName == "ctrl-b" {
			return nil, errBoom
		}
		return newConn(c), nil
	}
	worker := func(_ context.Context, _ api.Connection, controllerName string, account jujuclient.AccountDetails) (string, error) {
		return account.User + "@" + controllerName, nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b", "ctrl-c"})
	c.Assert(results[0].ControllerName, tc.Equals, "ctrl-a")
	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[0].Data, tc.Equals, "admin@ctrl-a")
	c.Assert(results[1].ControllerName, tc.Equals, "ctrl-b")
	c.Assert(results[1].Err, tc.ErrorIs, errBoom)
	c.Assert(results[2].ControllerName, tc.Equals, "ctrl-c")
	c.Assert(results[2].Err, tc.IsNil)
	c.Assert(results[2].Data, tc.Equals, "carol@ctrl-c")
}

func (s *fanoutSuite) TestRunAllFail(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	opener := func(_ context.Context, _ string) (api.Connection, error) {
		return nil, errBoom
	}
	worker := func(_ context.Context, _ api.Connection, _ string, _ jujuclient.AccountDetails) (string, error) {
		c.Errorf("worker should not be called when opener fails")
		return "", nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b"})

	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].Err, tc.ErrorIs, errBoom)
	c.Assert(results[1].Err, tc.ErrorIs, errBoom)
}

func (s *fanoutSuite) TestRunEmptyStore(c *tc.C) {
	store := jujuclient.NewMemStore()
	opener := func(_ context.Context, _ string) (api.Connection, error) {
		c.Errorf("opener should not be called for empty store")
		return nil, nil
	}
	worker := func(_ context.Context, _ api.Connection, _ string, _ jujuclient.AccountDetails) (string, error) {
		c.Errorf("worker should not be called for empty store")
		return "", nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, nil)
	c.Assert(len(results), tc.Equals, 0)
}

func (s *fanoutSuite) TestRunAllControllersFails(c *tc.C) {
	opener := func(_ context.Context, _ string) (api.Connection, error) {
		c.Errorf("opener should not be called when AllControllers fails")
		return nil, nil
	}
	worker := func(_ context.Context, _ api.Connection, _ string, _ jujuclient.AccountDetails) (string, error) {
		return "", nil
	}

	store := &allControllersFailingStore{MemStore: jujuclient.NewMemStore()}
	results := fanout.RunAll[string](context.Background(), store, opener, worker)

	c.Assert(len(results), tc.Equals, 1)
	c.Assert(results[0].ControllerName, tc.Equals, "")
	c.Assert(results[0].Err, tc.ErrorIs, errAllControllers)
}

func (s *fanoutSuite) TestRunPerControllerIdentity(c *tc.C) {
	// A user can be a different identity on each controller; the helper
	// must surface the per-controller account, not a single shared one.
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "alice"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	opener := func(_ context.Context, _ string) (api.Connection, error) {
		return newConn(c), nil
	}

	seen := make(map[string]string)
	var mu sync.Mutex
	worker := func(_ context.Context, _ api.Connection, controllerName string, account jujuclient.AccountDetails) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		seen[controllerName] = account.User
		return account.User, nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b"})
	c.Assert(len(results), tc.Equals, 2)
	c.Check(seen["ctrl-a"], tc.Equals, "alice")
	c.Check(seen["ctrl-b"], tc.Equals, "bob")
}

func (s *fanoutSuite) TestRunMissingAccount(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		// ctrl-b has no account; it should fail per-controller, not abort.
	}
	store := newStore(controllers, accounts)

	opener := func(_ context.Context, controllerName string) (api.Connection, error) {
		if controllerName == "ctrl-b" {
			c.Errorf("opener must not be called for controller without account")
		}
		return newConn(c), nil
	}
	worker := func(_ context.Context, _ api.Connection, _ string, account jujuclient.AccountDetails) (string, error) {
		return account.User, nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b"})
	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].ControllerName, tc.Equals, "ctrl-a")
	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[1].ControllerName, tc.Equals, "ctrl-b")
	c.Assert(results[1].Err, tc.NotNil)
}

func (s *fanoutSuite) TestRunClosesConnections(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	opener := func(_ context.Context, _ string) (api.Connection, error) {
		return newConn(c), nil
	}
	worker := func(_ context.Context, _ api.Connection, _ string, _ jujuclient.AccountDetails) (string, error) {
		return "", nil
	}

	results := fanout.Run[string](context.Background(), store, opener, worker, []string{"ctrl-a", "ctrl-b"})
	c.Assert(len(results), tc.Equals, 2)
	// gomock verifies each connection's Close was called exactly once when
	// the per-connection controller finishes.
}

// allControllersFailingStore is a ClientStore whose AllControllers always
// fails, used to exercise the hard-failure path of fanout.Run. It embeds
// *jujuclient.MemStore so it satisfies the full ClientStore interface; only
// AllControllers is overridden to fail.
type allControllersFailingStore struct {
	*jujuclient.MemStore
}

func (s *allControllersFailingStore) AllControllers() (map[string]jujuclient.ControllerDetails, error) {
	return nil, errAllControllers
}

var errBoom = errors.New("boom")
var errAllControllers = errors.New("all controllers failed")
