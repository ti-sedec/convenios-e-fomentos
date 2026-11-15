package project

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/pdfbuild"
	"github.com/wagner/convenios-e-fomentos/backend/internal/preview"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// ReportConfig ajusta a montagem do relatório completo (ver config.Config: REPORT_*).
type ReportConfig struct {
	// TmpDir é onde ficam os arquivos temporários da montagem; vazio usa o diretório temporário do sistema. Tem de ser em DISCO.
	TmpDir string
	// MaxInputBytes é a soma máxima dos arquivos que entram no relatório; 0 = sem limite.
	MaxInputBytes int64
	MaxPages      int
	Timeout       time.Duration
	// Retention é por quanto tempo o PDF pronto fica disponível para baixar.
	Retention time.Duration
}

// SetReports liga as opções do relatório completo.
func (s *Service) SetReports(c ReportConfig) {
	if c.Timeout <= 0 {
		c.Timeout = 15 * time.Minute
	}
	if c.Retention <= 0 {
		c.Retention = 24 * time.Hour
	}
	if c.MaxPages <= 0 {
		c.MaxPages = 5000
	}
	s.reportCfg = c
}

func (s *Service) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

// ReportWarning é um arquivo que não entrou no relatório completo.
type ReportWarning struct {
	Document string `json:"document"`
	File     string `json:"file"`
	Reason   string `json:"reason"`
}

// ReportStats são as medições de uma montagem.
type ReportStats struct {
	// PeakRSSBytes é o maior uso de memória residente do PROCESSO da API durante a montagem; BaselineRSSBytes, o uso no início.
	PeakRSSBytes     int64 `json:"peakRssBytes"`
	BaselineRSSBytes int64 `json:"baselineRssBytes"`
	PeakHeapBytes    int64 `json:"peakHeapBytes"`
	DurationMs       int64 `json:"durationMs"`
	Files            int   `json:"files"`
	InputBytes       int64 `json:"inputBytes"`
	OutputBytes      int64 `json:"outputBytes"`
	Pages            int   `json:"pages"`
}

// ---- Medidor de memória ----

// rssBytes lê a memória residente do processo (Linux). Fora do Linux devolve 0.
func rssBytes() int64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			kb, _ := strconv.ParseInt(strings.Fields(v)[0], 10, 64)
			return kb << 10
		}
	}
	return 0
}

type memSampler struct {
	stop       chan struct{}
	done       chan struct{}
	baseline   int64
	peakRSS    atomic.Int64
	peakHeap   atomic.Int64
	sampleOnce sync.Once
}

func startMemSampler() *memSampler {
	m := &memSampler{stop: make(chan struct{}), done: make(chan struct{}), baseline: rssBytes()}
	m.peakRSS.Store(m.baseline)
	go func() {
		defer close(m.done)
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		var ms runtime.MemStats
		for i := 0; ; i++ {
			select {
			case <-m.stop:
				return
			case <-t.C:
			}
			if r := rssBytes(); r > m.peakRSS.Load() {
				m.peakRSS.Store(r)
			}
			if i%5 == 0 {
				runtime.ReadMemStats(&ms)
				if h := int64(ms.HeapInuse); h > m.peakHeap.Load() { //nolint:gosec // bytes de heap cabem em int64
					m.peakHeap.Store(h)
				}
			}
		}
	}()
	return m
}

