// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package fanout_test

import (
	"context"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/canonical/gomock/gomock"
	"github.com/juju/errors"
	"github.com/juju/tc"

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
	maps.Copy(store.Controllers, controllers)
	maps.Copy(store.Accounts, accounts)
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

func (s *fanoutSuite) TestRunSucceed(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	var callCount atomic.Int64
	setup := func(_ context.Context, controllerName string) (fanout.Session, error) {
		account, err := store.AccountDetails(controllerName)
		c.Assert(err, tc.ErrorIsNil)
		return fanout.Session{Conn: newConn(c), Account: *account}, nil
	}
	worker := func(_ context.Context, session fanout.Session, controllerName string) (string, error) {
		callCount.Add(1)
		return session.Account.User + "@" + controllerName, nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b"})

	// Results are in the order requested.
	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].ControllerName, tc.Equals, "ctrl-a")
	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[0].Data, tc.Equals, "admin@ctrl-a")
	c.Assert(results[1].ControllerName, tc.Equals, "ctrl-b")
	c.Assert(results[1].Err, tc.IsNil)
	c.Assert(results[1].Data, tc.Equals, "bob@ctrl-b")
	c.Assert(callCount.Load(), tc.Equals, int64(2))
}

func (s *fanoutSuite) TestSetupRunsSequentiallyBeforeWorkers(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-a": {ControllerUUID: "uuid-a"},
		"ctrl-b": {ControllerUUID: "uuid-b"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	// inSetup is 1 while a setup call is running; a concurrent second
	// setup call would observe a value other than 0/1 and flag it.
	var inSetup atomic.Int64
	setup := func(_ context.Context, controllerName string) (fanout.Session, error) {
		if inSetup.Add(1) != 1 {
			c.Errorf("setup ran concurrently")
		}
		// Yield to give a concurrent setup (if there were one) a chance
		// to run before decrementing.
		runtime.Gosched()
		inSetup.Add(-1)
		account, err := store.AccountDetails(controllerName)
		c.Assert(err, tc.ErrorIsNil)
		return fanout.Session{Conn: newConn(c), Account: *account}, nil
	}
	worker := func(_ context.Context, session fanout.Session, controllerName string) (string, error) {
		return session.Account.User + "@" + controllerName, nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b"})

	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[1].Err, tc.IsNil)
	c.Assert(results[0].Data, tc.Equals, "admin@ctrl-a")
	c.Assert(results[1].Data, tc.Equals, "bob@ctrl-b")
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

	setup := func(_ context.Context, controllerName string) (fanout.Session, error) {
		if controllerName == "ctrl-b" {
			return fanout.Session{}, errBoom
		}
		account, err := store.AccountDetails(controllerName)
		c.Assert(err, tc.ErrorIsNil)
		return fanout.Session{Conn: newConn(c), Account: *account}, nil
	}
	worker := func(_ context.Context, session fanout.Session, controllerName string) (string, error) {
		return session.Account.User + "@" + controllerName, nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b", "ctrl-c"})
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
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		return fanout.Session{}, errBoom
	}
	worker := func(_ context.Context, _ fanout.Session, _ string) (string, error) {
		c.Errorf("worker should not be called when setup fails")
		return "", nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b"})

	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].Err, tc.ErrorIs, errBoom)
	c.Assert(results[1].Err, tc.ErrorIs, errBoom)
	c.Assert(fanout.AllFailed(results), tc.IsTrue)
}

func (s *fanoutSuite) TestRunEmpty(c *tc.C) {
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		c.Errorf("setup should not be called for empty controller list")
		return fanout.Session{}, nil
	}
	worker := func(_ context.Context, _ fanout.Session, _ string) (string, error) {
		c.Errorf("worker should not be called for empty controller list")
		return "", nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, nil)
	c.Assert(len(results), tc.Equals, 0)
	c.Assert(fanout.AllFailed(results), tc.IsFalse)
}

func (s *fanoutSuite) TestRunAllControllersFails(c *tc.C) {
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		c.Errorf("setup should not be called when AllControllers fails")
		return fanout.Session{}, nil
	}
	worker := func(_ context.Context, _ fanout.Session, _ string) (string, error) {
		return "", nil
	}

	store := &allControllersFailingStore{MemStore: jujuclient.NewMemStore()}
	results := fanout.RunAll[string](context.Background(), store, setup, worker)

	c.Assert(len(results), tc.Equals, 1)
	c.Assert(results[0].ControllerName, tc.Equals, "")
	c.Assert(results[0].Err, tc.ErrorIs, errAllControllers)
}

