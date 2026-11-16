package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// ReportInput é o corpo de POST /reports. Kind: summary (resposta imediata, o PDF) ou full (entra na fila: 202 com o registro). As
// opções valem true se omitidas; CoverSheets só vale para o completo.
type ReportInput struct {
	Kind        string `json:"kind"`
	PageNumbers *bool  `json:"pageNumbers"`
	CoverSheets *bool  `json:"coverSheets"`
}

func (in ReportInput) options() ReportOptions {
	on := func(b *bool) bool { return b == nil || *b }
	o := ReportOptions{PageNumbers: on(in.PageNumbers), CoverSheets: on(in.CoverSheets)}
	if in.Kind == ReportSummary {
		o.CoverSheets = false
	}
	return o
}

// ReportRecord é uma geração do histórico. Data só vem no detalhe.
type ReportRecord struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"`
	VersionNumber int32      `json:"versionNumber"`
	PageNumbers   bool       `json:"pageNumbers"`
	CoverSheets   bool       `json:"coverSheets"`
	GeneratedAt   time.Time  `json:"generatedAt"`
	GeneratedBy   string     `json:"generatedBy"`
	GeneratedByID *uuid.UUID `json:"generatedById"`
	// GeneratedByCPF é mascarado.
	GeneratedByCPF string `json:"generatedByCpf"`
	PageCount      int32  `json:"pageCount"`
	SizeBytes      int64  `json:"sizeBytes"`
	SHA256         string `json:"sha256"`
	// Situação da montagem (o resumido já nasce done). Só o relatório completo passa por queued e running.
	Status        string `json:"status"` // queued | running | done | failed
	ProgressDone  int32  `json:"progressDone"`
	ProgressTotal int32  `json:"progressTotal"`
	// Downloadable: o PDF ainda está disponível. O completo vence (ExpiresAt) e então só o registro permanece; o resumido é sempre
	// refeito dos dados.
	Downloadable bool            `json:"downloadable"`
	ExpiresAt    *time.Time      `json:"expiresAt"`
	StartedAt    *time.Time      `json:"startedAt"`
	FinishedAt   *time.Time      `json:"finishedAt"`
	Warnings     []ReportWarning `json:"warnings"`
	Error        string          `json:"error"`
	Stats        *ReportStats    `json:"stats,omitempty"`
	Data         *ReportData     `json:"data,omitempty"`
}

func decodeWarnings(b []byte) []ReportWarning {
	out := []ReportWarning{}
	_ = json.Unmarshal(b, &out)
	return out
}

func decodeStats(b []byte) *ReportStats {
	var st ReportStats
	if json.Unmarshal(b, &st) != nil || st == (ReportStats{}) {
		return nil
	}
	return &st
}

// recordOfRow converte a linha completa do banco.
func recordOfRow(r db.ProjectReport) ReportRecord {
	downloadable := r.Kind == ReportSummary || (r.ObjectKey != nil && r.ExpiresAt != nil && r.ExpiresAt.After(time.Now()))
	return ReportRecord{ID: r.ID, Kind: r.Kind, VersionNumber: r.VersionNumber, PageNumbers: r.PageNumbers, CoverSheets: r.CoverSheets,
		GeneratedAt: r.GeneratedAt, GeneratedBy: r.GeneratedByName, GeneratedByID: r.GeneratedByID, GeneratedByCPF: r.GeneratedByCpf,
		PageCount: r.PageCount, SizeBytes: r.SizeBytes, SHA256: r.Sha256, Status: r.Status, ProgressDone: r.ProgressDone, ProgressTotal: r.ProgressTotal,
		Downloadable: downloadable && r.Status == "done", ExpiresAt: r.ExpiresAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		Warnings: decodeWarnings(r.Warnings), Error: r.Error, Stats: decodeStats(r.Stats)}
}

func recordOfListRow(x db.ListProjectReportsRow) ReportRecord {
	return ReportRecord{ID: x.ID, Kind: x.Kind, VersionNumber: x.VersionNumber, PageNumbers: x.PageNumbers, CoverSheets: x.CoverSheets,
		GeneratedAt: x.GeneratedAt, GeneratedBy: x.GeneratedByName, GeneratedByID: x.GeneratedByID, GeneratedByCPF: x.GeneratedByCpf,
		PageCount: x.PageCount, SizeBytes: x.SizeBytes, SHA256: x.Sha256, Status: x.Status, ProgressDone: x.ProgressDone, ProgressTotal: x.ProgressTotal,
		Downloadable: x.Status == "done" && (x.Kind == ReportSummary || x.Downloadable.Bool), ExpiresAt: x.ExpiresAt, StartedAt: x.StartedAt,
		FinishedAt: x.FinishedAt, Warnings: decodeWarnings(x.Warnings), Error: x.Error, Stats: decodeStats(x.Stats)}
}

