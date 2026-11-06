package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"go.miloapis.com/activity/internal/activityprocessor"
)

const (
	// staffEmailDomainsFlag and staffEmailDomainsEnv set the email domains
	// treated as Datum staff.
	staffEmailDomainsFlag = "staff-email-domains"
	staffEmailDomainsEnv  = "ACTIVITY_STAFF_EMAIL_DOMAINS"

	// staffRedactionModeFlag and staffRedactionModeEnv select the staff
	// redaction mode (required or domain-only).
	staffRedactionModeFlag = "staff-redaction-mode"
	staffRedactionModeEnv  = "ACTIVITY_STAFF_REDACTION_MODE"
)

// addStaffRedactionModeFlag registers --staff-redaction-mode as a persistent
// root flag, like --staff-email-domains.
func addStaffRedactionModeFlag(root *cobra.Command) {
	root.PersistentFlags().String(staffRedactionModeFlag, activityprocessor.StaffRedactionRequired,
		"processor, reindex-worker, controller-manager (passed on to reindex workers) and the API server (serve): "+
			"\"required\" (default) recognises Datum staff from Milo staff-users and organization memberships and waits "+
			"for them (the processor blocks, tenant-scoped audit log queries return 503 until loaded); \"domain-only\" "+
			"uses only --staff-email-domains and never waits. domain-only is for clusters without Milo and must not be "+
			"used where customers have access. Also settable with "+staffRedactionModeEnv+".")
}

// staffRedactionMode returns the validated staff redaction mode: the flag
// when set, otherwise the environment variable, otherwise required.
func staffRedactionMode(cmd *cobra.Command) (string, error) {
	mode := os.Getenv(staffRedactionModeEnv)
	if f := cmd.Flags().Lookup(staffRedactionModeFlag); f != nil && f.Changed {
		mode = f.Value.String()
	}
	return activityprocessor.ParseStaffRedactionMode(strings.TrimSpace(mode))
}

// addStaffEmailDomainsFlag registers --staff-email-domains as a persistent
// flag on the root command, used by the processor and the reindex worker. It
// is persistent for the same reason as --allow-unenriched: reindex job
// template args come before the subcommand.
func addStaffEmailDomainsFlag(root *cobra.Command) {
	root.PersistentFlags().StringSlice(staffEmailDomainsFlag, activityprocessor.DefaultStaffEmailDomains,
		"processor, reindex-worker, controller-manager (passed on to reindex workers) and the API server (serve): "+
			"comma-separated email domains whose users count as Datum staff (shown to customers as \"Datum Support\" "+
			"outside their own organization), alongside staff-users members. Empty means the default; \""+
			activityprocessor.NoStaffEmailDomains+"\" disables the domain rule. Also settable with "+staffEmailDomainsEnv+".")
}

// staffEmailDomains returns the staff email domains: the flag when set,
// otherwise the environment variable, otherwise the default.
func staffEmailDomains(cmd *cobra.Command) []string {
	if f := cmd.Flags().Lookup(staffEmailDomainsFlag); f != nil && f.Changed {
		if v, err := cmd.Flags().GetStringSlice(staffEmailDomainsFlag); err == nil {
			return v
		}
	}
	if env := strings.TrimSpace(os.Getenv(staffEmailDomainsEnv)); env != "" {
		return strings.Split(env, ",")
	}
	return activityprocessor.DefaultStaffEmailDomains
}
