package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/pdfbuild"
)

// validationCode é um erro 422 com código próprio (o ErrValidation padrão traz só o código genérico).
func validationCode(code, msg string) *httpapi.APIError {
	return &httpapi.APIError{Status: 422, Code: code, Message: msg}
}

// reportData monta os dados do relatório (sem gerar PDF) a partir do retrato da última versão enviada: a lista de documentos, na
// ordem do checklist e depois os adicionais, é a mesma do relatório completo.
func (r *openRun) reportData(ctx context.Context) (ReportData, error) {
	s := r.svc
	cur, err := s.q.GetProject(ctx, r.projectID)
	if err != nil {
		return ReportData{}, err
	}
	r.institution = cur.InstitutionName
	v, err := s.q.GetLatestProjectVersion(ctx, r.projectID)
	if err != nil {
		return ReportData{}, err
	}
	var snap VersionSnapshot
	if err := json.Unmarshal(v.Snapshot, &snap); err != nil {
		return ReportData{}, err
	}
	framing, _, err := s.framingViews(ctx, s.q, cur)
	if err != nil {
		return ReportData{}, err
	}
	name := r.actor.Name
	if strings.TrimSpace(name) == "" {
		name = "usuário do sistema"
	}
	return buildReportData(snap, v, framing, s.reportLookups(ctx), name, s.now().UTC()), nil
}

// reportContent gera o relatório resumido do projeto (grava o registro da geração, como qualquer geração) e devolve o PDF.
func (r *openRun) reportContent() ([]byte, string, error) {
	ctx := context.WithoutCancel(r.ctxForReport())
	pdf, rec, err := r.svc.GenerateReport(ctx, r.actor, r.projectID, ReportInput{Kind: ReportSummary}, r.read)
	if err != nil {
		return nil, "", err
	}
	name := "relatorio.pdf"
	if rec.Data != nil {
		name = reportFilename(*rec.Data)
	}
	return pdf, name, nil
}

func (r *openRun) ctxForReport() context.Context { return context.Background() }

// docDescription é a descrição enviada ao SIGADOC: «<Tipo do documento> — <proponente ou emenda> — <código do projeto>», com o arquivo
// e a parte quando há mais de um. É também a chave com que um documento já criado é reconhecido numa retomada.
func docDescription(doc ReportDocument, institution, code string, fileIdx, fileCount, part, parts int) string {
	who := strings.TrimSpace(doc.AmendmentLabel)
	if who == "" {
		who = institution
	}
	d := fmt.Sprintf("%s — %s — %s", doc.Name, who, code)
	if fileCount > 1 {
		d += fmt.Sprintf(" (arquivo %d de %d)", fileIdx, fileCount)
	}
	if parts > 1 {
		d += fmt.Sprintf(" (parte %d de %d)", part, parts)
	}
	return d
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// processFile insere um arquivo do proponente no processo: converte para PDF (Word e imagem), divide em partes de até MaxPartMB e cria,
// assina e junta cada parte como documento capturado. Um arquivo que não pode ser preparado vira *itemFailure (não impede os outros).
func (r *openRun) processFile(ctx context.Context, data ReportData, doc ReportDocument, fl ReportFile, fileIdx, fileCount, seq int) error {
	s := r.svc
	baseKey := "file:" + fl.ID.String()
	label := doc.Name + ": " + fl.Name
	fileID := fl.ID

	first := docDescription(doc, r.institution, data.Code, fileIdx, fileCount, 1, 1)
	if err := s.q.UpsertSigadocItem(ctx, db.UpsertSigadocItemParams{ProjectID: r.projectID, Key: baseKey + ":1", Seq: int32(seq), Kind: itemDocument, //nolint:gosec // poucos itens
		Label: label, Description: first, FileID: &fileID, Part: 1, Parts: 1, Payload: []byte("{}")}); err != nil {
		return err
	}
	rowsOf := func() ([]db.SigadocItem, error) {
		all, err := s.q.ListSigadocItems(ctx, r.projectID)
		if err != nil {
			return nil, err
		}
		var out []db.SigadocItem
		for _, it := range all {
			if strings.HasPrefix(it.Key, baseKey+":") {
				out = append(out, it)
			}
		}
		return out, nil
	}
	rows, err := rowsOf()
	if err != nil {
		return err
	}
	allDone := len(rows) > 0
	for _, it := range rows {
		if it.Status != itemJoined {
			allDone = false
		}
	}
	if allDone {
		return nil
	}
	if err := r.active(ctx); err != nil {
		return err
	}
	if r.tmp == "" {
		return errors.New("sem diretório temporário para preparar o arquivo")
	}
	dir := filepath.Join(r.tmp, fmt.Sprintf("f%04d", seq))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	fail := func(err error) error {
		msg := itemErrorText(err)
		_ = s.q.SetSigadocItemState(ctx, db.SetSigadocItemStateParams{ProjectID: r.projectID, Key: rows[0].Key, Status: itemFailed, Error: truncate(msg, 300)})
		return &itemFailure{msg: label + ": " + msg}
	}
	pdfPath, _, err := s.prepareFile(ctx, r.projectID, fl, dir, seq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail(err)
	}
	sum, err := fileSHA(pdfPath)
	if err != nil {
		return err
	}
	parts, err := pdfbuild.Split(ctx, pdfPath, dir, int64(r.settings.MaxPartMB)<<20)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail(err)
	}
	// Os itens das partes: a primeira já existe; as outras nascem agora. A descrição passa a dizer "parte n de N".
	for i := 1; i <= len(parts); i++ {
		desc := docDescription(doc, r.institution, data.Code, fileIdx, fileCount, i, len(parts))
		key := fmt.Sprintf("%s:%d", baseKey, i)
		if i > 1 {
			if err := s.q.UpsertSigadocItem(ctx, db.UpsertSigadocItemParams{ProjectID: r.projectID, Key: key, Seq: int32(seq + i - 1), Kind: itemDocument, //nolint:gosec // poucos itens
				Label: label, Description: desc, FileID: &fileID, Part: int32(i), Parts: int32(len(parts)), Sha256: sum, Payload: []byte("{}")}); err != nil { //nolint:gosec // poucas partes
				return err
			}
		}
		// Só reescreve a descrição de um item que ainda não foi criado no SIGADOC (a descrição é a chave de reconhecimento).
		it, err := s.q.ListSigadocItems(ctx, r.projectID)
		if err != nil {
			return err
		}
		for _, x := range it {
			if x.Key == key && x.Status == itemPending {
				if err := s.q.SetSigadocItemParts(ctx, db.SetSigadocItemPartsParams{ProjectID: r.projectID, Key: key, Parts: int32(len(parts)), Sha256: sum, Description: desc}); err != nil { //nolint:gosec // poucas partes
					return err
				}
			}
		}
	}
	rows, err = rowsOf()
	if err != nil {
		return err
	}
	for i, row := range rows {
		if i >= len(parts) {
			break
		}
		path, name := parts[i], filepath.Base(fl.Name)
		if len(parts) > 1 {
			name = fmt.Sprintf("%s-parte-%d.pdf", strings.TrimSuffix(name, filepath.Ext(name)), i+1)
		} else if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ".pdf"
		}
		content := func() ([]byte, string, error) {
			b, err := os.ReadFile(path) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
			return b, name, err
		}
		if err := r.advance(ctx, row, content); err != nil {
			return err
		}
	}
	return nil
}
