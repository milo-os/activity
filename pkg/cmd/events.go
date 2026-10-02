package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/kubectl/pkg/cmd/util"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	clientset "go.miloapis.com/activity/pkg/client/clientset/versioned"
	"go.miloapis.com/activity/pkg/cmd/common"
)

// EventsOptions contains the options for querying Kubernetes events
type EventsOptions struct {
	// Filter options
	Namespace      string
	FieldSelector  string
	Type           string
	Reason         string
	RegardingKind  string
	RegardingName  string

	// Common flags
	TimeRange  common.TimeRangeFlags
	Pagination common.PaginationFlags
	Output     common.OutputFlags
	Suggest    common.SuggestFlags

	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewEventsOptions creates a new EventsOptions with default values
func NewEventsOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *EventsOptions {
	return &EventsOptions{
		IOStreams:  ioStreams,
		Factory:    f,
		PrintFlags: genericclioptions.NewPrintFlags(""),
		TimeRange: common.TimeRangeFlags{
			StartTime: "now-24h",
			EndTime:   "now",
		},
		Pagination: common.PaginationFlags{
			Limit: 25,
		},
	}
}

// NewEventsCommand creates the events command
func NewEventsCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewEventsOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "events [flags]",
		Short: "Query Kubernetes events with extended retention",
		Long: `Query Kubernetes events with 60-day retention (vs. 24 hours in native Events API).

This command provides access to historical Kubernetes events stored in ClickHouse,
allowing you to investigate past issues and track event patterns over time.

Time Formats:
  Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)
  Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)

Field Selectors:
  Use standard Kubernetes field selector syntax (e.g., "type=Warning").
  Multiple conditions are comma-separated (all must match).

  Supported fields:
    - type: Normal or Warning
    - reason: Event reason (FailedMount, Pulled, etc.)
    - regarding.kind: Pod, Deployment, etc.
    - regarding.name: Specific object name
    - regarding.namespace: Namespace of regarding object

Examples:
  # Recent events (last 24 hours)
  kubectl activity events

  # Warning events in the last week
  kubectl activity events --start-time "now-7d" --type Warning

  # Events for a specific pod
  kubectl activity events --regarding-name my-pod --regarding-kind Pod

  # Mount failures
  kubectl activity events --reason FailedMount

  # Events in production namespace
  kubectl activity events -n production

  # Use standard field selector
  kubectl activity events --field-selector "regarding.kind=Pod,type=Warning"

  # Discover what reasons exist
  kubectl activity events --suggest reason
`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	// Add flags
	common.AddTimeRangeFlags(cmd, &o.TimeRange, "now-24h")
	common.AddPaginationFlags(cmd, &o.Pagination, 25)
	common.AddOutputFlags(cmd, &o.Output)
	common.AddSuggestFlags(cmd, &o.Suggest)

	// Add event-specific flags
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().StringVar(&o.FieldSelector, "field-selector", "", "Standard Kubernetes field selector")
	cmd.Flags().StringVar(&o.Type, "type", "", "Filter by event type: Normal, Warning")
	cmd.Flags().StringVar(&o.Reason, "reason", "", "Filter by event reason (e.g., FailedMount, Pulled)")
	cmd.Flags().StringVar(&o.RegardingKind, "regarding-kind", "", "Filter by regarding object kind (Pod, Deployment)")
	cmd.Flags().StringVar(&o.RegardingName, "regarding-name", "", "Filter by regarding object name")

	// Add printer flags
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

// Complete fills in missing options
func (o *EventsOptions) Complete(cmd *cobra.Command) error {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.ErrOut == nil {
		o.ErrOut = os.Stderr
	}
	if o.In == nil {
		o.In = os.Stdin
	}
	return nil
}

// Validate checks that required options are set correctly
func (o *EventsOptions) Validate() error {
	if err := o.TimeRange.Validate(); err != nil {
		return err
	}
	if err := o.Pagination.Validate(); err != nil {
		return err
	}
	if err := common.ValidateEventType(o.Type); err != nil {
		return err
	}

	// Validate field selector values to prevent injection
	if o.Reason != "" {
		if _, err := common.EscapeFieldSelectorValue(o.Reason); err != nil {
			return fmt.Errorf("invalid --reason value: %w", err)
		}
	}
	if o.RegardingKind != "" {
		if _, err := common.EscapeFieldSelectorValue(o.RegardingKind); err != nil {
			return fmt.Errorf("invalid --regarding-kind value: %w", err)
		}
	}
	if o.RegardingName != "" {
		if _, err := common.EscapeFieldSelectorValue(o.RegardingName); err != nil {
			return fmt.Errorf("invalid --regarding-name value: %w", err)
		}
	}
	if o.Namespace != "" {
		if _, err := common.EscapeFieldSelectorValue(o.Namespace); err != nil {
			return fmt.Errorf("invalid --namespace value: %w", err)
		}
	}

	return nil
}

// Run executes the events query
func (o *EventsOptions) Run(ctx context.Context) error {
	config, err := o.Factory.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create activity client: %w", err)
	}

	// Handle suggest mode
	if o.Suggest.IsSuggestMode() {
		return common.PrintEventFacets(ctx, client, o.Suggest.Suggest, o.TimeRange.StartTime, o.TimeRange.EndTime, o.Out)
	}

	// Regular query mode
	if o.Pagination.AllPages {
		return o.runAllPages(ctx, client)
	}

	return o.runSinglePage(ctx, client)
}