func (m *memSampler) finish(st *ReportStats) {
	m.sampleOnce.Do(func() { close(m.stop); <-m.done })
	// Amostra final: uma montagem curta termina antes da primeira amostra do intervalo.
	if r := rssBytes(); r > m.peakRSS.Load() {
		m.peakRSS.Store(r)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if h := int64(ms.HeapInuse); h > m.peakHeap.Load() { //nolint:gosec // bytes de heap cabem em int64
		m.peakHeap.Store(h)
	}
	st.PeakRSSBytes, st.BaselineRSSBytes, st.PeakHeapBytes = m.peakRSS.Load(), m.baseline, m.peakHeap.Load()
}

// ---- Orçamento de memória ----

// A montagem usa cerca de 2,2 vezes o tamanho dos arquivos em memória (medido: 185 MB de arquivos deram pico de 440 MiB; 275 MB, 539
// MiB) e, dentro do contêiner, os temporários em disco e o cache também contam. Com 512 MB a API foi MORTA a partir de ~150 MB de
// arquivos (125 MB passou). O fator 3 e a reserva abaixo dão uma folga: com 512 MB o limite efetivo fica em ~108 MB.
const (
	reportMemFactor  = 3
	reportMemReserve = 60 << 20 // o que a API usa em repouso, com folga
	reportMemShare   = 0.75     // fração da memória do contêiner que a montagem pode usar
)

// inputBudget devolve o maior total de arquivos que a montagem aceita para uma memória de memMax bytes; 0 = sem limite de memória.
func inputBudget(memMax int64) int64 {
	if memMax <= 0 {
		return 0
	}
	b := (int64(float64(memMax)*reportMemShare) - reportMemReserve) / reportMemFactor
	return max(b, 0)
}

// cgroupMemoryMax lê o limite de memória do contêiner (cgroup v2 e v1); 0 se não há limite ou não foi possível ler.
func cgroupMemoryMax() int64 {
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		b, err := os.ReadFile(p) //nolint:gosec // caminhos fixos do cgroup
		if err != nil {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || v <= 0 || v >= 1<<50 { // "max" (sem limite) não é número; o v1 sem limite devolve um valor enorme
			return 0
		}
		return v
	}
	return 0
}

// effectiveInputLimit é o limite de arquivos do relatório completo: o menor entre o configurado e o que cabe na memória.
func (s *Service) effectiveInputLimit() (limit int64, byMemory bool) {
	limit = s.reportCfg.MaxInputBytes
	if mem := inputBudget(cgroupMemoryMax()); mem > 0 && (limit == 0 || mem < limit) {
		return mem, true
	}
	return limit, false
}

// ---- Fila ----

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// queueFullReport coloca o relatório completo na fila. A montagem acontece em segundo plano (RunReportWorker).
func (s *Service) queueFullReport(ctx context.Context, actor Actor, id uuid.UUID, opts ReportOptions, v db.ProjectVersion, data ReportData, name string) (ReportRecord, error) {
	if err := s.requireStore(); err != nil {
		return ReportRecord{}, err
	}
	var bytesIn int64
	files := 0
	for _, d := range data.Documents {
		for _, f := range d.Files {
			bytesIn += f.SizeBytes
			files++
		}
	}
	if limit, byMemory := s.effectiveInputLimit(); limit > 0 && bytesIn > limit {
		why := "Gere o relatório resumido ou peça ao administrador para aumentar o limite (REPORT_MAX_INPUT_MB)."
		if byMemory {
			why = "O limite vem da memória do servidor. Gere o relatório resumido ou peça ao administrador para aumentar a memória da API."
		}
		return ReportRecord{}, &httpapi.APIError{Status: 422, Code: "report_too_large", Message: fmt.Sprintf(
			"Os arquivos do projeto somam %s, acima do limite de %s para o relatório completo. %s", humanSize(bytesIn), humanSize(limit), why)}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ReportRecord{}, err
	}
	var rec ReportRecord
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.InsertQueuedProjectReport(ctx, db.InsertQueuedProjectReportParams{
			ProjectID: id, VersionNumber: v.Number, PageNumbers: opts.PageNumbers, CoverSheets: opts.CoverSheets,
			GeneratedByID: nilIfZero(actor.ID), GeneratedByName: name, GeneratedByCpf: maskedCPF(actor.CPF), GeneratedAt: data.GeneratedAt, Data: raw,
		})
		if err != nil {
			return err
		}
		rec = recordOfRow(row)
		rec.Data = &data
		return s.audit.Record(ctx, q, actor.entry("project.report-generate", id,
			map[string]any{"report": row.ID, "kind": ReportFull, "version": v.Number, "pageNumbers": opts.PageNumbers, "coverSheets": opts.CoverSheets, "files": files, "inputBytes": bytesIn}))
	})
	if isUniqueViolation(err) {
		return ReportRecord{}, httpapi.ErrConflict("report_in_progress", "Já existe um relatório completo sendo montado para este projeto. Aguarde ele terminar.")
	}
	return rec, err
}

