// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/client/modelmanager"
	"github.com/juju/juju/api/jujuclient"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/cmd/modelcmd/fanout"
	"github.com/juju/juju/core/life"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/output"
	"github.com/juju/juju/rpc/params"
)

// NewListModelsCommand returns a command to list models.
func NewListModelsCommand() cmd.Command {
	return modelcmd.WrapController(&modelsCommand{})
}

// ModelManagerAPI defines the methods on the model manager API that
// the models command calls.
type ModelManagerAPI interface {
	Close() error
	ListModels(ctx context.Context, user string) ([]base.UserModel, error)
	ListModelSummaries(ctx context.Context, user string, all bool) ([]base.UserModelSummary, error)
	ModelInfo(context.Context, []names.ModelTag) ([]params.ModelInfoResult, error)
}

// ModelsSysAPI defines the methods on the controller manager API that the
// list models command calls.
type ModelsSysAPI interface {
	Close() error
	AllModels(ctx context.Context) ([]base.UserModel, error)
}

// modelsCommand returns the list of all the models the
// current user can access on the current controller.
type modelsCommand struct {
	modelcmd.ControllerCommandBase
	out            cmd.Output
	all            bool
	allControllers bool
	loggedInUser   string
	user           string
	listUUID       bool
	exactTime      bool
	modelAPI       ModelManagerAPI
	sysAPI         ModelsSysAPI

	// controllerAPIs optionally maps controller names to injected
	// clients, for tests. When set, setup uses these instead of opening
	// connections, for both single- and multi-controller runs.
	controllerAPIs map[string]ModelManagerAPI

	runVars modelsRunValues
}

// Info implements Command.Info
func (c *modelsCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:     "models",
		Purpose:  "Lists models a user can access on a controller.",
		Doc:      listModelsDoc,
		Aliases:  []string{"list-models"},
		Examples: listModelsExamples,
		SeeAlso: []string{
			"add-model",
		},
	})
}

// SetFlags implements Command.SetFlags.
func (c *modelsCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	f.StringVar(&c.user, "user", "", "The user to list models for (administrative users only)")
	f.BoolVar(&c.all, "all", false, "Lists all models, regardless of user accessibility (administrative users only)")
	f.BoolVar(&c.allControllers, "all-controllers", false, "List models across all registered controllers")
	f.BoolVar(&c.listUUID, "uuid", false, "Display UUID for models")
	f.BoolVar(&c.exactTime, "exact-time", false, "Use full timestamps")
	c.out.AddFlags(f, "tabular", map[string]cmd.Formatter{
		"yaml":    cmd.FormatYaml,
		"json":    cmd.FormatJson,
		"tabular": c.formatTabular,
	})
}

