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
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/kubectl/pkg/cmd/util"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	clientset "go.miloapis.com/activity/pkg/client/clientset/versioned"
	"go.miloapis.com/activity/pkg/cmd/common"
)

// FeedOptions contains the options for querying activities
type FeedOptions struct {
	// Filter options
	Filter       string
	Namespace    string
	Actor        string
	Kind         string
	APIGroup     string
	ChangeSource string
	Search       string
	ResourceUID  string

	// Watch mode
	Watch bool

	// Common flags
	TimeRange  common.TimeRangeFlags
	Pagination common.PaginationFlags
	Output     common.OutputFlags
	Suggest    common.SuggestFlags

	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewFeedOptions creates a new FeedOptions with default values
func NewFeedOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *FeedOptions {
	return &FeedOptions{
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

// NewFeedCommand creates the feed command
func NewFeedCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewFeedOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "feed [flags]",
		Short: "Query human-readable activity summaries",
		Long: `Query human-readable activity summaries from the control plane.

Activities are translated from audit logs and events using ActivityPolicy rules,
providing human-friendly descriptions of what changed in your cluster.

Time Formats:
  Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)
  Absolute: "2024-01-01T00:00:00Z" (RFC3339 with timezone)

Output Formats:
  table (default): Structured view with timestamp, actor, source, and summary
  summary: Just the summaries, one per line
  json/yaml: Full activity objects

CEL Filters:
  spec.changeSource       - "human" or "system"
  spec.actor.name         - Actor display name
  spec.actor.type         - "user", "serviceaccount", "controller"
  spec.resource.kind      - Resource kind (Deployment, Pod, etc.)
  spec.resource.namespace - Resource namespace
  spec.resource.apiGroup  - API group
  spec.summary            - Activity summary text

Examples:
  # Recent human activity
  kubectl activity feed --change-source human

  # Activities for a specific actor
  kubectl activity feed --actor alice@example.com

  # Deployment changes
  kubectl activity feed --kind Deployment

  # Search for specific text
  kubectl activity feed --search "created HTTPProxy"

  # Live feed of human changes
  kubectl activity feed --change-source human --watch

  # Production namespace activity
  kubectl activity feed -n production

  # Filter with CEL for complex queries
  kubectl activity feed --filter "spec.resource.kind in ['Deployment', 'StatefulSet']"

  # Discover active users
  kubectl activity feed --suggest spec.actor.name
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

	// Add feed-specific flags
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Filter by resource namespace")
	cmd.Flags().StringVar(&o.Actor, "actor", "", "Filter by actor name")
	cmd.Flags().StringVar(&o.Kind, "kind", "", "Filter by resource kind (Deployment, Pod, etc.)")
	cmd.Flags().StringVar(&o.APIGroup, "api-group", "", "Filter by API group")
	cmd.Flags().StringVar(&o.ChangeSource, "change-source", "", "Filter by change source: human, system")
	cmd.Flags().StringVar(&o.Search, "search", "", "Full-text search in summaries")
	cmd.Flags().StringVar(&o.Filter, "filter", "", "CEL filter expression")
	cmd.Flags().StringVar(&o.ResourceUID, "resource-uid", "", "Get history of specific resource by UID")
	cmd.Flags().BoolVarP(&o.Watch, "watch", "w", false, "Watch for new activities")

	// Add printer flags
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

// Complete fills in missing options
func (o *FeedOptions) Complete(cmd *cobra.Command) error {
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
func (o *FeedOptions) Validate() error {
	if o.Watch {
		// Watch mode doesn't use time range
		if o.Filter != "" {
			return fmt.Errorf("--filter is not supported in watch mode; use field selector flags (--namespace, --actor, --kind, --change-source) for server-side filtering in watch mode")
		}
		return nil
	}

	if o.ChangeSource != "" && o.ChangeSource != "human" && o.ChangeSource != "system" {
		return fmt.Errorf("invalid --change-source value %q: must be \"human\" or \"system\"", o.ChangeSource)
	}

	if err := o.TimeRange.Validate(); err != nil {
		return err
	}
	if err := o.Pagination.Validate(); err != nil {
		return err
	}
	return nil
}

// Run executes the feed query
func (o *FeedOptions) Run(ctx context.Context) error {
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
		return common.PrintActivityFacets(ctx, client, o.Suggest.Suggest, o.TimeRange.StartTime, o.TimeRange.EndTime, o.buildFilter(), o.Out)
	}

	// Handle watch mode
	if o.Watch {
		return o.runWatch(ctx, client)
	}

	// Regular query mode
	if o.Pagination.AllPages {
		return o.runAllPages(ctx, client)
	}

	return o.runSinglePage(ctx, client)
}

// buildFilter creates a CEL filter from shorthand flags and explicit filter
func (o *FeedOptions) buildFilter() string {
	var filters []string

	if o.Namespace != "" {
		filters = append(filters, fmt.Sprintf("spec.resource.namespace == '%s'", common.EscapeCELString(o.Namespace)))
	}
	if o.Actor != "" {
		filters = append(filters, fmt.Sprintf("spec.actor.name == '%s'", common.EscapeCELString(o.Actor)))
	}
	if o.Kind != "" {
		filters = append(filters, fmt.Sprintf("spec.resource.kind == '%s'", common.EscapeCELString(o.Kind)))
	}
	if o.APIGroup != "" {
		filters = append(filters, fmt.Sprintf("spec.resource.apiGroup == '%s'", common.EscapeCELString(o.APIGroup)))
	}
	if o.ChangeSource != "" {
		filters = append(filters, fmt.Sprintf("spec.changeSource == '%s'", common.EscapeCELString(o.ChangeSource)))
	}
	if o.ResourceUID != "" {
		filters = append(filters, fmt.Sprintf("spec.resource.uid == '%s'", common.EscapeCELString(o.ResourceUID)))
	}

	combined := strings.Join(filters, " && ")

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
func (o *FeedOptions) runSinglePage(ctx context.Context, client *clientset.Clientset) error {
	query := &activityv1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "feed-",
		},
		Spec: activityv1alpha1.ActivityQuerySpec{
			StartTime: o.TimeRange.StartTime,
			EndTime:   o.TimeRange.EndTime,
			Filter:    o.buildFilter(),
			Search:    o.Search,
			Limit:     o.Pagination.Limit,
			Continue:  o.Pagination.ContinueAfter,
		},
	}

	if o.Output.Debug {
		fmt.Fprintf(o.ErrOut, "DEBUG: Query: %+v\n", query.Spec)
	}

	result, err := client.ActivityV1alpha1().ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return o.printResults(result)
}

// runAllPages fetches all pages of results
func (o *FeedOptions) runAllPages(ctx context.Context, client *clientset.Clientset) error {
	var allActivities []activityv1alpha1.Activity
	continueAfter := ""
	pageNum := 1
	totalCount := 0

	isTableOutput := common.IsDefaultOutputFormat(o.PrintFlags)
	var tablePrinter printers.ResourcePrinter
	if isTableOutput {
		tablePrinter = common.CreateTablePrinter(o.Output.NoHeaders)
	}

	for {
		query := &activityv1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "feed-",
			},
			Spec: activityv1alpha1.ActivityQuerySpec{
				StartTime: o.TimeRange.StartTime,
				EndTime:   o.TimeRange.EndTime,
				Filter:    o.buildFilter(),
				Search:    o.Search,
				Limit:     o.Pagination.Limit,
				Continue:  continueAfter,
			},
		}

		if o.Output.Debug {
			fmt.Fprintf(o.ErrOut, "DEBUG: Fetching page %d\n", pageNum)
		}

		result, err := client.ActivityV1alpha1().ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("query failed on page %d: %w", pageNum, err)
		}

		totalCount += len(result.Status.Results)

		if isTableOutput {
			table := activitiesToTable(result.Status.Results)
			if err := tablePrinter.PrintObj(table, o.Out); err != nil {
				return err
			}
			// Suppress headers on subsequent pages
			if pageNum == 1 {
				tablePrinter = common.CreateTablePrinter(true)
			}
		} else {
			allActivities = append(allActivities, result.Status.Results...)
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
		if err := printActivities(allActivities, printer, o.Out); err != nil {
			return err
		}
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintAllPagesInfo(totalCount)

	return nil
}

// runWatch implements watch mode for live activity streaming
func (o *FeedOptions) runWatch(ctx context.Context, client *clientset.Clientset) error {
	fmt.Fprintf(o.ErrOut, "Watching for activities... (press Ctrl+C to stop)\n\n")

	// Build list options with filters
	listOpts := metav1.ListOptions{}

	// Apply field selectors based on filter flags
	var fieldSelectors []string
	if o.Namespace != "" {
		if _, err := common.EscapeFieldSelectorValue(o.Namespace); err != nil {
			return fmt.Errorf("invalid --namespace value for watch: %w", err)
		}
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.resource.namespace=%s", o.Namespace))
	}
	if o.Actor != "" {
		if _, err := common.EscapeFieldSelectorValue(o.Actor); err != nil {
			return fmt.Errorf("invalid --actor value for watch: %w", err)
		}
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.actor.name=%s", o.Actor))
	}
	if o.Kind != "" {
		if _, err := common.EscapeFieldSelectorValue(o.Kind); err != nil {
			return fmt.Errorf("invalid --kind value for watch: %w", err)
		}
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.resource.kind=%s", o.Kind))
	}
	if o.ChangeSource != "" {
		if _, err := common.EscapeFieldSelectorValue(o.ChangeSource); err != nil {
			return fmt.Errorf("invalid --change-source value for watch: %w", err)
		}
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.changeSource=%s", o.ChangeSource))
	}
	if len(fieldSelectors) > 0 {
		listOpts.FieldSelector = strings.Join(fieldSelectors, ",")
	}

	// Start the watch
	watcher, err := client.ActivityV1alpha1().Activities("").Watch(ctx, listOpts)
	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	defer watcher.Stop()

	// Process watch events
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(o.ErrOut, "\nWatch stopped.\n")
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Channel closed, watch ended
				fmt.Fprintf(o.ErrOut, "\nWatch connection closed.\n")
				return nil
			}

			// Only handle Added events (activities are read-only, so we only get additions)
			activity, ok := event.Object.(*activityv1alpha1.Activity)
			if !ok {
				continue
			}

			// Apply client-side filters that can't be done via field selectors
			if !o.matchesClientSideFilters(activity) {
				continue
			}

			// Print the activity in watch format: [timestamp] summary
			timestamp := activity.CreationTimestamp.Format("15:04:05")
			_, _ = fmt.Fprintf(o.Out, "[%s] %s\n", timestamp, activity.Spec.Summary)
		}
	}
}

// matchesClientSideFilters checks if an activity matches client-side filters
func (o *FeedOptions) matchesClientSideFilters(activity *activityv1alpha1.Activity) bool {
	// API group filter
	if o.APIGroup != "" && activity.Spec.Resource.APIGroup != o.APIGroup {
		return false
	}

	// Resource UID filter
	if o.ResourceUID != "" && activity.Spec.Resource.UID != o.ResourceUID {
		return false
	}

	// Full-text search in summary
	if o.Search != "" && !strings.Contains(strings.ToLower(activity.Spec.Summary), strings.ToLower(o.Search)) {
		return false
	}

	// CEL filter (if provided) would need evaluation here
	// For now, we'll skip CEL filter support in watch mode to keep it simple
	// The server-side watch implementation handles most filtering needs

	return true
}

// printResults outputs the query results in the specified format
func (o *FeedOptions) printResults(result *activityv1alpha1.ActivityQuery) error {
	// "summary" is a custom format not known to the Kubernetes printer framework, so
	// it must be handled before calling CreatePrinter — which would reject unknown formats.
	if o.PrintFlags.OutputFormat != nil && *o.PrintFlags.OutputFormat == "summary" {
		return o.printSummary(result.Status.Results)
	}

	if common.IsDefaultOutputFormat(o.PrintFlags) {
		return o.printTable(result.Status.Results, result.Status.Continue)
	}

	printer, err := common.CreatePrinter(o.PrintFlags)
	if err != nil {
		return fmt.Errorf("failed to create printer: %w", err)
	}

	return printActivities(result.Status.Results, printer, o.Out)
}

// printTable prints activities as a formatted table
func (o *FeedOptions) printTable(activities []activityv1alpha1.Activity, continueToken string) error {
	table := activitiesToTable(activities)
	tablePrinter := common.CreateTablePrinter(o.Output.NoHeaders)

	if err := tablePrinter.PrintObj(table, o.Out); err != nil {
		return err
	}

	tp := common.NewTablePrinter(o.PrintFlags, o.IOStreams, o.Output.NoHeaders)
	tp.PrintPaginationInfo(continueToken, len(activities))

	return nil
}

// printSummary prints just the activity summaries, one per line
func (o *FeedOptions) printSummary(activities []activityv1alpha1.Activity) error {
	for _, activity := range activities {
		if _, err := fmt.Fprintln(o.Out, activity.Spec.Summary); err != nil {
			return fmt.Errorf("failed to print summary: %w", err)
		}
	}
	return nil
}

// activitiesToTable converts activities to a Table object
func activitiesToTable(activities []activityv1alpha1.Activity) *metav1.Table {
	table := &metav1.Table{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Table",
			APIVersion: "meta.k8s.io/v1",
		},
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Timestamp", Type: "string", Description: "Time of activity"},
			{Name: "Actor", Type: "string", Description: "Who performed the action"},
			{Name: "Source", Type: "string", Description: "Change source"},
			{Name: "Summary", Type: "string", Description: "Activity summary"},
		},
		Rows: activitiesToRows(activities),
	}
	return table
}

// activitiesToRows converts activities to table rows
func activitiesToRows(activities []activityv1alpha1.Activity) []metav1.TableRow {
	rows := make([]metav1.TableRow, 0, len(activities))
	for i := range activities {
		timestamp := activities[i].CreationTimestamp.UTC().Format(time.RFC3339)
		actor := activities[i].Spec.Actor.Name
		source := activities[i].Spec.ChangeSource
		summary := activities[i].Spec.Summary

		// Truncate long summaries for table display
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}

		row := metav1.TableRow{
			Cells: []interface{}{timestamp, actor, source, summary},
		}
		rows = append(rows, row)
	}
	return rows
}

// printActivities prints activities using the configured printer
func printActivities(activities []activityv1alpha1.Activity, printer printers.ResourcePrinter, out io.Writer) error {
	activityList := &activityv1alpha1.ActivityList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ActivityList",
			APIVersion: "activity.miloapis.com/v1alpha1",
		},
		Items: activities,
	}
	return printer.PrintObj(activityList, out)
}