// buildFieldSelector creates a field selector from shorthand flags
func (o *EventsOptions) buildFieldSelector() string {
	selectors := []string{}

	if o.Type != "" {
		selectors = append(selectors, fmt.Sprintf("type=%s", o.Type))
	}
	if o.Reason != "" {
		selectors = append(selectors, fmt.Sprintf("reason=%s", o.Reason))
	}
	if o.RegardingKind != "" {
		selectors = append(selectors, fmt.Sprintf("regarding.kind=%s", o.RegardingKind))
	}
	if o.RegardingName != "" {
		selectors = append(selectors, fmt.Sprintf("regarding.name=%s", o.RegardingName))
	}

	// Combine with explicit field selector
	if o.FieldSelector != "" {
		selectors = append(selectors, o.FieldSelector)
	}

	return strings.Join(selectors, ",")
}

// runSinglePage executes a single query
func (o *EventsOptions) runSinglePage(ctx context.Context, client *clientset.Clientset) error {
	query := &activityv1alpha1.EventQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "events-",
		},
		Spec: activityv1alpha1.EventQuerySpec{
			StartTime:     o.TimeRange.StartTime,
			EndTime:       o.TimeRange.EndTime,
			Namespace:     o.Namespace,
			FieldSelector: o.buildFieldSelector(),
			Limit:         o.Pagination.Limit,
			Continue:      o.Pagination.ContinueAfter,
		},
	}

	if o.Output.Debug {
		fmt.Fprintf(o.ErrOut, "DEBUG: Query: %+v\n", query.Spec)
	}

	result, err := client.ActivityV1alpha1().EventQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return o.printResults(result)
}

// runAllPages fetches all pages of results
func (o *EventsOptions) runAllPages(ctx context.Context, client *clientset.Clientset) error {
	var allEvents []activityv1alpha1.EventRecord
	continueAfter := ""
	pageNum := 1
	totalCount := 0

	isTableOutput := common.IsDefaultOutputFormat(o.PrintFlags)
	var tablePrinter printers.ResourcePrinter
	if isTableOutput {
		tablePrinter = common.CreateTablePrinter(o.Output.NoHeaders)
	}

	for {
		query := &activityv1alpha1.EventQuery{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "events-",
			},
			Spec: activityv1alpha1.EventQuerySpec{
				StartTime:     o.TimeRange.StartTime,
				EndTime:       o.TimeRange.EndTime,
				Namespace:     o.Namespace,
				FieldSelector: o.buildFieldSelector(),
				Limit:         o.Pagination.Limit,
				Continue:      continueAfter,
			},
		}

		if o.Output.Debug {
			fmt.Fprintf(o.ErrOut, "DEBUG: Fetching page %d\n", pageNum)
		}

		result, err := client.ActivityV1alpha1().EventQueries().Create(ctx, query, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("query failed on page %d: %w", pageNum, err)
		}

		totalCount += len(result.Status.Results)

		if isTableOutput {
			table := kubeEventsToTable(result.Status.Results)
			if err := tablePrinter.PrintObj(table, o.Out); err != nil {
				return err
			}
			// Suppress headers on subsequent pages
			if pageNum == 1 {
				tablePrinter = common.CreateTablePrinter(true)
			}
		} else {
			allEvents = append(allEvents, result.Status.Results...)
		}

		if result.Status.Continue == "" {
			break
		}

		continueAfter = result.Status.Continue
		pageNum++
	}

	if !isTableOutput {
		printer, err := common.CreatePrinter(o.PrintFlags)
		if err != nil {
			return fmt.Errorf("failed to create printer: %w", err)
		}
		if err := printEventRecords(allEvents, printer, o.Out); err != nil {
			return err
		}
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintAllPagesInfo(totalCount)

	return nil
}