// Run implements Command.Run
func (c *modelsCommand) Run(ctx *cmd.Context) error {
	controllerName, err := c.ControllerName()
	if err != nil {
		return errors.Trace(err)
	}
	accountDetails, err := c.CurrentAccountDetails()
	if err != nil {
		return err
	}
	c.loggedInUser = accountDetails.User

	if c.user == "" {
		c.user = accountDetails.User
	}
	if !names.IsValidUser(c.user) {
		return errors.NotValidf("user %q", c.user)
	}

	c.runVars = modelsRunValues{
		currentUser:    c.user,
		controllerName: controllerName,
	}
	// TODO(perrito666) 2016-05-02 lp:1558657
	now := time.Now()

	// The single-controller case is a fan-out over one controller;
	// --all-controllers fans out over every registered controller. Both
	// share the same setup, worker and merge logic.
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

	// Setup runs sequentially on this goroutine: opening connections may
	// prompt for credentials and touches unsynchronized CommandBase state,
	// so it must not run concurrently.
	setup := func(ctx context.Context, name string) (fanout.Session, error) {
		account, err := c.ClientStore().AccountDetails(name)
		if err != nil {
			return fanout.Session{}, errors.Trace(err)
		}
		// A test may have injected clients; use them instead of opening
		// connections.
		if _, ok := c.controllerAPIs[name]; ok {
			return fanout.Session{Account: *account}, nil
		}
		if !c.allControllers && c.modelAPI != nil {
			return fanout.Session{Account: *account}, nil
		}
		conn, err := c.CommandBase.NewAPIRoot(ctx, c.ClientStore(), name, "")
		if err != nil {
			return fanout.Session{}, errors.Trace(err)
		}
		return fanout.Session{Conn: conn, Account: *account}, nil
	}

	// user is either the explicit --user value or the empty string, in which
	// case each worker resolves the per-controller account's user.
	user := c.user
	worker := func(ctx context.Context, session fanout.Session, controllerName string) (modelSummaries, error) {
		// A nil Conn means setup substituted a test-injected client.
		var client ModelManagerAPI
		if session.Conn == nil {
			if api, ok := c.controllerAPIs[controllerName]; ok {
				client = api
			} else {
				client = c.modelAPI
			}
		} else {
			client = modelmanager.NewClient(session.Conn)
		}
		return c.getModelSummaries(ctx, client, now, controllerName, user, session.Account)
	}

	results := fanout.Run(ctx, setup, worker, controllerNames)

	// A test-injected client is not owned by the fan-out; close it now
	// that the fan-out is done, matching the pre-fanout defer behavior.
	if !c.allControllers && c.modelAPI != nil {
		_ = c.modelAPI.Close()
	}
	for _, api := range c.controllerAPIs {
		_ = api.Close()
	}

	var allSummaries []ModelSummary
	for _, r := range results {
		if r.Err != nil {
			if c.allControllers {
				fmt.Fprintf(ctx.GetStderr(), "could not list models on controller %q: %v\n", r.ControllerName, r.Err)
				continue
			}
			// Single-controller: propagate the error directly, matching
			// the pre-fanout behavior.
			return errors.Trace(r.Err)
		}
		// Per-model errors are downgraded to log messages, mirroring the
		// single-controller behavior.
		for _, e := range r.Data.warnings {
			ctx.Infof("%s", e)
		}
		allSummaries = append(allSummaries, r.Data.models...)
	}

	// A fan-out where no controller returned anything is a failure, even
	// though individual controller errors were only warned about above.
	if c.allControllers && fanout.AllFailed(results) {
		return errors.New("could not list models on any controller")
	}

	// Single-controller path: Cache models in the client store and mark the
	// current model so the tabular formatter can highlight it. In
	// multi-controller mode there is no single current model, and models
	// from N controllers cannot be cached under one controller's name.
	if !c.allControllers && len(allSummaries) > 0 {
		modelsToStore := map[string]jujuclient.ModelDetails{}
		for _, m := range allSummaries {
			modelsToStore[m.Name] = jujuclient.ModelDetails{ModelUUID: m.UUID, ModelType: m.Type}
		}
		if err := c.ClientStore().SetModels(c.runVars.controllerName, modelsToStore); err != nil {
			return errors.Trace(err)
		}
	}

	// Compute count flags from the collected summaries.
	c.runVars.hasMachinesCount = false
	c.runVars.hasCoresCount = false
	c.runVars.hasUnitsCount = false
	for _, m := range allSummaries {
		if _, ok := m.Counts[string(params.Machines)]; ok {
			c.runVars.hasMachinesCount = true
		}
		if _, ok := m.Counts[string(params.Cores)]; ok {
			c.runVars.hasCoresCount = true
		}
		if _, ok := m.Counts[string(params.Units)]; ok {
			c.runVars.hasUnitsCount = true
		}
	}

	// Sort by controller then model name for deterministic output.
	sort.Slice(allSummaries, func(i, j int) bool {
		if allSummaries[i].ControllerName != allSummaries[j].ControllerName {
			return allSummaries[i].ControllerName < allSummaries[j].ControllerName
		}
		return allSummaries[i].Name < allSummaries[j].Name
	})

	// In single-controller mode, mark the current model.
	modelSummarySet := ModelSummarySet{Models: allSummaries}
	if !c.allControllers {
		modelSummarySet.CurrentModelQualified, modelSummarySet.CurrentModel = c.currentModelName()
	}

	// For yaml/json in multi-controller mode, group by controller.
	if c.allControllers && (c.out.Name() == "yaml" || c.out.Name() == "json") {
		grouped := make(map[string][]ModelSummary)
		for _, m := range allSummaries {
			grouped[m.ControllerName] = append(grouped[m.ControllerName], m)
		}
		return c.out.Write(ctx, grouped)
	}
	if err := c.out.Write(ctx, modelSummarySet); err != nil {
		return err
	}
	if len(allSummaries) == 0 && c.out.Name() == "tabular" {
		fmt.Fprintln(ctx.Stderr, noModelsMessage)
	}
	return nil
}

