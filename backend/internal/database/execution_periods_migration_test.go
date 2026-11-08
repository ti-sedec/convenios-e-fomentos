package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/testutil"
)

// A 00036 troca "um par de datas + um detalhe + uma lista de municípios" por linhas {início, término, município, detalhe}. Este teste
// desce para o formato antigo, grava projetos nele e sobe de novo, para conferir o que acontece com os dados já existentes.
func TestMigracaoPeriodosDeExecucaoPreservaOsDadosAntigos(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPool(t)
	m := database.NewMigrator(pool)
	t.Cleanup(func() { _ = m.Close() })

	require.NoError(t, m.Down(ctx), "desfazer a 00036")

	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		require.NoError(t, err, sql)
	}
	exec(`INSERT INTO municipalities (ibge_code, name, uf, search_name) VALUES
		('5103403', 'Cuiabá', 'MT', 'cuiaba'), ('5108402', 'Várzea Grande', 'MT', 'varzea grande'), ('5107925', 'Sorriso', 'MT', 'sorriso')`)
	var admin string
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO admins (first_name, last_name, email) VALUES ('A', 'B', 'a@b.com') RETURNING id`).Scan(&admin))
	newProject := func(start, end any, detail string) string {
		var id string
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (created_by_id, proponent_name, proponent_cpf, start_date, end_date, execution_place_detail) VALUES ($1, 'Fulano', '12345678909', $2, $3, $4) RETURNING id`,
			admin, start, end, detail).Scan(&id))
		return id
	}
	withMunicipalities := newProject("2026-11-01", "2027-05-01", "Av. Principal")
	exec(`INSERT INTO project_municipalities VALUES ($1, '5108402'), ($1, '5103403')`, withMunicipalities)
	noMunicipality := newProject("2026-12-01", "2026-12-02", "")
	onlyMunicipality := newProject(nil, nil, "")
	exec(`INSERT INTO project_municipalities VALUES ($1, '5107925')`, onlyMunicipality)
	empty := newProject(nil, nil, "")

	require.NoError(t, m.Up(ctx))

	type row struct {
		position         int
		start, end, ibge *string
		detail           string
	}
	rows := func(project string) []row {
		t.Helper()
		r, err := pool.Query(ctx, `SELECT position, start_date::text, end_date::text, municipality_ibge, place_detail
			FROM project_execution_periods WHERE project_id = $1 ORDER BY position`, project)
		require.NoError(t, err)
		defer r.Close()
		var out []row
		for r.Next() {
			var x row
			require.NoError(t, r.Scan(&x.position, &x.start, &x.end, &x.ibge, &x.detail))
			out = append(out, x)
		}
		require.NoError(t, r.Err())
		return out
	}
	s := func(v string) *string { return &v }

	// Um município por linha (na ordem alfabética), com as datas do projeto; o detalhe vai para todas.
	assert.Equal(t, []row{
		{0, s("2026-11-01"), s("2027-05-01"), s("5103403"), "Av. Principal"},
		{1, s("2026-11-01"), s("2027-05-01"), s("5108402"), "Av. Principal"},
	}, rows(withMunicipalities))
	// Datas sem município: uma linha sem município.
	assert.Equal(t, []row{{0, s("2026-12-01"), s("2026-12-02"), nil, ""}}, rows(noMunicipality))
	// Município sem datas: uma linha sem datas.
	assert.Equal(t, []row{{0, nil, nil, s("5107925"), ""}}, rows(onlyMunicipality))
	// Projeto sem nada não ganha linha.
	assert.Empty(t, rows(empty))

	// Descer de novo volta ao formato antigo, resumindo as linhas (menor início, maior término, municípios distintos).
	require.NoError(t, m.Down(ctx))
	var start, end, detail string
	require.NoError(t, pool.QueryRow(ctx, `SELECT start_date::text, end_date::text, execution_place_detail FROM projects WHERE id = $1`, withMunicipalities).Scan(&start, &end, &detail))
	assert.Equal(t, []string{"2026-11-01", "2027-05-01", "Av. Principal"}, []string{start, end, detail})
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM project_municipalities WHERE project_id = $1`, withMunicipalities).Scan(&n))
	assert.Equal(t, 2, n)
}
