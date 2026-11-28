package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/controller"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// stubIndex is an indexWaiter whose WaitForSync result is fixed.
type stubIndex struct {
	err error
}

func (s stubIndex) WaitForSync(context.Context, time.Duration) error { return s.err }

func newReindexJobClient(t *testing.T) (*fake.ClientBuilder, *v1alpha1.ReindexJob) {
	t.Helper()
	job := &v1alpha1.ReindexJob{ObjectMeta: metav1.ObjectMeta{Name: "backfill"}}
	return fake.NewClientBuilder().
		WithScheme(controller.Scheme).
		WithObjects(job).
		WithStatusSubresource(&v1alpha1.ReindexJob{}), job
}

func jobAfter(t *testing.T, cl client.Client, name string) v1alpha1.ReindexJob {
	t.Helper()
	var got v1alpha1.ReindexJob
	if err := cl.Get(context.Background(), types.NamespacedName{Name: name}, &got); err != nil {
		t.Fatalf("get job: %v", err)
	}
	return got
}

func TestEnforceEnrichment(t *testing.T) {
	userForbidden := errors.Join(activityprocessor.ErrIndexUnavailable, errors.New("users.iam.miloapis.com is forbidden"))
	staffForbidden := errors.Join(activityprocessor.ErrIndexUnavailable, errors.New("groupmemberships.iam.miloapis.com is forbidden"))
	cases := []struct {
		name     string
		users    error
		staff    error
		allow    bool
		wantFail bool
		wantMsg  []string
	}{
		{name: "both synced", wantFail: false},
		{name: "user index forbidden", users: userForbidden, wantFail: true,
			wantMsg: []string{"actor display names are unavailable", userForbidden.Error(), allowUnenrichedEnv + "=true"}},
		{name: "user index timeout", users: activityprocessor.ErrIndexSyncTimeout, wantFail: true},
		{name: "user index unsynced, opt-out", users: activityprocessor.ErrIndexSyncTimeout, allow: true, wantFail: false},
		{name: "staff indexes unsynced", staff: staffForbidden, wantFail: true,
			wantMsg: []string{"staff memberships are unavailable", staffForbidden.Error(), "staff memberships are always required"}},
		{name: "staff indexes unsynced, opt-out does not help", staff: staffForbidden, allow: true, wantFail: true,
			wantMsg: []string{"even with unenriched re-indexing allowed"}},
		{name: "both unsynced, opt-out", users: userForbidden, staff: activityprocessor.ErrIndexSyncTimeout, allow: true, wantFail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder, job := newReindexJobClient(t)
			cl := builder.Build()
			err := enforceEnrichment(context.Background(), cl, job,
				enrichmentWaiters{users: stubIndex{err: tc.users}, staff: stubIndex{err: tc.staff}}, time.Second, tc.allow)
			if (err != nil) != tc.wantFail {
				t.Fatalf("enforceEnrichment error = %v, want failure %v", err, tc.wantFail)
			}
			got := jobAfter(t, cl, job.Name)
			if tc.wantFail != (got.Status.Phase == v1alpha1.ReindexJobFailed) {
				t.Errorf("phase = %q, want failed %v", got.Status.Phase, tc.wantFail)
			}
			for _, want := range tc.wantMsg {
				if !strings.Contains(got.Status.Message, want) {
					t.Errorf("status message %q does not mention %q", got.Status.Message, want)
				}
			}
		})
	}
}

// A StaffIndex that cannot sync fails the job even with the opt-out.
func TestEnforceEnrichment_RealStaffIndexUnsyncedFailsJob(t *testing.T) {
	builder, job := newReindexJobClient(t)
	cl := builder.Build()
	staff := &activityprocessor.StaffIndex{} // no API client: never syncs
	err := enforceEnrichment(context.Background(), cl, job, enrichmentWaiters{users: stubIndex{}, staff: staff}, time.Second, true)
	if err == nil {
		t.Fatal("expected failure with unsynced staff indexes")
	}
}

