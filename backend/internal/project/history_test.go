package project

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Os mesmos casos de TestRegraDeQuemEstaComOProjeto (app), que confere a função SQL project_holder_type.
func TestHolderOf(t *testing.T) {
	an := uuid.New()
	cases := []struct {
		status, stage, framing string
		analyst                *uuid.UUID
		want                   string
	}{
		{"archived", "in_analysis", "", &an, HolderNone},
		{"archived", "awaiting_proponent", "", nil, HolderNone},
		{"submitted", "awaiting_checklist", "", nil, HolderTeam},
		{"submitted", "in_analysis", "", nil, HolderTeam},
		{"submitted", "in_analysis", "", &an, HolderAnalyst},
		{"submitted", "in_analysis", "pending", &an, HolderDepartment},
		{"submitted", "in_analysis", "pending", nil, HolderDepartment},
		{"submitted", "awaiting_proponent", "", &an, HolderProponent},
		{"submitted", "awaiting_opinion", "confirmed", &an, HolderDepartment},
	}
	for _, c := range cases {
		r := db.Project{Status: c.status, Stage: ptr(c.stage), AnalystID: c.analyst}
		if c.framing != "" {
			r.FramingStatus = ptr(c.framing)
		}
		assert.Equal(t, c.want, holderOf(r), "%+v", c)
	}
}
