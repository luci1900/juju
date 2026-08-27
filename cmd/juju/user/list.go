// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for infos.

package user

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/juju/ansiterm"
	"github.com/juju/clock"
	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/client/usermanager"
	"github.com/juju/juju/api/jujuclient"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/cmd/modelcmd/fanout"
	"github.com/juju/juju/core/output"
	"github.com/juju/juju/rpc/params"
)

var usageListUsersSummary = `
Lists Juju users allowed to connect to a controller or model.`[1:]

var usageListUsersDetails = `
When used without a model name argument, users relevant to a controller are printed.
When used with a model name, users relevant to the specified model are printed.

`[1:]

const usageListUsersExamples = `
Print the users relevant to the current controller:

    juju users
    
Print the users relevant to the controller "another":

    juju users -c another

Print the users relevant to the model "mymodel":

    juju users mymodel
`

func NewListCommand() cmd.Command {
	return modelcmd.WrapController(&listCommand{
		infoCommandBase: infoCommandBase{
			clock: clock.WallClock,
		},
	})
}

// listCommand shows all the users in the Juju server.
type listCommand struct {
	infoCommandBase
	modelUserAPI modelUsersAPI

	All            bool
	allControllers bool
	modelName      string
	currentUser    string
}

// ModelUsersAPI defines the methods on the client API that the
// users command calls.
type modelUsersAPI interface {
	Close() error
	ModelUserInfo(ctx context.Context, modelUUID string) ([]params.ModelUserInfo, error)
}

func (c *listCommand) getModelUsersAPI(ctx context.Context) (modelUsersAPI, error) {
	if c.modelUserAPI != nil {
		return c.modelUserAPI, nil
	}
	return c.NewUserManagerAPIClient(ctx)
}

// Info implements Command.Info.
func (c *listCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:     "users",
		Args:     "[model-name]",
		Purpose:  usageListUsersSummary,
		Doc:      usageListUsersDetails,
		Aliases:  []string{"list-users"},
		Examples: usageListUsersExamples,
		SeeAlso: []string{
			"add-user",
			"register",
			"show-user",
			"disable-user",
			"enable-user",
		},
	})
}

// SetFlags implements Command.SetFlags.
func (c *listCommand) SetFlags(f *gnuflag.FlagSet) {
	c.infoCommandBase.SetFlags(f)
	f.BoolVar(&c.All, "all", false, "Include disabled users (on controller only)")
	f.BoolVar(&c.allControllers, "all-controllers", false, "List users across all registered controllers")
	c.out.AddFlags(f, "tabular", map[string]cmd.Formatter{
		"yaml":    cmd.FormatYaml,
		"json":    cmd.FormatJson,
		"tabular": c.formatTabular,
	})
}

// Init implements Command.Init.
func (c *listCommand) Init(args []string) (err error) {
	c.modelName, err = cmd.ZeroOrOneArgs(args)
	if err != nil {
		return err
	}
	return err
}

// Run implements Command.Run.
func (c *listCommand) Run(ctx *cmd.Context) (err error) {
	if c.out.Name() == "tabular" {
		// Only the tabular outputters need to know the current user,
		// but both of them do, so do it in one place.
		accountDetails, err := c.CurrentAccountDetails()
		if err != nil {
			return err
		}
		c.currentUser = names.NewUserTag(accountDetails.User).Id()
	}
	if c.modelName == "" {
		return c.controllerUsers(ctx)
	}
	return c.modelUsers(ctx)
}