// RunReportWorker monta os relatórios completos da fila, um por vez, até o contexto terminar. Várias instâncias da API podem rodá-lo
// ao mesmo tempo: cada montagem é de uma só.
func (s *Service) RunReportWorker(ctx context.Context) {
	if s.store == nil {
		return
	}
	var lastMaintenance time.Time
	for ctx.Err() == nil {
		if time.Since(lastMaintenance) > time.Minute {
			s.reportMaintenance(ctx)
			lastMaintenance = time.Now()
		}
		did, err := s.ProcessNextReport(ctx)
		if err != nil && ctx.Err() == nil {
			s.logger().ErrorContext(ctx, "montagem do relatório completo", "error", err)
		}
		if !did {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

// reportMaintenance encerra as montagens cujo processo morreu e apaga os PDFs que passaram da validade.
func (s *Service) reportMaintenance(ctx context.Context) {
	cutoff := time.Now().Add(-2 * time.Minute)
	stale, err := s.q.FailStaleProjectReports(ctx, &cutoff)
	if err != nil {
		s.logger().ErrorContext(ctx, "encerrando montagens interrompidas", "error", err)
	}
	for _, x := range stale {
		s.logger().WarnContext(ctx, "montagem de relatório interrompida", "report", x.ID, "project", x.ProjectID)
	}
	expired, err := s.q.ListExpiredProjectReports(ctx)
	if err != nil {
		s.logger().ErrorContext(ctx, "listando relatórios vencidos", "error", err)
		return
	}
	for _, x := range expired {
		if x.ObjectKey == nil {
			continue
		}
		if err := s.store.Delete(ctx, *x.ObjectKey); err != nil {
			s.logger().ErrorContext(ctx, "apagando relatório vencido", "report", x.ID, "error", err)
			continue
		}
		_ = s.q.ClearProjectReportObject(ctx, x.ID)
	}
}

// ProcessNextReport pega a próxima montagem da fila e a executa até o fim. Devolve false se a fila estava vazia.
func (s *Service) ProcessNextReport(ctx context.Context) (bool, error) {
	row, err := s.q.ClaimNextProjectReport(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.runFullReport(ctx, row)
	return true, nil
}

type reportProgress struct{ done, total atomic.Int32 }

func (s *Service) runFullReport(ctx context.Context, row db.ProjectReport) {
	// O Go guarda a memória que usou para a próxima alocação; devolve ao sistema ao fim, para a API voltar ao uso de repouso.
	defer debug.FreeOSMemory()
	started := time.Now()
	sampler := startMemSampler()
	prog := &reportProgress{}
	jobCtx, cancel := context.WithTimeout(ctx, s.reportCfg.Timeout)
	defer cancel()

	// Batimento: mostra que a montagem está viva (ver FailStaleProjectReports) e leva o andamento para a tela.
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-t.C:
				_ = s.q.HeartbeatProjectReport(context.WithoutCancel(ctx), db.HeartbeatProjectReportParams{ID: row.ID, ProgressDone: prog.done.Load(), ProgressTotal: prog.total.Load()})
			}
		}
	}()

	res, err := s.safeAssemble(jobCtx, row, prog)
	cancel()
	<-beatDone
	stats := ReportStats{DurationMs: time.Since(started).Milliseconds(), Files: res.files, InputBytes: res.inputBytes, OutputBytes: res.size, Pages: res.pages}
	sampler.finish(&stats)
	fin, finCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finCancel()
	warnings, _ := json.Marshal(append([]ReportWarning{}, res.warnings...))
	statsJSON, _ := json.Marshal(stats)
	if err != nil {
		msg := reportErrorMessage(err, jobCtx.Err(), ctx.Err())
		s.logger().ErrorContext(ctx, "montagem do relatório completo falhou", "report", row.ID, "project", row.ProjectID, "error", err, "peakRssMiB", stats.PeakRSSBytes>>20)
		_ = s.q.FailProjectReport(fin, db.FailProjectReportParams{ID: row.ID, Error: msg, Warnings: warnings, Stats: statsJSON})
		if res.key != "" {
			_ = s.store.Delete(fin, res.key)
		}
		return
	}
	expires := time.Now().Add(s.reportCfg.Retention)
	if err := s.q.FinishProjectReport(fin, db.FinishProjectReportParams{ID: row.ID, PageCount: int32(res.pages), SizeBytes: res.size, //nolint:gosec // limite de páginas pequeno
		Sha256: res.sha, ObjectKey: &res.key, ExpiresAt: &expires, Warnings: warnings, Stats: statsJSON}); err != nil {
		s.logger().ErrorContext(ctx, "gravando o fim da montagem", "report", row.ID, "error", err)
		return
	}
	s.logger().InfoContext(ctx, "relatório completo montado", "report", row.ID, "project", row.ProjectID, "files", stats.Files, "pages", stats.Pages,
		"inputMiB", stats.InputBytes>>20, "outputMiB", stats.OutputBytes>>20, "seconds", stats.DurationMs/1000,
		"baselineRssMiB", stats.BaselineRSSBytes>>20, "peakRssMiB", stats.PeakRSSBytes>>20, "peakHeapMiB", stats.PeakHeapBytes>>20)
}

// safeAssemble roda a montagem sem deixar um pânico (de uma biblioteca ou de um arquivo estranho) derrubar a API.
func (s *Service) safeAssemble(ctx context.Context, row db.ProjectReport, prog *reportProgress) (res fullResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger().ErrorContext(ctx, "pânico na montagem do relatório", "report", row.ID, "panic", fmt.Sprint(r))
			err = fmt.Errorf("pânico na montagem: %v", r)
		}
	}()
	return s.assembleFull(ctx, row, prog)
}

