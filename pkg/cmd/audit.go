package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/kubectl/pkg/cmd/util"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	clientset "go.miloapis.com/activity/pkg/client/clientset/versioned"
	"go.miloapis.com/activity/pkg/cmd/common"
)

// AuditOptions contains the options for querying audit logs
type AuditOptions struct {
	// Filter options
	Filter    string
	Namespace string
	Resource  string
	Verb      string
	User      string

	// Common flags
	TimeRange  common.TimeRangeFlags
	Pagination common.PaginationFlags
	Output     common.OutputFlags
	Suggest    common.SuggestFlags

	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewAuditOptions creates a new AuditOptions with default values
func NewAuditOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *AuditOptions {
	return &AuditOptions{
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

// NewAuditCommand creates the audit command (replaces old query command)
func NewAuditCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewAuditOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "audit [flags]",
		Short: "Query audit logs from the control plane",
		Long: `Query audit logs from the control plane with time ranges and filters.

This command allows you to search audit logs using time ranges, CEL filters,
and convenient shorthand flags for common patterns.

Time Formats:
  Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)
  Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)

Shorthand Filters:
  --namespace, --resource, --verb, --user flags are combined with AND logic.
  The --filter flag is applied after shorthand filters.

Common Filters:
  verb == 'delete'                                    # All deletions
  objectRef.namespace == 'production'                 # Events in production
  verb in ['create', 'update', 'delete', 'patch']     # Write operations
  responseStatus.code >= 400                          # Failed requests
  user.username.startsWith('system:serviceaccount:')  # Service account activity
  objectRef.resource == 'secrets'                     # Secret access

Examples:
  # Recent activity (last 24 hours)
  kubectl activity audit

  # Deletions in the last week
  kubectl activity audit --start-time "now-7d" --verb delete

  # Production namespace activity
  kubectl activity audit --namespace production

  # Failed operations
  kubectl activity audit --filter "responseStatus.code >= 400"

  # Secret access by a specific user
  kubectl activity audit --resource secrets --user alice@example.com

  # Export to JSON for processing
  kubectl activity audit --start-time "now-30d" --all-pages -o json > audit.json

  # Discover what users have activity
  kubectl activity audit --suggest user.username

  # Custom output format
  kubectl activity audit -o jsonpath='{.items[*].objectRef.name}'
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

	// Add audit-specific shorthand flags
	cmd.Flags().StringVar(&o.Filter, "filter", "", "CEL filter expression to narrow results")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by target namespace")
	cmd.Flags().StringVar(&o.Resource, "resource", "", "Filter by resource type (e.g., secrets, pods)")
	cmd.Flags().StringVar(&o.Verb, "verb", "", "Filter by API verb (create, update, delete, patch, get, list, watch)")
	cmd.Flags().StringVar(&o.User, "user", "", "Filter by username")

	// Add printer flags (handles -o json, -o yaml, etc.)
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

// Complete fills in missing options
func (o *AuditOptions) Complete(cmd *cobra.Command) error {
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
func (o *AuditOptions) Validate() error {
	if err := o.TimeRange.Validate(); err != nil {
		return err
	}
	if err := o.Pagination.Validate(); err != nil {
		return err
	}
	return nil
}

// Run executes the audit query
func (o *AuditOptions) Run(ctx context.Context) error {
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
		return common.PrintAuditLogFacets(ctx, client, o.Suggest.Suggest, o.TimeRange.StartTime, o.TimeRange.EndTime, o.buildFilter(), o.Out)
	}

	// Regular query mode
	if o.Pagination.AllPages {
		return o.runAllPages(ctx, client)
	}

	return o.runSinglePage(ctx, client)
}

// buildFilter creates a CEL filter from shorthand flags and explicit filter
func (o *AuditOptions) buildFilter() string {
	var filters []string

	if o.Namespace != "" {
		filters = append(filters, fmt.Sprintf("objectRef.namespace == '%s'", common.EscapeCELString(o.Namespace)))
	}
	if o.Resource != "" {
		filters = append(filters, fmt.Sprintf("objectRef.resource == '%s'", common.EscapeCELString(o.Resource)))
	}
	if o.Verb != "" {
		filters = append(filters, fmt.Sprintf("verb == '%s'", common.EscapeCELString(o.Verb)))
	}
	if o.User != "" {
		filters = append(filters, fmt.Sprintf("user.username == '%s'", common.EscapeCELString(o.User)))
	}

	// Combine shorthand filters
	combined := strings.Join(filters, " && ")

	// Add explicit filter if provided
	if o.Filter != "" {
		if combined != "" {
			combined = fmt.Sprintf("(%s) && (%s)", combined, o.Filter)
		} else {
			combined = o.Filter
		}
	}

	return combined
}

// runSinglePage executes a single query
func (o *AuditOptions) runSinglePage(ctx context.Context, client *clientset.Clientset) error {
	query := &activityv1alpha1.AuditLogQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "audit-",
		},
		Spec: activityv1alpha1.AuditLogQuerySpec{
			StartTime: o.TimeRange.StartTime,
			EndTime:   o.TimeRange.EndTime,
			Filter:    o.buildFilter(),
			Limit:     o.Pagination.Limit,
			Continue:  o.Pagination.ContinueAfter,
		},
	}

	if o.Output.Debug {
		fmt.Fprintf(o.ErrOut, "DEBUG: Query: %+v\n", query.Spec)
	}

	result, err := client.ActivityV1alpha1().AuditLogQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return o.printResults(result)
}

// runAllPages fetches all pages of results
func (o *AuditOptions) runAllPages(ctx context.Context, client *clientset.Clientset) error {
	var allEvents []auditv1.Event
	continueAfter := ""
	pageNum := 1
	totalCount := 0

	isTableOutput := common.IsDefaultOutputFormat(o.PrintFlags)
	var tablePrinter printers.ResourcePrinter
	if isTableOutput {
		tablePrinter = common.CreateTablePrinter(o.Output.NoHeaders)
	}

	for {
		query := &activityv1alpha1.AuditLogQuery{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "audit-",
			},
			Spec: activityv1alpha1.AuditLogQuerySpec{
				StartTime: o.TimeRange.StartTime,
				EndTime:   o.TimeRange.EndTime,
				Filter:    o.buildFilter(),
				Limit:     o.Pagination.Limit,
				Continue:  continueAfter,
			},
		}

		if o.Output.Debug {
			fmt.Fprintf(o.ErrOut, "DEBUG: Fetching page %d\n", pageNum)
		}

		result, err := client.ActivityV1alpha1().AuditLogQueries().Create(ctx, query, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("query failed on page %d: %w", pageNum, err)
		}

		totalCount += len(result.Status.Results)

		// For table output, print each page as we get it
		if isTableOutput {
			table := eventsToTable(result.Status.Results)
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

	// Print collected results for JSON/YAML
	if !isTableOutput {
		printer, err := common.CreatePrinter(o.PrintFlags)
		if err != nil {
			return fmt.Errorf("failed to create printer: %w", err)
		}
		if err := printEvents(allEvents, printer, o.Out); err != nil {
			return err
		}
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintAllPagesInfo(totalCount)

	return nil
}

// printResults outputs the query results in the specified format
func (o *AuditOptions) printResults(result *activityv1alpha1.AuditLogQuery) error {
	if common.IsDefaultOutputFormat(o.PrintFlags) {
		return o.printTable(result.Status.Results, result.Status.Continue)
	}

	printer, err := common.CreatePrinter(o.PrintFlags)
	if err != nil {
		return fmt.Errorf("failed to create printer: %w", err)
	}

	return printEvents(result.Status.Results, printer, o.Out)
}

// printTable prints events as a formatted table
func (o *AuditOptions) printTable(events []auditv1.Event, continueToken string) error {
	table := eventsToTable(events)
	tablePrinter := common.CreateTablePrinter(o.Output.NoHeaders)

	if err := tablePrinter.PrintObj(table, o.Out); err != nil {
		return err
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintPaginationInfo(continueToken, len(events))

	return nil
}

// eventsToTable converts audit events to a Table object
func eventsToTable(events []auditv1.Event) *metav1.Table {
	table := &metav1.Table{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Table",
			APIVersion: "meta.k8s.io/v1",
		},
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Timestamp", Type: "string", Description: "Time of the event"},
			{Name: "Verb", Type: "string", Description: "Action performed"},
			{Name: "User", Type: "string", Description: "User who performed the action"},
			{Name: "Resource", Type: "string", Description: "Resource affected"},
			{Name: "Status", Type: "string", Description: "HTTP status code"},
		},
		Rows: eventsToRows(events),
	}
	return table
}

// eventsToRows converts audit events to table rows
func eventsToRows(events []auditv1.Event) []metav1.TableRow {
	rows := make([]metav1.TableRow, 0, len(events))
	for i := range events {
		timestamp := "<unknown>"
		if !events[i].StageTimestamp.IsZero() {
			timestamp = events[i].StageTimestamp.UTC().Format(time.RFC3339)
		} else if !events[i].RequestReceivedTimestamp.IsZero() {
			timestamp = events[i].RequestReceivedTimestamp.UTC().Format(time.RFC3339)
		}
		verb := events[i].Verb
		username := events[i].User.Username

		resource := ""
		if events[i].ObjectRef != nil {
			if events[i].ObjectRef.Namespace != "" {
				resource = fmt.Sprintf("%s/%s/%s", events[i].ObjectRef.Namespace, events[i].ObjectRef.Resource, events[i].ObjectRef.Name)
			} else {
				resource = fmt.Sprintf("%s/%s", events[i].ObjectRef.Resource, events[i].ObjectRef.Name)
			}
		}

		status := ""
		if events[i].ResponseStatus != nil {
			status = fmt.Sprintf("%d", events[i].ResponseStatus.Code)
		}

		row := metav1.TableRow{
			Cells: []interface{}{timestamp, verb, username, resource, status},
		}
		rows = append(rows, row)
	}
	return rows
}

// printEvents prints audit events using the configured printer
func printEvents(events []auditv1.Event, printer printers.ResourcePrinter, out io.Writer) error {
	eventList := &auditv1.EventList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EventList",
			APIVersion: "audit.k8s.io/v1",
		},
		Items: events,
	}
	return printer.PrintObj(eventList, out)
}