func TestStaffEmailDomainsFlagAndEnv(t *testing.T) {
	t.Setenv(staffEmailDomainsEnv, "")
	opts, err := runRootWithArgs(t, "--staff-email-domains=datum.net,example.org", "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if err != nil || opts == nil {
		t.Fatalf("root command failed: %v", err)
	}
	if got := strings.Join(opts.StaffEmailDomains, ","); got != "datum.net,example.org" {
		t.Errorf("StaffEmailDomains = %q", got)
	}

	t.Setenv(staffEmailDomainsEnv, "corp.example")
	opts, err = runRootWithArgs(t, "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if err != nil || opts == nil {
		t.Fatalf("root command failed: %v", err)
	}
	if got := strings.Join(opts.StaffEmailDomains, ","); got != "corp.example" {
		t.Errorf("StaffEmailDomains from env = %q", got)
	}

	t.Setenv(staffEmailDomainsEnv, "")
	opts, _ = runRootWithArgs(t, "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if got := strings.Join(opts.StaffEmailDomains, ","); got != "datum.net" {
		t.Errorf("default StaffEmailDomains = %q", got)
	}
}

// runRootWithArgs runs the real root command, capturing the reindex worker
// options instead of running the worker.
func runRootWithArgs(t *testing.T, args ...string) (*ReindexWorkerOptions, error) {
	t.Helper()
	var got *ReindexWorkerOptions
	orig := runReindexWorker
	runReindexWorker = func(_ context.Context, o *ReindexWorkerOptions) error {
		got = o
		return nil
	}
	t.Cleanup(func() { runReindexWorker = orig })

	root := NewActivityServerCommand()
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	return got, err
}

// The controller puts job template args before the controller's own args
// (reindex-worker, job name, NATS flags); see internal/controller/jobtemplate.go.
func TestReindexWorker_AllowUnenrichedInTemplateArgPosition(t *testing.T) {
	controllerArgs := []string{"reindex-worker", "myjob", "--nats-url=nats://nats:4222"}
	cases := []struct {
		name         string
		templateArgs []string
		env          string
		want         bool
	}{
		{name: "default off", templateArgs: []string{"--kubeconfig=/etc/kubernetes/kubeconfig"}, want: false},
		{name: "bare flag before subcommand", templateArgs: []string{"--kubeconfig=/etc/kubernetes/kubeconfig", "--allow-unenriched"}, want: true},
		{name: "explicit value before subcommand", templateArgs: []string{"--allow-unenriched=true", "--kubeconfig=/etc/kubernetes/kubeconfig"}, want: true},
		{name: "environment variable", templateArgs: []string{"--kubeconfig=/etc/kubernetes/kubeconfig"}, env: "true", want: true},
		{name: "flag overrides environment", templateArgs: []string{"--allow-unenriched=false"}, env: "true", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(allowUnenrichedEnv, tc.env)
			opts, err := runRootWithArgs(t, append(append([]string{}, tc.templateArgs...), controllerArgs...)...)
			if err != nil {
				t.Fatalf("root command failed: %v", err)
			}
			if opts == nil {
				t.Fatal("reindex worker was not run")
			}
			if opts.JobName != "myjob" {
				t.Errorf("JobName = %q, want %q", opts.JobName, "myjob")
			}
			if opts.AllowUnenriched != tc.want {
				t.Errorf("AllowUnenriched = %v, want %v", opts.AllowUnenriched, tc.want)
			}
		})
	}
}

func TestEnforceUserEnrichment_FailureMessageNamesWorkingOptOut(t *testing.T) {
	msg := unenrichedFailureMessage(activityprocessor.ErrUserIndexSyncTimeout)
	if !strings.Contains(msg, allowUnenrichedEnv+"=true") {
		t.Errorf("failure message %q does not name %s=true", msg, allowUnenrichedEnv)
	}
}