func reportErrorMessage(err, jobErr, parentErr error) string {
	switch {
	case parentErr != nil:
		return "A montagem foi interrompida porque o servidor está reiniciando. Gere o relatório de novo."
	case errors.Is(jobErr, context.DeadlineExceeded):
		return "A montagem demorou mais que o limite de tempo. Gere de novo ou use o relatório resumido."
	}
	var te *tooLargeError
	if errors.As(err, &te) {
		return te.Error()
	}
	return "Não foi possível montar o relatório completo. Tente de novo; se persistir, use o relatório resumido."
}

type tooLargeError struct{ msg string }

func (e *tooLargeError) Error() string { return e.msg }

// ---- Montagem ----

type fullResult struct {
	key        string
	pages      int
	size       int64
	sha        string
	files      int
	inputBytes int64
	warnings   []ReportWarning
}

// fullItem é um arquivo do projeto já preparado como PDF.
type fullItem struct {
	doc   ReportDocument
	file  ReportFile
	pdf   string // PDF pronto (saneado), ou a página de aviso
	cover string // folha de rosto, se pedida
	pages int
	start int // página inicial no PDF final (a folha de rosto, se houver)
}

func (s *Service) reportTmpRoot() string {
	if s.reportCfg.TmpDir != "" {
		return s.reportCfg.TmpDir
	}
	return os.TempDir()
}

func (s *Service) fetchToFile(ctx context.Context, key, path string) error {
	rc, err := s.store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	f, err := os.Create(path) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// reasonOf traduz o erro de um arquivo para o texto da página de aviso.
func reasonOf(err error) string {
	switch {
	case errors.Is(err, pdfbuild.ErrEncrypted):
		return "O arquivo está protegido por senha."
	case errors.Is(err, pdfbuild.ErrUnreadable):
		return "O arquivo está corrompido ou não é um PDF válido."
	case errors.Is(err, pdfbuild.ErrImage):
		return "A imagem não pôde ser lida ou é grande demais."
	case errors.Is(err, errUnsupportedForReport):
		return "Este tipo de arquivo não pode ser incluído no PDF."
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errFileMissing):
		return "O arquivo não foi encontrado no armazenamento."
	}
	var api *httpapi.APIError
	if errors.As(err, &api) || errors.Is(err, preview.ErrUnavailable) {
		return "O documento do Word não pôde ser convertido para PDF."
	}
	return "O arquivo não pôde ser lido."
}