func (s *fanoutSuite) TestRunAllVisitsSortedControllers(c *tc.C) {
	controllers := map[string]jujuclient.ControllerDetails{
		"ctrl-b": {ControllerUUID: "uuid-b"},
		"ctrl-a": {ControllerUUID: "uuid-a"},
	}
	accounts := map[string]jujuclient.AccountDetails{
		"ctrl-a": {User: "admin"},
		"ctrl-b": {User: "bob"},
	}
	store := newStore(controllers, accounts)

	var mu sync.Mutex
	var setupOrder []string
	setup := func(_ context.Context, controllerName string) (fanout.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		setupOrder = append(setupOrder, controllerName)
		account, err := store.AccountDetails(controllerName)
		c.Assert(err, tc.ErrorIsNil)
		return fanout.Session{Conn: newConn(c), Account: *account}, nil
	}
	worker := func(_ context.Context, session fanout.Session, controllerName string) (string, error) {
		return session.Account.User + "@" + controllerName, nil
	}

	results := fanout.RunAll[string](context.Background(), store, setup, worker)

	c.Assert(len(results), tc.Equals, 2)
	c.Assert(results[0].ControllerName, tc.Equals, "ctrl-a")
	c.Assert(results[1].ControllerName, tc.Equals, "ctrl-b")
	// Setup visits controllers in sorted order.
	c.Assert(setupOrder, tc.DeepEquals, []string{"ctrl-a", "ctrl-b"})
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

	setup := func(_ context.Context, controllerName string) (fanout.Session, error) {
		account, err := store.AccountDetails(controllerName)
		c.Assert(err, tc.ErrorIsNil)
		return fanout.Session{Conn: newConn(c), Account: *account}, nil
	}

	seen := make(map[string]string)
	var mu sync.Mutex
	worker := func(_ context.Context, session fanout.Session, controllerName string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		seen[controllerName] = session.Account.User
		return session.Account.User, nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b"})
	c.Assert(len(results), tc.Equals, 2)
	c.Check(seen["ctrl-a"], tc.Equals, "alice")
	c.Check(seen["ctrl-b"], tc.Equals, "bob")
}

func (s *fanoutSuite) TestRunClosesConnections(c *tc.C) {
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		return fanout.Session{Conn: newConn(c)}, nil
	}
	worker := func(_ context.Context, _ fanout.Session, _ string) (string, error) {
		return "", nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a", "ctrl-b"})
	c.Assert(len(results), tc.Equals, 2)
	// gomock verifies each connection's Close was called exactly once when
	// the per-connection controller finishes.
}

func (s *fanoutSuite) TestRunClosesConnectionWhenWorkerFails(c *tc.C) {
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		return fanout.Session{Conn: newConn(c)}, nil
	}
	worker := func(_ context.Context, _ fanout.Session, _ string) (string, error) {
		return "", errBoom
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a"})
	c.Assert(results[0].Err, tc.ErrorIs, errBoom)
	// gomock verifies Close was called exactly once even though the
	// worker failed.
}

func (s *fanoutSuite) TestRunNilConnNotClosed(c *tc.C) {
	// A nil Conn (e.g. a test-substituted client) must not be closed.
	setup := func(_ context.Context, _ string) (fanout.Session, error) {
		return fanout.Session{Account: jujuclient.AccountDetails{User: "admin"}}, nil
	}
	worker := func(_ context.Context, session fanout.Session, _ string) (string, error) {
		return session.Account.User, nil
	}

	results := fanout.Run[string](context.Background(), setup, worker, []string{"ctrl-a"})
	c.Assert(results[0].Err, tc.IsNil)
	c.Assert(results[0].Data, tc.Equals, "admin")
}

func (s *fanoutSuite) TestAllFailed(c *tc.C) {
	c.Assert(fanout.AllFailed([]fanout.Result[string]{}), tc.IsFalse)
	c.Assert(fanout.AllFailed([]fanout.Result[string]{{Err: errBoom}}), tc.IsTrue)
	c.Assert(fanout.AllFailed([]fanout.Result[string]{{Err: errBoom}, {Data: "x"}}), tc.IsFalse)
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