func (c *modelsCommand) currentModelName() (qualified, name string) {
	current, err := c.ClientStore().CurrentModel(c.runVars.controllerName)
	if err == nil {
		qualified, name = current, current
		if c.user != "" {
			// If current model's qualifier is this user, un-qualify model name.
			name = common.UserModelName(current, c.runVars.currentUser)
		}
	}
	return
}

// modelSummaries is the per-controller result of getModelSummaries: the
// converted summaries plus per-model warnings that the caller logs.
type modelSummaries struct {
	models   []ModelSummary
	warnings []string
}

// getModelSummaries fetches model summaries for the given user from the
// given API and converts them to ModelSummary values scoped to
// controllerName. Per-model errors are returned as warnings for the
// caller to log, mirroring the single-controller behavior.
func (c *modelsCommand) getModelSummaries(
	ctx context.Context,
	api ModelManagerAPI,
	now time.Time,
	controllerName string,
	user string,
	account jujuclient.AccountDetails,
) (modelSummaries, error) {
	listUser := user
	if listUser == "" {
		listUser = account.User
	}
	if !names.IsValidUser(listUser) {
		return modelSummaries{}, errors.NotValidf("user %q", listUser)
	}
	summaries, err := api.ListModelSummaries(ctx, listUser, c.all)
	if err != nil {
		return modelSummaries{}, errors.Trace(err)
	}
	out := modelSummaries{models: make([]ModelSummary, 0, len(summaries))}
	for _, result := range summaries {
		if result.Error != nil {
			out.warnings = append(out.warnings, result.Error.Error())
			continue
		}
		summary, err := c.modelSummaryFromParams(result, now)
		if err != nil {
			out.warnings = append(out.warnings, err.Error())
			continue
		}
		summary.ControllerName = controllerName
		out.models = append(out.models, summary)
	}
	return out, nil
}

// ModelSummarySet contains the set of summaries for models.
type ModelSummarySet struct {
	Models []ModelSummary `yaml:"models" json:"models"`

	// CurrentModel is the name of the current model, qualified for the
	// user for which we're listing models. i.e. for the user admin,
	// and the model admin/foo, this field will contain "foo"; for
	// bob and the same model, the field will contain "admin/foo".
	CurrentModel string `yaml:"current-model,omitempty" json:"current-model,omitempty"`

	// CurrentModelQualified is the fully qualified name for the current
	// model, i.e. having the format $qualifier/$model.
	CurrentModelQualified string `yaml:"-" json:"-"`
}

// ModelSummary contains a summary of some information about a model.
type ModelSummary struct {
	// Name is a fully qualified model name, i.e. having the format $qualifier/$model.
	Name string `json:"name" yaml:"name"`

	// ShortName is un-qualified model name.
	ShortName string          `json:"short-name" yaml:"short-name"`
	Qualifier string          `json:"-" yaml:"-"`
	UUID      string          `json:"model-uuid" yaml:"model-uuid"`
	Type      model.ModelType `json:"model-type" yaml:"model-type"`

	ControllerUUID     string                  `json:"controller-uuid" yaml:"controller-uuid"`
	ControllerName     string                  `json:"controller-name" yaml:"controller-name"`
	IsController       bool                    `json:"is-controller" yaml:"is-controller"`
	Cloud              string                  `json:"cloud" yaml:"cloud"`
	CloudRegion        string                  `json:"region,omitempty" yaml:"region,omitempty"`
	CloudCredential    *common.ModelCredential `json:"credential,omitempty" yaml:"credential,omitempty"`
	ProviderType       string                  `json:"type,omitempty" yaml:"type,omitempty"`
	Life               life.Value              `json:"life" yaml:"life"`
	Status             *common.ModelStatus     `json:"status,omitempty" yaml:"status,omitempty"`
	UserAccess         string                  `yaml:"access" json:"access"`
	UserLastConnection string                  `yaml:"last-connection" json:"last-connection"`

	// Counts is the map of different counts where key is the entity that was counted
	// and value is the number, for e.g. {"machines":10,"cores":3, "units:4}.
	Counts       map[string]int64 `json:"-" yaml:"-"`
	AgentVersion string           `json:"agent-version,omitempty" yaml:"agent-version,omitempty"`
}