func errReportUnavailable(a *ReportAccess) error {
	msg := "O relatório não está disponível para este projeto."
	if a != nil && a.Reason != "" {
		msg = a.Reason
	}
	return httpapi.ErrConflict("report_unavailable", msg)
}

// reportProject carrega o projeto e confere o alcance da leitura. Rascunho e projeto fora do alcance são 404.
func (s *Service) reportProject(ctx context.Context, q *db.Queries, id uuid.UUID, read authz.Decision) (db.Project, error) {
	r, err := q.GetProject(ctx, id)
	if err != nil {
		return db.Project{}, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) || r.Status == StatusDraft {
		return db.Project{}, httpapi.ErrNotFound()
	}
	return r, nil
}

// GenerateReport gera o relatório do projeto a partir do retrato da última versão enviada e grava o registro (quem, quando, opções e
// os dados usados). O resumido devolve o PDF na hora. O completo entra na fila (a montagem junta os documentos e demora): devolve só o
// registro, com o PDF vazio.
func (s *Service) GenerateReport(ctx context.Context, actor Actor, id uuid.UUID, in ReportInput, read authz.Decision) ([]byte, ReportRecord, error) {
	if in.Kind != ReportSummary && in.Kind != ReportFull {
		return nil, ReportRecord{}, httpapi.ErrValidation([]httpapi.FieldError{{Field: "kind", Message: "Informe \"summary\" ou \"full\"."}})
	}
	r, err := s.reportProject(ctx, s.q, id, read)
	if err != nil {
		return nil, ReportRecord{}, err
	}
	if a := reportAccess(r); a == nil || !a.Available {
		return nil, ReportRecord{}, errReportUnavailable(a)
	}
	v, err := s.q.GetLatestProjectVersion(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ReportRecord{}, httpapi.ErrNotFound()
	}
	if err != nil {
		return nil, ReportRecord{}, err
	}
	var snap VersionSnapshot
	if err := json.Unmarshal(v.Snapshot, &snap); err != nil {
		return nil, ReportRecord{}, err
	}
	framing, _, err := s.framingViews(ctx, s.q, r)
	if err != nil {
		return nil, ReportRecord{}, err
	}
	now := s.now().UTC().Truncate(time.Second)
	name := actor.Name
	if strings.TrimSpace(name) == "" {
		name = "usuário do sistema"
	}
	opts := in.options()
	data := buildReportData(snap, v, framing, s.reportLookups(ctx), name, now)
	data.Kind = in.Kind
	if in.Kind == ReportFull {
		rec, err := s.queueFullReport(ctx, actor, id, opts, v, data, name)
		return nil, rec, err
	}

	pdf, pages, err := renderReportPDF(data, opts)
	if err != nil {
		return nil, ReportRecord{}, err
	}
	sum := sha256.Sum256(pdf)
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, ReportRecord{}, err
	}
	var rec ReportRecord
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.InsertProjectReport(ctx, db.InsertProjectReportParams{
			ProjectID: id, Kind: in.Kind, VersionNumber: v.Number, PageNumbers: opts.PageNumbers, CoverSheets: false,
			GeneratedByID: nilIfZero(actor.ID), GeneratedByName: name, GeneratedByCpf: maskedCPF(actor.CPF), GeneratedAt: now,
			PageCount: int32(pages), //nolint:gosec // o relatório tem poucas páginas
			SizeBytes: int64(len(pdf)), Sha256: hex.EncodeToString(sum[:]), Data: raw,
		})
		if err != nil {
			return err
		}
		rec = recordOfRow(row)
		return s.audit.Record(ctx, q, actor.entry("project.report-generate", id,
			map[string]any{"report": row.ID, "kind": in.Kind, "version": v.Number, "pageNumbers": opts.PageNumbers}))
	})
	if err != nil {
		return nil, ReportRecord{}, err
	}
	rec.Data = &data
	return pdf, rec, nil
}