// printResults outputs the query results in the specified format
func (o *EventsOptions) printResults(result *activityv1alpha1.EventQuery) error {
	if common.IsDefaultOutputFormat(o.PrintFlags) {
		return o.printTable(result.Status.Results, result.Status.Continue)
	}

	printer, err := common.CreatePrinter(o.PrintFlags)
	if err != nil {
		return fmt.Errorf("failed to create printer: %w", err)
	}

	return printEventRecords(result.Status.Results, printer, o.Out)
}

// printTable prints events as a formatted table
func (o *EventsOptions) printTable(events []activityv1alpha1.EventRecord, continueToken string) error {
	table := kubeEventsToTable(events)
	tablePrinter := common.CreateTablePrinter(o.Output.NoHeaders)

	if err := tablePrinter.PrintObj(table, o.Out); err != nil {
		return err
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintPaginationInfo(continueToken, len(events))

	return nil
}

// kubeEventsToTable converts EventRecords to a Table object
func kubeEventsToTable(events []activityv1alpha1.EventRecord) *metav1.Table {
	table := &metav1.Table{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Table",
			APIVersion: "meta.k8s.io/v1",
		},
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Last Seen", Type: "string", Description: "Last occurrence time"},
			{Name: "Type", Type: "string", Description: "Event type"},
			{Name: "Reason", Type: "string", Description: "Event reason"},
			{Name: "Object", Type: "string", Description: "Regarding object"},
			{Name: "Message", Type: "string", Description: "Event message"},
		},
		Rows: kubeEventsToRows(events),
	}
	return table
}

// kubeEventsToRows converts EventRecords to table rows
func kubeEventsToRows(events []activityv1alpha1.EventRecord) []metav1.TableRow {
	rows := make([]metav1.TableRow, 0, len(events))
	for i := range events {
		ev := &events[i].Event

		lastSeen := ""
		if !ev.EventTime.IsZero() {
			lastSeen = ev.EventTime.UTC().Format(time.RFC3339)
		}

		eventType := ev.Type
		reason := ev.Reason

		object := fmt.Sprintf("%s/%s", ev.Regarding.Kind, ev.Regarding.Name)
		if ev.Regarding.Namespace != "" {
			object = ev.Regarding.Namespace + "/" + object
		}

		message := ev.Note
		// Truncate long messages
		if len(message) > 80 {
			message = message[:77] + "..."
		}

		row := metav1.TableRow{
			Cells: []interface{}{lastSeen, eventType, reason, object, message},
		}
		rows = append(rows, row)
	}
	return rows
}

// printEventRecords prints EventRecords using the configured printer by extracting
// the underlying eventsv1.Event list
func printEventRecords(records []activityv1alpha1.EventRecord, printer printers.ResourcePrinter, out io.Writer) error {
	items := make([]eventsv1.Event, 0, len(records))
	for i := range records {
		items = append(items, records[i].Event)
	}
	eventList := &eventsv1.EventList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EventList",
			APIVersion: "events.k8s.io/v1",
		},
		Items: items,
	}
	return printer.PrintObj(eventList, out)
}
