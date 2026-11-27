package maintenance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/maintenance"
	"github.com/wagner/convenios-e-fomentos/backend/internal/testutil"
)

// E-mails já resolvidos têm corpo com dados pessoais: saem depois da carência. Os pendentes nunca saem.
func TestCleanupApagaEmailsResolvidosAntigos(t *testing.T) {
	pool := testutil.NewPool(t)
	ctx := context.Background()
	for i, c := range []struct{ status, age string }{
		{"sent", "30 days"}, {"failed", "30 days"}, {"sent", "2 days"}, {"pending", "30 days"},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO outbound_emails (kind, to_email, subject, body, dedup_key, status, created_at)
			VALUES ('k', 'a@x.com', 's', 'corpo com dados pessoais', $1, $2, now() - $3::interval)`, string(rune('a'+i)), c.status, c.age)
		require.NoError(t, err)
	}
	res, err := maintenance.Cleanup(ctx, db.New(pool), 7)
	require.NoError(t, err)
	assert.EqualValues(t, 2, res.ResolvedEmails, "enviado e falho antigos")

	var left []string
	rows, err := pool.Query(ctx, "SELECT dedup_key FROM outbound_emails ORDER BY dedup_key")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		left = append(left, k)
	}
	assert.Equal(t, []string{"c", "d"}, left, "ficam o enviado recente e o pendente")

	_, err = maintenance.Cleanup(ctx, db.New(pool), -1)
	assert.Error(t, err)
}
