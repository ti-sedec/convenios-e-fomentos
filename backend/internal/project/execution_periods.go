package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// ufExecution é a UF onde o projeto pode ser executado (decisão #12 do planejamento: a sede pode ser de fora, a execução não).
const ufExecution = "MT"

// executionPeriodParams valida as linhas de período/local e as converte para o banco. O rascunho aceita linhas incompletas;
// a obrigatoriedade dos campos é conferida no envio (executionPeriodIssues).
func executionPeriodParams(ctx context.Context, q *db.Queries, projectID uuid.UUID, in []ExecutionPeriod, p *problems) ([]db.AddProjectExecutionPeriodParams, error) {
	if len(in) > maxExecutionPeriods {
		p.add("executionPeriods", fmt.Sprintf("Informe no máximo %d períodos de execução.", maxExecutionPeriods))
		return nil, nil
	}

	// Município: uma consulta só para todas as linhas, para apontar a linha certa quando algum não for de MT.
	var codes []string
	seenCode := map[string]bool{}
	for _, r := range in {
		if c := strings.TrimSpace(r.MunicipalityIBGE); c != "" && !seenCode[c] {
			seenCode[c] = true
			codes = append(codes, c)
		}
	}
	valid := map[string]bool{}
	if len(codes) > 0 {
		found, err := q.ListExistingMunicipalityCodes(ctx, db.ListExistingMunicipalityCodesParams{Codes: codes, Uf: ufExecution})
		if err != nil {
			return nil, err
		}
		for _, c := range found {
			valid[c] = true
		}
	}

	out := make([]db.AddProjectExecutionPeriodParams, 0, len(in))
	seen := map[string]bool{}
	for i, r := range in {
		f, n := fmt.Sprintf("executionPeriods[%d]", i), i+1
		row := db.AddProjectExecutionPeriodParams{ProjectID: projectID, Position: int32(i)} //nolint:gosec // i < 50
		row.StartDate, row.EndDate = datePeriod(r.StartDate, f+".startDate", n, p), datePeriod(r.EndDate, f+".endDate", n, p)
		if row.StartDate.Valid && row.EndDate.Valid && row.EndDate.Time.Before(row.StartDate.Time) {
			p.add(f+".endDate", fmt.Sprintf("Período %d: o término não pode ser anterior ao início.", n))
		}
		if c := strings.TrimSpace(r.MunicipalityIBGE); c != "" {
			if valid[c] {
				row.MunicipalityIbge = &c
			} else {
				p.add(f+".municipalityIbge", fmt.Sprintf("Período %d: só municípios de Mato Grosso podem ser municípios de execução.", n))
			}
		}
		row.PlaceDetail = text(r.PlaceDetail)
		if tooLong(row.PlaceDetail, maxPlaceDetail) {
			p.add(f+".placeDetail", fmt.Sprintf("Período %d: use até %d caracteres no detalhe do local.", n, maxPlaceDetail))
		}
		// Linha idêntica a outra é quase certamente engano. Linhas sobrepostas, não: dois locais no mesmo dia são legítimos.
		if row.StartDate.Valid || row.EndDate.Valid || row.MunicipalityIbge != nil || row.PlaceDetail != "" {
			key := fmt.Sprintf("%s|%s|%s|%s", dateString(row.StartDate), dateString(row.EndDate), str(row.MunicipalityIbge), strings.ToLower(row.PlaceDetail))
			if seen[key] {
				p.add(f, fmt.Sprintf("Período %d: este período e local já foi informado em outra linha.", n))
			}
			seen[key] = true
		}
		out = append(out, row)
	}
	return out, nil
}

// executionPeriodsView monta as linhas para a resposta e os valores derivados: o menor início, o maior término e os municípios
// distintos (na ordem em que aparecem).
func executionPeriodsView(rows []db.ListProjectExecutionPeriodsRow) (periods []ExecutionPeriod, start, end string, munis []Municipality) {
	periods, munis = make([]ExecutionPeriod, len(rows)), []Municipality{}
	seen := map[string]bool{}
	for i, r := range rows {
		e := ExecutionPeriod{ID: r.ID.String(), StartDate: dateString(r.StartDate), EndDate: dateString(r.EndDate), MunicipalityIBGE: str(r.MunicipalityIbge), PlaceDetail: r.PlaceDetail}
		if r.MunicipalityIbge != nil {
			m := Municipality{IBGECode: *r.MunicipalityIbge, Name: str(r.MunicipalityName), UF: r.MunicipalityUf.String}
			e.Municipality = &m
			if !seen[m.IBGECode] {
				seen[m.IBGECode] = true
				munis = append(munis, m)
			}
		}
		// As datas AAAA-MM-DD se comparam como texto.
		if e.StartDate != "" && (start == "" || e.StartDate < start) {
			start = e.StartDate
		}
		if e.EndDate != "" && e.EndDate > end {
			end = e.EndDate
		}
		periods[i] = e
	}
	return periods, start, end, munis
}

// executionPeriodsText descreve os períodos e locais em uma linha cada ("01/11/2026 a 05/11/2026 — Cuiabá — Ginásio X"), para o
// comprovante e os resumos.
func executionPeriodsText(periods []ExecutionPeriod) []string {
	out := make([]string, 0, len(periods))
	for _, e := range periods {
		parts := []string{orDash(brDate(e.StartDate)) + " a " + orDash(brDate(e.EndDate))}
		if e.Municipality != nil {
			parts = append(parts, e.Municipality.Name)
		}
		if e.PlaceDetail != "" {
			parts = append(parts, e.PlaceDetail)
		}
		out = append(out, strings.Join(parts, " — "))
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// executionPeriodIssues confere as linhas no envio: ao menos uma, e cada uma com início, término e município.
func executionPeriodIssues(l *issueList, sec string, periods []ExecutionPeriod) {
	l.required(sec, "executionPeriods", "Informe pelo menos um período e local de execução.", len(periods) == 0)
	for i, e := range periods {
		f := fmt.Sprintf("executionPeriods[%d]", i)
		n := i + 1
		l.required(sec, f+".startDate", fmt.Sprintf("Período %d: informe o início previsto.", n), e.StartDate == "")
		l.required(sec, f+".endDate", fmt.Sprintf("Período %d: informe o término previsto.", n), e.EndDate == "")
		l.required(sec, f+".municipalityIbge", fmt.Sprintf("Período %d: informe o município de execução.", n), e.MunicipalityIBGE == "")
	}
}

// datePeriod lê uma data da linha; o erro diz de qual período se trata (a tela lista só as mensagens).
func datePeriod(s, field string, n int, p *problems) pgtype.Date {
	var local problems
	d := date(s, field, &local)
	for _, e := range local {
		p.add(e.Field, fmt.Sprintf("Período %d: %s", n, strings.ToLower(e.Message[:1])+e.Message[1:]))
	}
	return d
}