func (c *modelsCommand) modelSummaryFromParams(apiSummary base.UserModelSummary, now time.Time) (ModelSummary, error) {
	var statusSince string
	if c.exactTime {
		statusSince = apiSummary.Status.Since.String()
	} else {
		statusSince = common.FriendlyDuration(apiSummary.Status.Since, now)
	}
	summary := ModelSummary{
		ShortName:      apiSummary.Name,
		Name:           jujuclient.QualifyModelName(apiSummary.Qualifier.String(), apiSummary.Name),
		Qualifier:      apiSummary.Qualifier.String(),
		UUID:           apiSummary.UUID,
		Type:           apiSummary.Type,
		ControllerUUID: apiSummary.ControllerUUID,
		IsController:   apiSummary.IsController,
		Life:           apiSummary.Life,
		Cloud:          apiSummary.Cloud,
		CloudRegion:    apiSummary.CloudRegion,
		UserAccess:     apiSummary.ModelUserAccess,
		Status: &common.ModelStatus{
			Current: apiSummary.Status.Status,
			Message: apiSummary.Status.Info,
			Since:   statusSince,
		},
	}
	if apiSummary.AgentVersion != nil {
		summary.AgentVersion = apiSummary.AgentVersion.String()
	}
	if apiSummary.Migration != nil {
		status := summary.Status
		if status == nil {
			status = &common.ModelStatus{}
			summary.Status = status
		}
		status.Migration = apiSummary.Migration.Status
		status.MigrationStart = common.FriendlyDuration(apiSummary.Migration.StartTime, now)
		status.MigrationEnd = common.FriendlyDuration(apiSummary.Migration.EndTime, now)
	}

	if apiSummary.ProviderType != "" {
		summary.ProviderType = apiSummary.ProviderType
	}
	if apiSummary.CloudCredential != "" {
		if !names.IsValidCloudCredential(apiSummary.CloudCredential) {
			return ModelSummary{}, errors.NotValidf("cloud credential ID %q", apiSummary.CloudCredential)
		}
		credTag := names.NewCloudCredentialTag(apiSummary.CloudCredential)
		summary.CloudCredential = &common.ModelCredential{
			Name:  credTag.Name(),
			Owner: credTag.Owner().Id(),
			Cloud: credTag.Cloud().Id(),
		}
	}
	if apiSummary.UserLastConnection != nil {
		if c.exactTime {
			summary.UserLastConnection = apiSummary.UserLastConnection.String()
		} else {
			summary.UserLastConnection = common.UserFriendlyDuration(*apiSummary.UserLastConnection, now)
		}
	} else {
		summary.UserLastConnection = "never connected"
	}
	summary.Counts = map[string]int64{}
	for _, v := range apiSummary.Counts {
		summary.Counts[v.Entity] = v.Count
	}
	return summary, nil
}

// These values are specific to an individual Run() of the model command.
type modelsRunValues struct {
	currentUser      string
	controllerName   string
	hasMachinesCount bool
	hasCoresCount    bool
	hasUnitsCount    bool
}

// ModelSet contains the set of models known to the client,
// and UUID of the current model.
// (anastasiamac 2017-23-11) This is old, pre juju 2.3 implementation.
type ModelSet struct {
	Models []common.ModelInfo `yaml:"models" json:"models"`

	// CurrentModel is the name of the current model, qualified for the
	// user for which we're listing models. i.e. for the user admin,
	// and the model admin/foo, this field will contain "foo"; for
	// bob and the same model, the field will contain "admin/foo".
	CurrentModel string `yaml:"current-model,omitempty" json:"current-model,omitempty"`

	// CurrentModelQualified is the fully qualified name for the current
	// model, i.e. having the format $qualifier/$model.
	CurrentModelQualified string `yaml:"-" json:"-"`
}