var (
	errUnsupportedForReport = errors.New("tipo não suportado")
	errFileMissing          = errors.New("arquivo ausente")
)

// prepareFile baixa o arquivo e devolve um PDF saneado com o seu conteúdo.
func (s *Service) prepareFile(ctx context.Context, projectID uuid.UUID, fl ReportFile, dir string, n int) (string, int, error) {
	pf, err := s.q.GetProjectFile(ctx, db.GetProjectFileParams{ID: fl.ID, ProjectID: projectID})
	if err != nil {
		return "", 0, err
	}
	src := filepath.Join(dir, fmt.Sprintf("in-%04d", n))
	pdfPath := filepath.Join(dir, fmt.Sprintf("pdf-%04d.pdf", n))
	defer func() { _ = os.Remove(src) }()
	switch {
	case pf.ContentType == "application/pdf":
		if err := s.fetchToFile(ctx, pf.ObjectKey, pdfPath); err != nil {
			return "", 0, mapMissing(err)
		}
	case strings.HasPrefix(pf.ContentType, "image/"):
		if err := s.fetchToFile(ctx, pf.ObjectKey, src); err != nil {
			return "", 0, mapMissing(err)
		}
		if err := pdfbuild.ImageToPDF(src, pdfPath); err != nil {
			return "", 0, err
		}
	case pf.ContentType == docxType:
		key, err := s.convertedKey(ctx, pf.ObjectKey, pf.ID, pf.SizeBytes)
		if err != nil {
			return "", 0, err
		}
		if err := s.fetchToFile(ctx, key, pdfPath); err != nil {
			return "", 0, mapMissing(err)
		}
	default:
		return "", 0, errUnsupportedForReport
	}
	clean := filepath.Join(dir, fmt.Sprintf("san-%04d.pdf", n))
	if _, err := pdfbuild.Sanitize(ctx, pdfPath, clean); err != nil {
		_ = os.Remove(pdfPath)
		return "", 0, err
	}
	_ = os.Remove(pdfPath)
	pages, err := pdfbuild.PageCount(ctx, clean)
	if err != nil {
		_ = os.Remove(clean)
		return "", 0, err
	}
	return clean, pages, nil
}

func mapMissing(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return errFileMissing
	}
	return err
}

func writeBytes(path string, b []byte) error { return os.WriteFile(path, b, 0o600) }

