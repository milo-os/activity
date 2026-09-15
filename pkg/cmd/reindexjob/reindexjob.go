package reindexjob

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/cmd/util"

	activityv1alpha1 "go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	clientset "go.miloapis.com/activity/pkg/client/clientset/versioned"
	"go.miloapis.com/activity/pkg/cmd/common"
)

// NewReindexJobCommand creates the reindex parent command with subcommands.
func NewReindexJobCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Manage ReindexJob resources",
		Long: `Commands for creating and managing ReindexJob resources.

A ReindexJob triggers re-processing of historical audit logs and events through
current ActivityPolicy rules. Use this to fix policy bugs retroactively, add
coverage for new policies, or refine activity summaries after policy improvements.

ReindexJob is a one-shot resource: once completed or failed, it cannot be re-run.
Create a new ReindexJob for subsequent re-indexing operations.

Examples:
  # Reindex the last 7 days
  kubectl activity reindex create --start-time now-7d

  # Reindex a specific time range with a specific policy
  kubectl activity reindex create --start-time 2026-02-01T00:00:00Z --end-time 2026-03-01T00:00:00Z --policy httpproxy-policy

  # List all reindex jobs
  kubectl activity reindex list

  # Check status of a job
  kubectl activity reindex status my-reindex-job

  # Delete a completed job
  kubectl activity reindex delete my-reindex-job
`,
	}

	cmd.AddCommand(NewCreateCommand(f, ioStreams))
	cmd.AddCommand(NewListCommand(f, ioStreams))
	cmd.AddCommand(NewStatusCommand(f, ioStreams))
	cmd.AddCommand(NewDeleteCommand(f, ioStreams))

	return cmd
}

// newActivityClient builds an activity clientset from the given factory.
func newActivityClient(f util.Factory) (*clientset.Clientset, error) {
	config, err := f.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create activity client: %w", err)
	}
	return client, nil
}