// formatTabular takes an interface{} to adhere to the cmd.Formatter interface
func (c *modelsCommand) formatTabular(writer io.Writer, value any) error {
	summariesSet, ok := value.(ModelSummarySet)
	if !ok {
		return errors.Errorf("expected value of type ModelSummarySet, got %T", value)
	}
	if c.allControllers {
		return c.tabularSummariesGroupedByController(writer, summariesSet)
	}
	controllerName, err := c.ControllerName()
	if err != nil {
		return errors.Trace(err)
	}
	return c.tabularSummaries(writer, summariesSet, controllerName)
}

// tabularSummariesGroupedByController renders one section per controller in
// multi-controller mode. Each section has its own header, matching the
// single-controller layout, so the output reads as a concatenation of
// per-controller `juju models` runs.
func (c *modelsCommand) tabularSummariesGroupedByController(writer io.Writer, modelSet ModelSummarySet) error {
	// Group summaries by controller, preserving the already-sorted order.
	groups := make(map[string][]ModelSummary)
	var controllerNames []string
	for _, m := range modelSet.Models {
		if _, ok := groups[m.ControllerName]; !ok {
			controllerNames = append(controllerNames, m.ControllerName)
		}
		groups[m.ControllerName] = append(groups[m.ControllerName], m)
	}
	sort.Strings(controllerNames)

	first := true
	for _, controllerName := range controllerNames {
		if !first {
			fmt.Fprintln(writer)
		}
		first = false
		if err := c.tabularSummaries(writer, ModelSummarySet{Models: groups[controllerName]}, controllerName); err != nil {
			return err
		}
	}
	return nil
}

func (c *modelsCommand) tabularColumns(tw *ansiterm.TabWriter, w output.Wrapper, controllerName string) {
	w.Println("Controller: " + controllerName)
	w.Println()
	w.Print("Model")
	if c.listUUID {
		w.Print("UUID")
	}
	w.Print("Cloud/Region", "Type", "Status")
	printColumnHeader := func(columnName string, columnNumber int) {
		w.Print(columnName)
		offset := 0
		if c.listUUID {
			offset++
		}
		tw.SetColumnAlignRight(columnNumber + offset)
	}

	if c.runVars.hasMachinesCount {
		printColumnHeader("Machines", 4)
	}

	if c.runVars.hasCoresCount {
		printColumnHeader("Cores", 5)
	}

	if c.runVars.hasUnitsCount {
		printColumnHeader("Units", 5)
	}

	w.Println("Access", "Last connection")
}

// tabularSummaries takes model summaries set to adhere to the cmd.Formatter interface
func (c *modelsCommand) tabularSummaries(writer io.Writer, modelSet ModelSummarySet, controllerName string) error {
	tw := output.TabWriter(writer)
	w := output.Wrapper{tw}
	c.tabularColumns(tw, w, controllerName)

	for _, m := range modelSet.Models {
		cloudRegion := strings.Trim(m.Cloud+"/"+m.CloudRegion, "/")
		name := common.UserModelName(m.Name, c.runVars.currentUser)
		if m.Name == modelSet.CurrentModelQualified {
			name += "*"
			w.PrintColor(output.CurrentHighlight, name)
		} else {
			w.Print(name)
		}
		if c.listUUID {
			w.Print(m.UUID)
		}
		status := "-"
		if m.Status != nil && m.Status.Current.String() != "" {
			status = m.Status.Current.String()
		}
		w.Print(cloudRegion, m.ProviderType, status)
		if c.runVars.hasMachinesCount {
			if v, ok := m.Counts[string(params.Machines)]; ok {
				w.Print(v)
			} else {
				w.Print(0)
			}
		}
		if c.runVars.hasCoresCount {
			if v, ok := m.Counts[string(params.Cores)]; ok {
				w.Print(v)
			} else {
				w.Print("-")
			}
		}
		if c.runVars.hasUnitsCount {
			if v, ok := m.Counts[string(params.Units)]; ok {
				w.Print(v)
			} else {
				w.Print("-")
			}
		}
		access := m.UserAccess
		if access == "" {
			access = "-"
		}
		w.Println(access, m.UserLastConnection)
	}
	tw.Flush()
	return nil
}

var listModelsDoc = `
The models listed here are either models you have created yourself, or
models which have been shared with you. Default values for user and
controller are, respectively, the current user and the current controller.
The active model is denoted by an asterisk.
`

const listModelsExamples = `
    juju models
    juju models --user bob
`
