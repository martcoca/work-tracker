package main

import (
	"strings"
	"testing"

	"github.com/martcoca/work-tracker/surface"
)

func TestVerifyStatusesRequiresFreshPacketAndAuthorityExports(t *testing.T) {
	valid := []surface.HeldExportStatus{
		{Name: "packets", Available: true, ServiceOwned: true},
		{Name: "tenant-directory", Available: true, Required: true},
		{Name: "agent-grants", Available: true, Required: true},
	}
	if err := verifyStatuses(valid); err != nil {
		t.Fatalf("valid statuses: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func([]surface.HeldExportStatus) []surface.HeldExportStatus
		want   string
	}{
		{
			name: "packets absent",
			mutate: func(statuses []surface.HeldExportStatus) []surface.HeldExportStatus {
				statuses[0].Available = false
				return statuses
			},
			want: "packets did not verify",
		},
		{
			name: "authority optional",
			mutate: func(statuses []surface.HeldExportStatus) []surface.HeldExportStatus {
				statuses[1].Required = false
				return statuses
			},
			want: "tenant-directory did not verify as required external authority",
		},
		{
			name: "missing status",
			mutate: func(statuses []surface.HeldExportStatus) []surface.HeldExportStatus {
				return statuses[:2]
			},
			want: "agent-grants status is missing",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			statuses := append([]surface.HeldExportStatus(nil), valid...)
			err := verifyStatuses(test.mutate(statuses))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}