func nilIfZero(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// reportLookups resolve os nomes de município e de tipo de entidade; um código sem cadastro vira vazio.
func (s *Service) reportLookups(ctx context.Context) reportLookups {
	return reportLookups{
		municipality: func(ibge string) string {
			if ibge == "" {
				return ""
			}
			m, err := s.q.GetMunicipality(ctx, ibge)
			if err != nil {
				return ""
			}
			return m.Name
		},
		entityType: func(id string) string {
			u, err := uuid.Parse(id)
			if err != nil {
				return ""
			}
			t, err := s.q.GetEntityType(ctx, u)
			if err != nil {
				return ""
			}
			return t.Name
		},
	}
}

// ListReports devolve o histórico de gerações do projeto, da mais recente para a mais antiga (sem os dados usados).
func (s *Service) ListReports(ctx context.Context, id uuid.UUID, read authz.Decision) ([]ReportRecord, error) {
	if _, err := s.reportProject(ctx, s.q, id, read); err != nil {
		return nil, err
	}
	rows, err := s.q.ListProjectReports(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]ReportRecord, len(rows))
	for i, x := range rows {
		out[i] = recordOfListRow(x)
	}
	return out, nil
}

func (s *Service) loadReport(ctx context.Context, id, rid uuid.UUID, read authz.Decision) (db.ProjectReport, ReportData, error) {
	if _, err := s.reportProject(ctx, s.q, id, read); err != nil {
		return db.ProjectReport{}, ReportData{}, err
	}
	row, err := s.q.GetProjectReport(ctx, db.GetProjectReportParams{ID: rid, ProjectID: id})
	if err != nil {
		return db.ProjectReport{}, ReportData{}, notFound(err)
	}
	var data ReportData
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return db.ProjectReport{}, ReportData{}, err
	}
	return row, data, nil
}

// GetReport devolve uma geração do histórico com os dados que foram usados.
func (s *Service) GetReport(ctx context.Context, id, rid uuid.UUID, read authz.Decision) (ReportRecord, error) {
	row, data, err := s.loadReport(ctx, id, rid, read)
	if err != nil {
		return ReportRecord{}, err
	}
	rec := recordOfRow(row)
	rec.Data = &data
	return rec, nil
}

// ReportDownload é o PDF de uma geração: em memória (resumido, refeito dos dados) ou em fluxo do armazenamento (completo).
type ReportDownload struct {
	Name   string
	PDF    []byte
	Stream io.ReadCloser
	Size   int64
}

// DownloadReport entrega o PDF de uma geração e registra o acesso na auditoria. O resumido é refeito dos dados guardados; o completo
// vem do armazenamento enquanto não vence.
func (s *Service) DownloadReport(ctx context.Context, actor Actor, id, rid uuid.UUID, read authz.Decision) (ReportDownload, error) {
	row, data, err := s.loadReport(ctx, id, rid, read)
	if err != nil {
		return ReportDownload{}, err
	}
	dl := ReportDownload{Name: reportFilename(data)}
	switch {
	case row.Kind == ReportSummary:
		pdf, _, err := renderReportPDF(data, ReportOptions{PageNumbers: row.PageNumbers})
		if err != nil {
			return ReportDownload{}, err
		}
		dl.PDF = pdf
	case row.Status == "queued" || row.Status == "running":
		return ReportDownload{}, httpapi.ErrConflict("report_not_ready", "O relatório completo ainda está sendo montado.")
	case row.Status == "failed":
		return ReportDownload{}, httpapi.ErrConflict("report_failed", "A montagem deste relatório falhou. Gere de novo.")
	case row.ObjectKey == nil || row.ExpiresAt == nil || !row.ExpiresAt.After(time.Now()):
		return ReportDownload{}, httpapi.ErrConflict("report_expired", "O PDF deste relatório completo já venceu. Gere de novo.")
	default:
		if err := s.requireStore(); err != nil {
			return ReportDownload{}, err
		}
		rc, err := s.store.Get(ctx, *row.ObjectKey)
		if errors.Is(err, storage.ErrNotFound) {
			return ReportDownload{}, httpapi.ErrConflict("report_expired", "O PDF deste relatório completo já venceu. Gere de novo.")
		}
		if err != nil {
			return ReportDownload{}, err
		}
		dl.Stream, dl.Size = rc, row.SizeBytes
	}
	if err := s.audit.Record(ctx, s.q, actor.entry("project.report-download", id, map[string]any{"report": rid, "kind": row.Kind})); err != nil {
		if dl.Stream != nil {
			_ = dl.Stream.Close()
		}
		return ReportDownload{}, err
	}
	return dl, nil
}

func reportFilename(d ReportData) string {
	kind := "resumido"
	if d.Kind == ReportFull {
		kind = "completo"
	}
	return fmt.Sprintf("relatorio-%s-%s-%s.pdf", d.Code, kind, d.GeneratedAt.In(cuiaba()).Format("20060102-1504"))
}