// assembleFull monta o PDF completo em disco e o envia ao armazenamento. Em qualquer falha apaga os arquivos temporários.
func (s *Service) assembleFull(ctx context.Context, row db.ProjectReport, prog *reportProgress) (res fullResult, err error) {
	var d ReportData
	if err := json.Unmarshal(row.Data, &d); err != nil {
		return res, err
	}
	opts := ReportOptions{PageNumbers: row.PageNumbers, CoverSheets: row.CoverSheets}
	if err := os.MkdirAll(s.reportTmpRoot(), 0o700); err != nil {
		return res, err
	}
	dir, err := os.MkdirTemp(s.reportTmpRoot(), "relatorio-*")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	total := 0
	for _, doc := range d.Documents {
		total += len(doc.Files)
	}
	prog.total.Store(int32(total))                                                                                                 //nolint:gosec // no máximo 300 arquivos
	_ = s.q.HeartbeatProjectReport(ctx, db.HeartbeatProjectReportParams{ID: row.ID, ProgressDone: 0, ProgressTotal: int32(total)}) //nolint:gosec // idem

	// 1. Cada arquivo vira um PDF saneado (ou uma página de aviso) e, se pedido, ganha a folha de rosto.
	var items []*fullItem
	n, pagesSoFar := 0, 0
	for _, doc := range d.Documents {
		for _, fl := range doc.Files {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			n++
			res.inputBytes += fl.SizeBytes
			it := &fullItem{doc: doc, file: fl}
			path, pages, perr := s.prepareFile(ctx, row.ProjectID, fl, dir, n)
			if perr != nil {
				if ctx.Err() != nil {
					return res, ctx.Err()
				}
				reason := reasonOf(perr)
				s.logger().WarnContext(ctx, "arquivo não incorporado ao relatório", "report", row.ID, "file", fl.ID, "error", perr)
				res.warnings = append(res.warnings, ReportWarning{Document: doc.Name, File: fl.Name, Reason: reason})
				b, nerr := noticePDF(d, doc, fl, reason)
				if nerr != nil {
					return res, nerr
				}
				path = filepath.Join(dir, fmt.Sprintf("aviso-%04d.pdf", n))
				if err := writeBytes(path, b); err != nil {
					return res, err
				}
				pages = 1
			}
			it.pdf, it.pages = path, pages
			if opts.CoverSheets {
				b, cerr := coverSheetPDF(d, doc, fl, n, total, pages)
				if cerr != nil {
					return res, cerr
				}
				it.cover = filepath.Join(dir, fmt.Sprintf("capa-%04d.pdf", n))
				if err := writeBytes(it.cover, b); err != nil {
					return res, err
				}
				pagesSoFar++
			}
			pagesSoFar += pages
			if pagesSoFar > s.reportCfg.MaxPages {
				return res, &tooLargeError{fmt.Sprintf("O relatório passaria do limite de %d páginas.", s.reportCfg.MaxPages)}
			}
			items = append(items, it)
			prog.done.Store(int32(n)) //nolint:gosec // idem
		}
	}
	res.files = n

	// 2. O relatório (duas passagens: a primeira descobre quantas páginas tem, a segunda grava os números de página do sumário).
	repOpts := ReportOptions{PageNumbers: false} // a numeração, se pedida, é carimbada depois em todas as páginas
	toc := &reportToc{Start: map[uuid.UUID]int{}}
	_, lay, err := renderReport(d, repOpts, toc)
	if err != nil {
		return res, err
	}
	cur := lay.Pages + 1
	for _, it := range items {
		it.start = cur
		toc.Start[it.file.ID] = cur
		if it.cover != "" {
			cur++
		}
		cur += it.pages
	}
	totalPages := cur - 1
	repBytes, lay2, err := renderReport(d, repOpts, toc)
	if err != nil {
		return res, err
	}
	if lay2.Pages != lay.Pages {
		return res, fmt.Errorf("o relatório mudou de tamanho entre as passagens (%d e %d páginas)", lay.Pages, lay2.Pages)
	}
	repPath := filepath.Join(dir, "relatorio.pdf")
	if err := writeBytes(repPath, repBytes); err != nil {
		return res, err
	}
	repBytes = nil //nolint:ineffassign,wastedassign // solta a referência antes da junção (o PDF já está em disco)

	// 3. Junta.
	inputs := []string{repPath}
	for _, it := range items {
		if it.cover != "" {
			inputs = append(inputs, it.cover)
		}
		inputs = append(inputs, it.pdf)
	}
	merged := filepath.Join(dir, "junto.pdf")
	if err := pdfbuild.Merge(ctx, inputs, merged); err != nil {
		return res, err
	}
	for _, p := range inputs {
		_ = os.Remove(p)
	}

	// 4. Bookmarks, links e numeração.
	cur2 := merged
	step := func(name string, fn func(in, out string) error) error {
		out := filepath.Join(dir, name)
		if err := fn(cur2, out); err != nil {
			return err
		}
		_ = os.Remove(cur2)
		cur2 = out
		return nil
	}
	if err := step("com-bookmarks.pdf", func(in, out string) error {
		return pdfbuild.WriteOutline(ctx, in, out, reportBookmarks(d, lay2, items))
	}); err != nil {
		return res, err
	}
	links := make([]pdfbuild.Link, 0, len(lay2.Links))
	for _, l := range lay2.Links {
		links = append(links, pdfbuild.Link{Page: l.Page, X: l.X, Y: l.Y, W: l.W, H: l.H, Target: toc.Start[l.File]})
	}
	if err := step("com-links.pdf", func(in, out string) error { return pdfbuild.AddLinks(ctx, in, out, links) }); err != nil {
		return res, err
	}
	if opts.PageNumbers {
		if err := step("numerado.pdf", func(in, out string) error { return pdfbuild.StampPageNumbers(ctx, in, out) }); err != nil {
			return res, err
		}
	}

	// 5. Conferência final: sem conteúdo ativo e com o número de páginas esperado.
	active, err := pdfbuild.ActiveContent(ctx, cur2)
	if err != nil {
		return res, err
	}
	if len(active) > 0 {
		return res, fmt.Errorf("o PDF final ainda tem conteúdo ativo: %v", active)
	}
	got, err := pdfbuild.PageCount(ctx, cur2)
	if err != nil {
		return res, err
	}
	if got != totalPages {
		return res, fmt.Errorf("o PDF final tem %d páginas, esperava %d", got, totalPages)
	}
	res.pages = got

	// 6. Envia ao armazenamento (em fluxo, sem carregar o arquivo na memória) e calcula o hash.
	f, err := os.Open(cur2) //nolint:gosec // arquivo montado pelo servidor
	if err != nil {
		return res, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return res, err
	}
	res.size, res.sha = size, hex.EncodeToString(h.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return res, err
	}
	res.key = fmt.Sprintf("reports/%s/%s.pdf", row.ProjectID, row.ID)
	if err := s.store.PutObjectStream(ctx, res.key, "application/pdf", f, size); err != nil {
		return res, err
	}
	return res, nil
}