func TestIsTerminalReindexJob(t *testing.T) {
	for phase, want := range map[v1alpha1.ReindexJobPhase]bool{
		"":                           false,
		v1alpha1.ReindexJobPending:   false,
		v1alpha1.ReindexJobRunning:   false,
		v1alpha1.ReindexJobSucceeded: true,
		v1alpha1.ReindexJobFailed:    true,
	} {
		job := &v1alpha1.ReindexJob{Status: v1alpha1.ReindexJobStatus{Phase: phase}}
		if got := isTerminalReindexJob(job); got != want {
			t.Errorf("isTerminalReindexJob(%q) = %v, want %v", phase, got, want)
		}
	}
}

// A pod restarted after the worker finished must leave a terminal ReindexJob
// exactly as it is, so a Failed job stays Failed.
func TestFetchActiveReindexJob_TerminalJobIsLeftAlone(t *testing.T) {
	for _, phase := range []v1alpha1.ReindexJobPhase{v1alpha1.ReindexJobFailed, v1alpha1.ReindexJobSucceeded} {
		t.Run(string(phase), func(t *testing.T) {
			job := &v1alpha1.ReindexJob{
				ObjectMeta: metav1.ObjectMeta{Name: "backfill"},
				Status:     v1alpha1.ReindexJobStatus{Phase: phase, Message: "original reason"},
			}
			cl := fake.NewClientBuilder().WithScheme(controller.Scheme).WithObjects(job).
				WithStatusSubresource(&v1alpha1.ReindexJob{}).Build()
			var before v1alpha1.ReindexJob
			if err := cl.Get(context.Background(), types.NamespacedName{Name: "backfill"}, &before); err != nil {
				t.Fatalf("get: %v", err)
			}

			_, finished, err := fetchActiveReindexJob(context.Background(), cl, "backfill")
			if err != nil || !finished {
				t.Fatalf("fetchActiveReindexJob = finished %v, err %v; want finished", finished, err)
			}

			var after v1alpha1.ReindexJob
			if err := cl.Get(context.Background(), types.NamespacedName{Name: "backfill"}, &after); err != nil {
				t.Fatalf("get: %v", err)
			}
			if after.ResourceVersion != before.ResourceVersion || after.Status.Phase != phase || after.Status.Message != "original reason" {
				t.Errorf("terminal job was modified: before %+v, after %+v", before.Status, after.Status)
			}
		})
	}
}

func TestFetchActiveReindexJob_ActiveJobContinues(t *testing.T) {
	job := &v1alpha1.ReindexJob{ObjectMeta: metav1.ObjectMeta{Name: "backfill"}, Status: v1alpha1.ReindexJobStatus{Phase: v1alpha1.ReindexJobRunning}}
	cl := fake.NewClientBuilder().WithScheme(controller.Scheme).WithObjects(job).Build()
	got, finished, err := fetchActiveReindexJob(context.Background(), cl, "backfill")
	if err != nil || finished || got == nil || got.Name != "backfill" {
		t.Fatalf("fetchActiveReindexJob = (%v, %v, %v), want the job and finished=false", got, finished, err)
	}
}

func TestStaffRedactionModeFlagAndEnv(t *testing.T) {
	t.Setenv(staffRedactionModeEnv, "")
	opts, err := runRootWithArgs(t, "--staff-redaction-mode=domain-only", "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if err != nil || opts == nil || opts.StaffRedactionMode != "domain-only" {
		t.Fatalf("flag: opts = %+v, err = %v", opts, err)
	}

	t.Setenv(staffRedactionModeEnv, "domain-only")
	opts, err = runRootWithArgs(t, "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if err != nil || opts.StaffRedactionMode != "domain-only" {
		t.Fatalf("env: opts = %+v, err = %v", opts, err)
	}

	t.Setenv(staffRedactionModeEnv, "")
	opts, _ = runRootWithArgs(t, "reindex-worker", "myjob", "--nats-url=nats://n:4222")
	if opts.StaffRedactionMode != "required" {
		t.Errorf("default mode = %q, want required", opts.StaffRedactionMode)
	}

	if _, err := runRootWithArgs(t, "--staff-redaction-mode=off", "reindex-worker", "myjob", "--nats-url=nats://n:4222"); err == nil {
		t.Error("invalid mode accepted")
	}
}