// CreateOptions contains the options for creating a ReindexJob.
type CreateOptions struct {
	// Time range
	StartTime string
	EndTime   string

	// Policy selection
	PolicyNames []string

	// Processing config
	BatchSize int32
	RateLimit int32
	DryRun    bool

	// Lifecycle
	TTL int32

	// Common flags
	Output common.OutputFlags

	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewCreateOptions creates a new CreateOptions with defaults.
func NewCreateOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *CreateOptions {
	return &CreateOptions{
		IOStreams:  ioStreams,
		Factory:   f,
		PrintFlags: genericclioptions.NewPrintFlags(""),
		EndTime:   "now",
	}
}

// NewCreateCommand creates the reindex create subcommand.
func NewCreateCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewCreateOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "create [flags]",
		Short: "Create a new ReindexJob",
		Long: `Create a new ReindexJob to re-process historical audit logs and events.

Time Format:
  Relative: "now-7d", "now-2h", "now-30m" (units: s, m, h, d, w)
  Absolute: "2026-01-01T00:00:00Z" (RFC3339 with timezone)

Note: Relative times are resolved when the job STARTS processing, not when
the resource is created. This ensures consistent time ranges even if the job
is queued.

KUBERNETES EVENT LIMITATION:
When a Kubernetes Event is updated (e.g., count incremented), it retains the
same UID. Re-indexing will produce ONE activity per Event UID, reflecting the
Event's final state. Use --policy to scope re-indexing to specific policies
if you need to preserve historical event occurrences.

Examples:
  # Reindex the last 7 days
  kubectl activity reindex create --start-time now-7d

  # Reindex a specific time range
  kubectl activity reindex create --start-time 2026-02-01T00:00:00Z --end-time 2026-03-01T00:00:00Z

  # Reindex with specific policies only
  kubectl activity reindex create --start-time now-7d --policy httpproxy-policy --policy dns-policy

  # Dry run to estimate impact
  kubectl activity reindex create --start-time now-7d --dry-run

  # Auto-delete 1 hour after completion
  kubectl activity reindex create --start-time now-7d --ttl 3600
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

	cmd.Flags().StringVar(&o.StartTime, "start-time", "", "Start of time range to reindex (required; relative: 'now-7d' or absolute: RFC3339)")
	cmd.Flags().StringVar(&o.EndTime, "end-time", "now", "End of time range to reindex (relative: 'now' or absolute: RFC3339)")
	cmd.Flags().StringArrayVar(&o.PolicyNames, "policy", nil, "Limit reindexing to specific ActivityPolicy names (may be specified multiple times)")
	cmd.Flags().Int32Var(&o.BatchSize, "batch-size", 0, "Number of events per batch (100-10000; default: server default of 1000)")
	cmd.Flags().Int32Var(&o.RateLimit, "rate-limit", 0, "Maximum events per second (10-1000; default: server default of 100)")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Preview changes without writing activities")
	cmd.Flags().Int32Var(&o.TTL, "ttl", 0, "Seconds to retain job after completion (0 = retain indefinitely)")

	common.AddOutputFlags(cmd, &o.Output)
	o.PrintFlags.AddFlags(cmd)

	_ = cmd.MarkFlagRequired("start-time")

	return cmd
}

// Complete fills in missing options.
func (o *CreateOptions) Complete(_ *cobra.Command) error {
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

// Validate checks that required options are set correctly.
func (o *CreateOptions) Validate() error {
	if o.StartTime == "" {
		return fmt.Errorf("--start-time is required")
	}
	if o.BatchSize != 0 && (o.BatchSize < 100 || o.BatchSize > 10000) {
		return fmt.Errorf("--batch-size must be between 100 and 10000")
	}
	if o.RateLimit != 0 && (o.RateLimit < 10 || o.RateLimit > 1000) {
		return fmt.Errorf("--rate-limit must be between 10 and 1000")
	}
	if o.TTL < 0 {
		return fmt.Errorf("--ttl must be >= 0")
	}
	return nil
}

// Run creates the ReindexJob resource.
func (o *CreateOptions) Run(ctx context.Context) error {
	client, err := newActivityClient(o.Factory)
	if err != nil {
		return err
	}

	job := &activityv1alpha1.ReindexJob{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "reindex-",
		},
		Spec: activityv1alpha1.ReindexJobSpec{
			TimeRange: activityv1alpha1.ReindexTimeRange{
				StartTime: o.StartTime,
				EndTime:   o.EndTime,
			},
		},
	}

	// Apply policy selector if names were provided.
	if len(o.PolicyNames) > 0 {
		job.Spec.PolicySelector = &activityv1alpha1.ReindexPolicySelector{
			Names: o.PolicyNames,
		}
	}

	// Apply processing config only when non-default values were set.
	if o.BatchSize != 0 || o.RateLimit != 0 || o.DryRun {
		cfg := &activityv1alpha1.ReindexConfig{
			DryRun: o.DryRun,
		}
		if o.BatchSize != 0 {
			cfg.BatchSize = o.BatchSize
		}
		if o.RateLimit != 0 {
			cfg.RateLimit = o.RateLimit
		}
		job.Spec.Config = cfg
	}

	// Apply TTL when explicitly requested (0 means retain indefinitely).
	if o.TTL > 0 {
		ttl := o.TTL
		job.Spec.TTLSecondsAfterFinished = &ttl
	}

	if o.Output.Debug {
		fmt.Fprintf(o.ErrOut, "DEBUG: Creating ReindexJob: %+v\n", job.Spec)
	}

	result, err := client.ActivityV1alpha1().ReindexJobs().Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create reindex job: %w", err)
	}

	fmt.Fprintf(o.Out, "reindexjob/%s created\n", result.Name)
	return nil
}

// ListOptions contains the options for listing ReindexJobs.
type ListOptions struct {
	Output     common.OutputFlags
	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewListOptions creates a new ListOptions with defaults.
func NewListOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *ListOptions {
	return &ListOptions{
		IOStreams:  ioStreams,
		Factory:   f,
		PrintFlags: genericclioptions.NewPrintFlags(""),
	}
}

// NewListCommand creates the reindex list subcommand.
func NewListCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewListOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "list [flags]",
		Short: "List ReindexJob resources",
		Long: `List all ReindexJob resources and their current status.

Examples:
  # List all reindex jobs
  kubectl activity reindex list

  # Output as JSON
  kubectl activity reindex list -o json
`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	common.AddOutputFlags(cmd, &o.Output)
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

// Validate checks that required options are set correctly.
func (o *ListOptions) Validate() error {
	return nil
}

// Complete fills in missing options.
func (o *ListOptions) Complete(_ *cobra.Command) error {
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

// Run lists ReindexJob resources.
func (o *ListOptions) Run(ctx context.Context) error {
	client, err := newActivityClient(o.Factory)
	if err != nil {
		return err
	}

	list, err := client.ActivityV1alpha1().ReindexJobs().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list reindex jobs: %w", err)
	}

	if !common.IsDefaultOutputFormat(o.PrintFlags) {
		printer, err := common.CreatePrinter(o.PrintFlags)
		if err != nil {
			return fmt.Errorf("failed to create printer: %w", err)
		}
		return printer.PrintObj(list, o.Out)
	}

	return o.printTable(list.Items)
}

// printTable prints reindex jobs as a formatted table.
func (o *ListOptions) printTable(jobs []activityv1alpha1.ReindexJob) error {
	table := reindexJobsToTable(jobs)
	tablePrinter := common.CreateTablePrinter(o.Output.NoHeaders)
	return tablePrinter.PrintObj(table, o.Out)
}

// StatusOptions contains the options for showing ReindexJob status.
type StatusOptions struct {
	Name string

	Output     common.OutputFlags
	PrintFlags *genericclioptions.PrintFlags
	genericclioptions.IOStreams
	Factory util.Factory
}

// NewStatusOptions creates a new StatusOptions with defaults.
func NewStatusOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *StatusOptions {
	return &StatusOptions{
		IOStreams:  ioStreams,
		Factory:   f,
		PrintFlags: genericclioptions.NewPrintFlags(""),
	}
}

// NewStatusCommand creates the reindex status subcommand.
func NewStatusCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewStatusOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "status NAME [flags]",
		Short: "Show detailed status of a ReindexJob",
		Long: `Show the detailed status and progress of a specific ReindexJob.

Examples:
  # Show status of a job
  kubectl activity reindex status my-reindex-job

  # Output as YAML
  kubectl activity reindex status my-reindex-job -o yaml
`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Name = args[0]
			if err := o.Complete(cmd); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	common.AddOutputFlags(cmd, &o.Output)
	o.PrintFlags.AddFlags(cmd)

	return cmd
}

// Validate checks that required options are set correctly.
func (o *StatusOptions) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

// Complete fills in missing options.
func (o *StatusOptions) Complete(_ *cobra.Command) error {
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

// Run fetches and displays the ReindexJob status.
func (o *StatusOptions) Run(ctx context.Context) error {
	client, err := newActivityClient(o.Factory)
	if err != nil {
		return err
	}

	job, err := client.ActivityV1alpha1().ReindexJobs().Get(ctx, o.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get reindex job %q: %w", o.Name, err)
	}

	if !common.IsDefaultOutputFormat(o.PrintFlags) {
		printer, err := common.CreatePrinter(o.PrintFlags)
		if err != nil {
			return fmt.Errorf("failed to create printer: %w", err)
		}
		return printer.PrintObj(job, o.Out)
	}

	return o.printDetail(job)
}

// printDetail prints detailed status for a single ReindexJob.
func (o *StatusOptions) printDetail(job *activityv1alpha1.ReindexJob) error {
	fmt.Fprintf(o.Out, "Name:        %s\n", job.Name)
	fmt.Fprintf(o.Out, "Phase:       %s\n", phaseString(job.Status.Phase))

	if job.Status.Message != "" {
		fmt.Fprintf(o.Out, "Message:     %s\n", job.Status.Message)
	}

	fmt.Fprintf(o.Out, "\nSpec:\n")
	fmt.Fprintf(o.Out, "  Time Range: %s -> %s\n", job.Spec.TimeRange.StartTime, job.Spec.TimeRange.EndTime)

	if job.Spec.PolicySelector != nil && len(job.Spec.PolicySelector.Names) > 0 {
		fmt.Fprintf(o.Out, "  Policies:   %v\n", job.Spec.PolicySelector.Names)
	}

	if job.Spec.Config != nil {
		if job.Spec.Config.DryRun {
			fmt.Fprintf(o.Out, "  Dry Run:    true\n")
		}
		if job.Spec.Config.BatchSize != 0 {
			fmt.Fprintf(o.Out, "  Batch Size: %d\n", job.Spec.Config.BatchSize)
		}
		if job.Spec.Config.RateLimit != 0 {
			fmt.Fprintf(o.Out, "  Rate Limit: %d events/sec\n", job.Spec.Config.RateLimit)
		}
	}

	if job.Status.StartedAt != nil || job.Status.CompletedAt != nil {
		fmt.Fprintf(o.Out, "\nTimestamps:\n")
		if job.Status.StartedAt != nil {
			fmt.Fprintf(o.Out, "  Started:   %s\n", 		job.Status.StartedAt.UTC().Format(time.RFC3339))
		}
		if job.Status.CompletedAt != nil {
			fmt.Fprintf(o.Out, "  Completed: %s\n", 		job.Status.CompletedAt.UTC().Format(time.RFC3339))
		}
	}

	if job.Status.Progress != nil {
		p := job.Status.Progress
		fmt.Fprintf(o.Out, "\nProgress:\n")
		if p.TotalEvents > 0 {
			pct := float64(p.ProcessedEvents) / float64(p.TotalEvents) * 100
			fmt.Fprintf(o.Out, "  Events:      %d / %d (%.1f%%)\n", p.ProcessedEvents, p.TotalEvents, pct)
		} else {
			fmt.Fprintf(o.Out, "  Events:      %d processed\n", p.ProcessedEvents)
		}
		fmt.Fprintf(o.Out, "  Activities:  %d generated\n", p.ActivitiesGenerated)
		if p.Errors > 0 {
			fmt.Fprintf(o.Out, "  Errors:      %d\n", p.Errors)
		}
		if p.TotalBatches > 0 {
			fmt.Fprintf(o.Out, "  Batches:     %d / %d\n", p.CurrentBatch, p.TotalBatches)
		}
	}

	if len(job.Status.Conditions) > 0 {
		fmt.Fprintf(o.Out, "\nConditions:\n")
		for _, c := range job.Status.Conditions {
			fmt.Fprintf(o.Out, "  %s=%s", c.Type, c.Status)
			if c.Reason != "" {
				fmt.Fprintf(o.Out, " (%s)", c.Reason)
			}
			if c.Message != "" {
				fmt.Fprintf(o.Out, ": %s", c.Message)
			}
			fmt.Fprintln(o.Out)
		}
	}

	return nil
}

// DeleteOptions contains the options for deleting a ReindexJob.
type DeleteOptions struct {
	Name string

	genericclioptions.IOStreams
	Factory util.Factory
}

// NewDeleteOptions creates a new DeleteOptions.
func NewDeleteOptions(f util.Factory, ioStreams genericclioptions.IOStreams) *DeleteOptions {
	return &DeleteOptions{
		IOStreams: ioStreams,
		Factory:  f,
	}
}

// NewDeleteCommand creates the reindex delete subcommand.
func NewDeleteCommand(f util.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewDeleteOptions(f, ioStreams)

	cmd := &cobra.Command{
		Use:   "delete NAME [flags]",
		Short: "Delete a ReindexJob",
		Long: `Delete a ReindexJob resource.

Examples:
  # Delete a reindex job
  kubectl activity reindex delete my-reindex-job
`,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Name = args[0]
			if err := o.Complete(cmd); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	return cmd
}

// Validate checks that required options are set correctly.
func (o *DeleteOptions) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

// Complete fills in missing options.
func (o *DeleteOptions) Complete(_ *cobra.Command) error {
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

// Run deletes the named ReindexJob.
func (o *DeleteOptions) Run(ctx context.Context) error {
	client, err := newActivityClient(o.Factory)
	if err != nil {
		return err
	}

	if err := client.ActivityV1alpha1().ReindexJobs().Delete(ctx, o.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete reindex job %q: %w", o.Name, err)
	}

	fmt.Fprintf(o.Out, "reindexjob/%s deleted\n", o.Name)
	return nil
}

// reindexJobsToTable converts ReindexJobs to a metav1.Table for consistent table output.
func reindexJobsToTable(jobs []activityv1alpha1.ReindexJob) *metav1.Table {
	table := &metav1.Table{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Table",
			APIVersion: "meta.k8s.io/v1",
		},
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string", Description: "Job name"},
			{Name: "Phase", Type: "string", Description: "Current phase"},
			{Name: "Progress", Type: "string", Description: "Events processed / total"},
			{Name: "Started", Type: "string", Description: "When processing began"},
			{Name: "Completed", Type: "string", Description: "When processing finished"},
		},
		Rows: reindexJobsToRows(jobs),
	}
	return table
}

// reindexJobsToRows converts ReindexJobs to table rows.
func reindexJobsToRows(jobs []activityv1alpha1.ReindexJob) []metav1.TableRow {
	rows := make([]metav1.TableRow, 0, len(jobs))
	for i := range jobs {
		job := &jobs[i]

		progress := progressString(job.Status.Progress)

		started := ""
		if job.Status.StartedAt != nil {
			started = 		job.Status.StartedAt.UTC().Format(time.RFC3339)
		}

		completed := ""
		if job.Status.CompletedAt != nil {
			completed = 		job.Status.CompletedAt.UTC().Format(time.RFC3339)
		}

		row := metav1.TableRow{
			Cells: []interface{}{
				job.Name,
				phaseString(job.Status.Phase),
				progress,
				started,
				completed,
			},
		}
		rows = append(rows, row)
	}
	return rows
}

// phaseString returns the phase as a string, defaulting to "Pending" when unset.
func phaseString(phase activityv1alpha1.ReindexJobPhase) string {
	if phase == "" {
		return string(activityv1alpha1.ReindexJobPending)
	}
	return string(phase)
}

// progressString formats progress as "processed/total" or "-" when unavailable.
func progressString(p *activityv1alpha1.ReindexProgress) string {
	if p == nil {
		return "-"
	}
	if p.TotalEvents > 0 {
		return fmt.Sprintf("%d/%d", p.ProcessedEvents, p.TotalEvents)
	}
	if p.ProcessedEvents > 0 {
		return fmt.Sprintf("%d", p.ProcessedEvents)
	}
	return "-"
}