// reportBookmarks monta a árvore: o relatório (com as suas seções) e os documentos (com os arquivos quando há mais de um).
func reportBookmarks(d ReportData, lay reportLayout, items []*fullItem) []pdfbuild.Bookmark {
	trunc := func(s string) string {
		r := []rune(s)
		if len(r) > 150 {
			return string(r[:150]) + "…"
		}
		return s
	}
	kids := []pdfbuild.Bookmark{{Title: "Identificação", Page: 1}}
	for _, sec := range d.Sections {
		if p := lay.Sections[sec.Title]; p > 0 {
			kids = append(kids, pdfbuild.Bookmark{Title: sec.Title, Page: p})
		}
	}
	if p := lay.Sections["Documentos"]; p > 0 {
		kids = append(kids, pdfbuild.Bookmark{Title: "Sumário dos documentos", Page: p})
	}
	if p := lay.Sections["Declaração de veracidade"]; p > 0 {
		kids = append(kids, pdfbuild.Bookmark{Title: "Declaração de veracidade", Page: p})
	}
	out := []pdfbuild.Bookmark{{Title: "Relatório do projeto", Page: 1, Kids: kids}}

	var docs []pdfbuild.Bookmark
	for i := 0; i < len(items); {
		j := i
		for j < len(items) && items[j].doc.Name == items[i].doc.Name && items[j].doc.AmendmentLabel == items[i].doc.AmendmentLabel && items[j].doc.Group == items[i].doc.Group {
			j++
		}
		doc := items[i].doc
		title := doc.Name
		if doc.AmendmentLabel != "" {
			title += " (" + doc.AmendmentLabel + ")"
		}
		if doc.Group != "" {
			title = doc.Group + " › " + title
		}
		bm := pdfbuild.Bookmark{Title: trunc(title), Page: items[i].start}
		if j-i > 1 {
			for _, it := range items[i:j] {
				bm.Kids = append(bm.Kids, pdfbuild.Bookmark{Title: trunc(it.file.Name), Page: it.start})
			}
		}
		docs = append(docs, bm)
		i = j
	}
	if len(docs) > 0 {
		out = append(out, pdfbuild.Bookmark{Title: "Documentos", Page: docs[0].Page, Kids: docs})
	}
	return out
}

// RunReportMaintenanceForTest roda agora a manutenção da fila (montagens interrompidas e PDFs vencidos).
func (s *Service) RunReportMaintenanceForTest(ctx context.Context) { s.reportMaintenance(ctx) }
