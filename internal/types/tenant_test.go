package types

import "testing"

func TestNormalizeTenantType(t *testing.T) {
	tests := []struct {
		name       string
		tenantType string
		want       string
	}{
		{name: "canonical project is unchanged", tenantType: TenantTypeProject, want: TenantTypeProject},
		{name: "lowercase project", tenantType: "project", want: TenantTypeProject},
		{name: "lowercase organization", tenantType: "organization", want: TenantTypeOrganization},
		{name: "lowercase user", tenantType: "user", want: TenantTypeUser},
		{name: "uppercase platform", tenantType: "Platform", want: TenantTypePlatform},
		{name: "mixed case project", tenantType: "pRoJeCt", want: TenantTypeProject},
		{name: "empty is unchanged", tenantType: "", want: ""},
		{name: "unrecognized type passes through unchanged", tenantType: "Tenant", want: "Tenant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTenantType(tt.tenantType); got != tt.want {
				t.Errorf("NormalizeTenantType(%q) = %q, want %q", tt.tenantType, got, tt.want)
			}
		})
	}
}