func (c *listCommand) modelUsers(ctx *cmd.Context) error {
	client, err := c.getModelUsersAPI(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	uuids, err := c.ModelUUIDs(ctx, []string{c.modelName})
	if err != nil {
		return err
	}
	if len(uuids) == 0 {
		return errors.Errorf("model %q not found", c.modelName)
	}
	result, err := client.ModelUserInfo(ctx, uuids[0])
	if err != nil {
		return err
	}
	if len(result) == 0 {
		ctx.Infof("No users to display.")
		return nil
	}
	return c.out.Write(ctx, common.ModelUserInfoFromParams(result, c.clock.Now()))
}

func (c *listCommand) controllerUsers(ctx *cmd.Context) error {
	controllerName, err := c.ControllerName()
	if err != nil {
		return errors.Trace(err)
	}

	// Decide which controllers to visit. The single-controller case is a
	// fan-out over one controller; --all-controllers fans out over every
	// registered controller. Both share the same worker and merge logic.
	var controllerNames []string
	if c.allControllers {
		all, err := c.ClientStore().AllControllers()
		if err != nil {
			return errors.Trace(err)
		}
		for name := range all {
			controllerNames = append(controllerNames, name)
		}
		sort.Strings(controllerNames)
	} else {
		controllerNames = []string{controllerName}
	}

	store := c.ClientStore()
	opener := func(ctx context.Context, name string) (api.Connection, error) {
		// In the single-controller path the API may have been injected by
		// tests; skip opening a real connection in that case.
		if !c.allControllers && c.api != nil {
			return nil, nil
		}
		return c.CommandBase.NewAPIRoot(ctx, store, name, "")
	}

	disabled := usermanager.IncludeDisabled(c.All)
	worker := func(ctx context.Context, conn api.Connection, controllerName string, account jujuclient.AccountDetails) ([]UserInfo, error) {
		// In the single-controller path the API may have been injected by
		// tests; use it directly. Otherwise build a client from the
		// fan-out connection.
		var client UserInfoAPI
		if !c.allControllers && c.api != nil {
			client = c.api
		} else {
			client = usermanager.NewClient(conn)
			defer client.Close()
		}
		result, err := client.UserInfo(ctx, nil, disabled)
		if err != nil {
			return nil, errors.Trace(err)
		}
		users := c.apiUsersToUserInfoSlice(result)
		for i := range users {
			users[i].ControllerName = controllerName
		}
		return users, nil
	}

	results := fanout.Run(ctx, store, opener, worker, controllerNames)

	// In the single-controller path the API was injected by tests; close it
	// now that the fan-out is done, matching the old defer behavior.
	if !c.allControllers && c.api != nil {
		_ = c.api.Close()
	}

	var allUsers []UserInfo
	for _, r := range results {
		if r.Err != nil {
			if c.allControllers {
				fmt.Fprintf(ctx.GetStderr(), "could not list users on controller %q: %v\n", r.ControllerName, r.Err)
				continue
			}
			// Single-controller: propagate the error directly, matching
			// the pre-fanout behavior.
			return errors.Trace(r.Err)
		}
		allUsers = append(allUsers, r.Data...)
	}

	if len(allUsers) == 0 {
		ctx.Infof("No users to display.")
		return nil
	}
	return c.out.Write(ctx, allUsers)
}

func (c *listCommand) formatTabular(writer io.Writer, value any) error {
	if c.modelName == "" {
		return c.formatControllerUsers(writer, value)
	}
	return c.formatModelUsers(writer, value)
}

func (c *listCommand) isLoggedInUser(username string) bool {
	tag := names.NewUserTag(username)
	return tag.Id() == c.currentUser
}

func (c *listCommand) formatModelUsers(writer io.Writer, value any) error {
	users, ok := value.(map[string]common.ModelUserInfo)
	if !ok {
		return errors.Errorf("expected value of type %T, got %T", users, value)
	}
	modelUsers := set.NewStrings()
	for name := range users {
		modelUsers.Add(name)
	}
	tw := output.TabWriter(writer)
	w := output.Wrapper{TabWriter: tw}
	w.Println("Name", "Display name", "Access", "Last connection")
	for _, name := range modelUsers.SortedValues() {
		user := users[name]

		var highlight *ansiterm.Context
		userName := name
		if c.isLoggedInUser(name) {
			userName += "*"
			highlight = output.CurrentHighlight
		}
		w.PrintColor(highlight, userName)
		w.Println(user.DisplayName, user.Access, user.LastConnection)
	}
	tw.Flush()
	return nil
}

func (c *listCommand) formatControllerUsers(writer io.Writer, value any) error {
	users, valueConverted := value.([]UserInfo)
	if !valueConverted {
		return errors.Errorf("expected value of type %T, got %T", users, value)
	}

	if c.allControllers {
		return c.formatControllerUsersGrouped(writer, users)
	}

	controllerName, err := c.ControllerName()
	if err != nil {
		return errors.Trace(err)
	}
	tw := output.TabWriter(writer)
	w := output.Wrapper{TabWriter: tw}
	w.Println("Controller: " + controllerName)
	w.Println()
	w.Println("Name", "Display name", "Access", "Date created", "Last connection")
	for _, user := range users {
		conn := user.LastConnection
		if user.Disabled {
			conn += " (disabled)"
		}
		var highlight *ansiterm.Context
		userName := user.Username
		if c.isLoggedInUser(user.Username) {
			userName += "*"
			highlight = output.CurrentHighlight
		}
		w.PrintColor(highlight, userName)
		w.Println(user.DisplayName, user.Access, user.DateCreated, conn)
	}
	tw.Flush()
	return nil
}

// formatControllerUsersGrouped renders one section per controller in
// multi-controller mode, matching the layout of the single-controller
// output but grouped by the ControllerName field on each UserInfo.
func (c *listCommand) formatControllerUsersGrouped(writer io.Writer, users []UserInfo) error {
	groups := make(map[string][]UserInfo)
	var controllerNames []string
	for _, u := range users {
		name := u.ControllerName
		if _, ok := groups[name]; !ok {
			controllerNames = append(controllerNames, name)
		}
		groups[name] = append(groups[name], u)
	}
	sort.Strings(controllerNames)

	first := true
	for _, controllerName := range controllerNames {
		if !first {
			fmt.Fprintln(writer)
		}
		first = false
		tw := output.TabWriter(writer)
		w := output.Wrapper{TabWriter: tw}
		w.Println("Controller: " + controllerName)
		w.Println()
		w.Println("Name", "Display name", "Access", "Date created", "Last connection")
		for _, user := range groups[controllerName] {
			conn := user.LastConnection
			if user.Disabled {
				conn += " (disabled)"
			}
			var highlight *ansiterm.Context
			userName := user.Username
			if c.isLoggedInUser(user.Username) {
				userName += "*"
				highlight = output.CurrentHighlight
			}
			w.PrintColor(highlight, userName)
			w.Println(user.DisplayName, user.Access, user.DateCreated, conn)
		}
		tw.Flush()
	}
	return nil
}
