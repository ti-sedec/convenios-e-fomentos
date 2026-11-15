package project

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cnpj"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Tipos de relatório. Só o resumido existe nesta fase; o completo (com os documentos juntados) vem depois.
const (
	ReportSummary = "summary"
	ReportFull    = "full"

	// reportSchema versiona o formato de ReportData guardado no histórico. Se ele mudar, o renderizador precisa continuar lendo
	// os registros antigos.
	reportSchema = 1
)

// ReportAccess diz se o relatório do projeto pode ser gerado agora e, se não, por quê (a tela mostra o motivo).
type ReportAccess struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// reportAccess aplica a regra: projeto ativo com o enquadramento confirmado. Rascunho não tem relatório (nil).
func reportAccess(r db.Project) *ReportAccess {
	switch {
	case r.Status == StatusDraft:
		return nil
	case r.Status == StatusArchived:
		return &ReportAccess{Reason: "O projeto está arquivado: o relatório só é gerado para projetos ativos."}
	case r.FramingStatus == nil || *r.FramingStatus != FramingConfirmed:
		return &ReportAccess{Reason: "O relatório fica disponível depois que o Revisor do Departamento confirma o enquadramento."}
	}
	return &ReportAccess{Available: true}
}

// ReportField é uma linha "rótulo: valor" já pronta para imprimir.
type ReportField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ReportSection é um bloco do relatório (Proponente, Instituição...).
type ReportSection struct {
	Key    string        `json:"key"`
	Title  string        `json:"title"`
	Fields []ReportField `json:"fields"`
}

// ReportFile é um arquivo de documento: só os metadados (o conteúdo nunca entra no relatório resumido).
type ReportFile struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	ContentType string     `json:"contentType"`
	SizeBytes   int64      `json:"sizeBytes"`
	SHA256      string     `json:"sha256"`
	UploadedAt  *time.Time `json:"uploadedAt,omitempty"`
}

// ReportDocument é um item do checklist (ou documento adicional) com a situação na versão enviada.
type ReportDocument struct {
	Group          string       `json:"group"`
	Name           string       `json:"name"`
	AmendmentLabel string       `json:"amendmentLabel"`
	Status         string       `json:"status"` // texto pronto: Entregue, Justificado, Não enviado
	Justification  string       `json:"justification"`
	Extra          bool         `json:"extra"`
	Files          []ReportFile `json:"files"`
}

// ReportData é tudo o que alimenta o relatório. É gravado no histórico exatamente como foi usado, sem os documentos anexados
// (só os metadados dos arquivos), e o PDF é refeito a partir dele. Os CPFs vão mascarados.
type ReportData struct {
	Schema        int        `json:"schema"`
	Kind          string     `json:"kind"`
	Code          string     `json:"code"`
	Instrument    string     `json:"instrument"`
	VersionNumber int32      `json:"versionNumber"`
	SubmittedAt   *time.Time `json:"submittedAt"`
	GeneratedAt   time.Time  `json:"generatedAt"`
	GeneratedBy   string     `json:"generatedBy"`
	// Notice é o aviso de envio fora do prazo, quando há.
	Notice      string           `json:"notice"`
	Identity    []ReportField    `json:"identity"`
	Sections    []ReportSection  `json:"sections"`
	Documents   []ReportDocument `json:"documents"`
	Declaration Declaration      `json:"declaration"`
}

// ReportOptions são as escolhas de quem gera.
type ReportOptions struct {
	PageNumbers bool `json:"pageNumbers"`
	CoverSheets bool `json:"coverSheets"`
}

// ---- Montagem dos dados ----

type fieldList struct{ f []ReportField }

// add acrescenta a linha, ignorando as de valor vazio.
func (l *fieldList) add(label, value string) {
	if strings.TrimSpace(value) != "" {
		l.f = append(l.f, ReportField{Label: label, Value: strings.TrimSpace(value)})
	}
}

func yesNo(b bool) string {
	if b {
		return "Sim"
	}
	return "Não"
}

func maskedCPF(s string) string {
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "*"):
		return s
	}
	return cpf.Mask(s)
}

func phoneText(p Phone) string {
	n := p.Number
	kind := map[string]string{PhoneMobile: "celular", PhoneLandline: "fixo"}[p.Type]
	switch len(n) {
	case 11:
		n = fmt.Sprintf("(%s) %s-%s", n[:2], n[2:7], n[7:])
	case 10:
		n = fmt.Sprintf("(%s) %s-%s", n[:2], n[2:6], n[6:])
	}
	if kind != "" {
		return n + " (" + kind + ")"
	}
	return n
}

func phonesText(ps []Phone) string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, phoneText(p))
	}
	return strings.Join(out, "; ")
}

func emailsText(es []Email) string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		t := e.Address
		if e.Primary {
			t += " (principal)"
		}
		out = append(out, t)
	}
	return strings.Join(out, "; ")
}

func dateOrEmpty(iso string) string {
	if iso == "" {
		return ""
	}
	return brDate(iso)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// reportLookups são as buscas por nome feitas na geração (o retrato guarda só códigos).
type reportLookups struct {
	municipality func(ibge string) string
	entityType   func(id string) string
}

// buildReportData monta os dados a partir do retrato imutável da versão enviada. Nada aqui lê o cadastro vivo (D3).
func buildReportData(snap VersionSnapshot, version db.ProjectVersion, framing *Framing, lk reportLookups, generatedBy string, at time.Time) ReportData {
	p := snap.Project
	d := ReportData{
		Schema: reportSchema, Kind: ReportSummary, Code: p.Code,
		Instrument:    map[string]string{InstrumentConvenio: "Convênio", InstrumentFomento: "Termo de Fomento"}[p.Institution.Instrument],
		VersionNumber: version.Number, SubmittedAt: &version.SubmittedAt, GeneratedAt: at, GeneratedBy: generatedBy, Declaration: snap.Declaration,
	}
	d.Declaration.ByCPF = maskedCPF(d.Declaration.ByCPF)
	if p.LateSubmission {
		d.Notice = "Enviado depois da data-limite de protocolo antes da vedação eleitoral: o projeto segue apenas com atos preparatórios e preliminares; o repasse fica para depois do período de restrição."
	}

	id := fieldList{}
	id.add("Instrumento", d.Instrument)
	id.add("Nº no SIGCon", p.Proposal.SigconNumber)
	id.add("Versão do projeto", fmt.Sprintf("%d (enviada em %s)", version.Number, version.SubmittedAt.In(cuiaba()).Format("02/01/2006 15:04")))
	if framing != nil && framing.Decision == FramingConfirmed {
		t := "Confirmado no departamento " + framing.Department.Name
		if framing.DecidedAt != nil {
			t += " em " + framing.DecidedAt.In(cuiaba()).Format("02/01/2006")
		}
		if framing.DecidedBy != "" {
			t += " (" + framing.DecidedBy + ")"
		}
		id.add("Enquadramento", t)
	}
	d.Identity = id.f

	// Proponente
	pr := fieldList{}
	pp := p.Proponent
	pr.add("Nome", pp.Name)
	pr.add("CPF", maskedCPF(pp.CPF))
	pr.add("Cargo", pp.JobTitle)
	loc := lk.municipality(pp.MunicipalityIBGE)
	if loc != "" && pp.UF != "" {
		loc += "/" + pp.UF
	}
	pr.add("Município", loc)
	pr.add("Telefones", phonesText(pp.Phones))
	pr.add("E-mails", emailsText(pp.Emails))
	if pp.IsLegalRepresentative != nil {
		pr.add("É o representante legal", yesNo(*pp.IsLegalRepresentative))
		if !*pp.IsLegalRepresentative {
			pr.add("Representante legal", pp.LegalRepName)
			pr.add("CPF do representante", maskedCPF(pp.LegalRepCPF))
			pr.add("Cargo do representante", pp.LegalRepJobTitle)
		}
	}
	d.Sections = append(d.Sections, ReportSection{Key: "proponent", Title: "Proponente", Fields: pr.f})

	// Instituição
	in := fieldList{}
	ins := p.Institution
	in.add("Nome", ins.Name)
	in.add("CNPJ", cnpj.Format(ins.CNPJ))
	in.add("Tipo de entidade", lk.entityType(ins.EntityTypeID))
	if ins.EntityTypeIsOther {
		in.add("Tipo informado", ins.EntityTypeOther)
	}
	in.add("Natureza", map[string]string{"public": "Pública", "private": "Privada"}[ins.Nature])
	in.add("Sem fins lucrativos", yesNo(ins.NonProfit))
	addr := strings.TrimSpace(strings.Join(nonEmpty(ins.Street, ins.Number, ins.Complement, ins.District), ", "))
	if ins.Zip != "" {
		addr += " — CEP " + ins.Zip
	}
	in.add("Endereço", addr)
	im := lk.municipality(ins.MunicipalityIBGE)
	if im != "" && ins.UF != "" {
		im += "/" + ins.UF
	}
	in.add("Município", im)
	in.add("E-mail", ins.Email)
	in.add("Telefone", ins.Phone)
	in.add("Certidão de Habilitação", map[string]string{HabilitationFull: "Plena", HabilitationPartial: "Parcial"}[ins.HabilitationStatus])
	in.add("Nº da certidão", ins.HabilitationNumber)
	in.add("Válida até", dateOrEmpty(ins.HabilitationValidUntil))
	d.Sections = append(d.Sections, ReportSection{Key: "institution", Title: "Instituição", Fields: in.f})

	// Proposta
	pf := fieldList{}
	pro := p.Proposal
	pf.add("Título", pro.Title)
	pf.add("Nº no SIGCon", pro.SigconNumber)
	pf.add("Registrado no SIGCon em", dateOrEmpty(pro.SigconRegisteredOn))
	pf.add("Objeto", pro.Object)
	pf.add("Justificativa", pro.Justification)
	pf.add("Natureza do objeto", map[string]string{"works": "Obra ou serviço de engenharia", "goods": "Aquisição de bens", "services": "Contratação de serviços", "event": "Evento", "other": "Outro"}[pro.ObjectNature])
	pf.add("Contratação artística", yesNo(pro.HasArtisticHiring))
	pf.add("Estado de calamidade", yesNo(pro.Calamity))
	if pro.StartDate != "" || pro.EndDate != "" {
		pf.add("Vigência prevista", orDash(dateOrEmpty(pro.StartDate))+" a "+orDash(dateOrEmpty(pro.EndDate)))
	}
	label := "Execução em"
	for _, t := range executionPeriodsText(pro.ExecutionPeriods) {
		pf.add(label, t)
		label = ""
	}
	d.Sections = append(d.Sections, ReportSection{Key: "proposal", Title: "Proposta", Fields: pf.f})

	// Recursos
	fu := fieldList{}
	f := p.Funding
	fu.add("Tipo de recurso", map[string]string{FundingAmendment: "Emenda parlamentar", FundingOwn: "Recurso próprio da SEDEC", FundingMixed: "Emenda parlamentar e recurso próprio da SEDEC"}[f.Type])
	fu.add("Recurso próprio", brlIf(f.OwnAmountCents))
	label = "Emendas"
	for _, a := range f.Amendments {
		fu.add(label, fmt.Sprintf("%s — emenda %s/%d — %s", a.ParliamentarianName, a.Number, a.Year, brl(a.AmountCents)))
		label = ""
	}
	fu.add("Total das emendas", brlIf(f.AmendmentTotalCents))
	fu.add("Total solicitado", brl(f.TotalCents))
	fu.add("Contrapartida", brlIf(f.CounterpartCents))
	fu.add("Tipo da contrapartida", map[string]string{CounterpartFinancial: "Financeira", CounterpartInKind: "Em bens e serviços"}[f.CounterpartKind])
	fu.add("Descrição da contrapartida", f.CounterpartDescription)
	fu.add("Custos indiretos", brlIf(f.IndirectCostsCents))
	fu.add("Chamamento público", map[string]string{TenderHeld: "Realizado", TenderWaived: "Dispensado", TenderUnfeasible: "Inexigível", TenderNotApplicable: "Não se aplica"}[f.TenderStatus])
	fu.add("Edital nº", f.TenderNoticeNumber)
	fu.add("Edital aprovado em", dateOrEmpty(f.TenderApprovedOn))
	fu.add("Fundamento legal", f.TenderLegalBasis)
	fu.add("Justificativa do chamamento", f.TenderJustification)
	fu.add("Cachês de artistas", brlIf(f.ArtistFeesCents))
	fu.add("Cachês de artistas regionais", brlIf(f.RegionalArtistFeesCents))
	if f.GovernorWaiver {
		fu.add("Dispensa do Governador", "Sim")
	}
	fu.add("Pesquisa de preços mais antiga", dateOrEmpty(f.PriceOldestCollectedOn))
	d.Sections = append(d.Sections, ReportSection{Key: "funding", Title: "Recursos", Fields: fu.f})

	// Documentos: só os metadados dos arquivos.
	for _, it := range snap.Documents.Items {
		doc := ReportDocument{Group: it.Group, Name: it.Name, AmendmentLabel: it.AmendmentLabel, Justification: it.Justification}
		switch it.Status {
		case ItemDone:
			doc.Status = "Entregue"
		case ItemJustified:
			doc.Status = "Justificado (não enviado)"
		default:
			doc.Status = "Não enviado"
		}
		for _, fl := range it.Files {
			if fl.Status == FileConfirmed {
				at := fl.CreatedAt
				doc.Files = append(doc.Files, ReportFile{ID: fl.ID, Name: fl.Filename, ContentType: fl.ContentType, SizeBytes: fl.SizeBytes, SHA256: fl.SHA256, UploadedAt: &at})
			}
		}
		d.Documents = append(d.Documents, doc)
	}
	for _, x := range snap.Documents.ExtraDocuments {
		doc := ReportDocument{Group: "Documentos adicionais", Name: x.Title, Status: "Entregue", Extra: true}
		if x.File == nil || x.File.Status != FileConfirmed {
			continue
		}
		at := x.File.CreatedAt
		doc.Files = []ReportFile{{ID: x.File.ID, Name: x.File.Filename, ContentType: x.File.ContentType, SizeBytes: x.File.SizeBytes, SHA256: x.File.SHA256, UploadedAt: &at}}
		d.Documents = append(d.Documents, doc)
	}
	return d
}

func nonEmpty(ss ...string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// ---- Renderização ----

func reportTitle(kind string) string {
	if kind == ReportFull {
		return "Relatório completo"
	}
	return "Relatório resumido"
}

const mmToPt = 72.0 / 25.4

// reportLink é uma área clicável do sumário do relatório completo, em pontos (origem no canto inferior esquerdo da página).
type reportLink struct {
	Page       int
	X, Y, W, H float64
	File       uuid.UUID
}

// reportToc liga a renderização ao relatório completo: a página inicial de cada arquivo no PDF final. Nulo no resumido.
type reportToc struct {
	Start map[uuid.UUID]int
}

// reportLayout é o que a renderização devolve além do PDF: onde cada seção começou (para os bookmarks) e os links do sumário.
type reportLayout struct {
	Pages    int
	Sections map[string]int
	Links    []reportLink
}

// renderReportPDF gera o relatório resumido. É uma função pura dos dados e das opções: com a data de criação fixa em GeneratedAt,
// o mesmo registro sempre produz os mesmos bytes.
func renderReportPDF(d ReportData, opts ReportOptions) ([]byte, int, error) {
	pdf, lay, err := renderReport(d, opts, nil)
	return pdf, lay.Pages, err
}

// fit corta o texto com reticências para caber na largura (mm) com a fonte atual.
func fitText(pdf *fpdf.Fpdf, text string, width float64) string {
	if pdf.GetStringWidth(text) <= width {
		return text
	}
	for len(text) > 1 && pdf.GetStringWidth(text+"...") > width {
		r := []rune(text)
		text = string(r[:len(r)-1])
	}
	return text + "..."
}

// renderReport gera o PDF (A4). Com toc nulo é o resumido; com toc, a seção de documentos vira o sumário do completo: cada arquivo
// com o número da página onde começa e um link para ela.
func renderReport(d ReportData, opts ReportOptions, toc *reportToc) ([]byte, reportLayout, error) {
	lay := reportLayout{Sections: map[string]int{}}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false) // texto legível no arquivo: facilita a conferência e os testes
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(d.GeneratedAt)
	pdf.SetModificationDate(d.GeneratedAt)
	title := reportTitle(d.Kind) + " do projeto " + d.Code
	pdf.SetTitle(title, true)
	pdf.SetAuthor("SEDEC/MT", true)
	pdf.SetMargins(18, 16, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.AliasNbPages("{nb}")
	tr := pdf.UnicodeTranslatorFromDescriptor("") // as fontes padrão do PDF usam Latin-1
	pw, ph := pdf.GetPageSize()

	generated := d.GeneratedAt.In(cuiaba()).Format("02/01/2006 15:04")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(90, 90, 90)
		left := fmt.Sprintf("Projeto %s — %s — gerado em %s (Cuiabá)", d.Code, reportTitle(d.Kind), generated)
		w := 0.0
		if opts.PageNumbers {
			w = 130
		}
		pdf.CellFormat(w, 5, tr(left), "", 0, "L", false, 0, "")
		if opts.PageNumbers {
			pdf.CellFormat(0, 5, tr(fmt.Sprintf("Página %d de {nb}", pdf.PageNo())), "", 0, "R", false, 0, "")
		}
		pdf.SetTextColor(0, 0, 0)
	})
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 15)
	pdf.CellFormat(0, 8, tr(reportTitle(d.Kind)+" do projeto"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, tr("Secretaria de Estado de Desenvolvimento Econômico de Mato Grosso (SEDEC/MT)"), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(3)

	pdf.SetFillColor(240, 240, 240)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 8, tr("  Código do projeto: "+d.Code), "1", 1, "L", true, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, tr(fmt.Sprintf("  Gerado em %s (horário de Cuiabá) por %s", generated, d.GeneratedBy)), "LRB", 1, "L", false, 0, "")
	if d.Notice != "" {
		pdf.SetTextColor(150, 60, 0)
		pdf.MultiCell(0, 5, tr(d.Notice), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(3)

	section := func(t string) {
		if pdf.GetY() > 255 { // evita o título sozinho no pé da página
			pdf.AddPage()
		}
		lay.Sections[t] = pdf.PageNo()
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(0, 6, tr(t), "B", 1, "L", false, 0, "")
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
	}
	line := func(label, value string) {
		// Mede antes de desenhar: se a linha não cabe no que resta da página, vai inteira para a próxima (senão o rótulo ficava numa
		// página e o valor na outra).
		pdf.SetFont("Helvetica", "B", 10)
		lines := len(pdf.SplitLines([]byte(tr(label)), 50))
		pdf.SetFont("Helvetica", "", 10)
		if n := len(pdf.SplitLines([]byte(tr(value)), pw-18-(18+50))); n > lines {
			lines = n
		}
		if pdf.GetY()+float64(lines)*5 > ph-18 {
			pdf.AddPage()
		}
		pdf.SetFont("Helvetica", "B", 10)
		x, y := pdf.GetX(), pdf.GetY()
		pdf.MultiCell(50, 5, tr(label), "", "L", false)
		endLabel := pdf.GetY()
		pdf.SetXY(x+50, y)
		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(0, 5, tr(value), "", "L", false)
		if endLabel > pdf.GetY() {
			pdf.SetY(endLabel)
		}
	}

	section("Identificação")
	for _, f := range d.Identity {
		line(f.Label+":", f.Value)
	}
	for _, s := range d.Sections {
		section(s.Title)
		for _, f := range s.Fields {
			if f.Label == "" {
				line("", f.Value) // continuação (várias emendas, vários períodos)
				continue
			}
			line(f.Label+":", f.Value)
		}
	}

	nDocs, nFiles := 0, 0
	for _, doc := range d.Documents {
		nDocs++
		nFiles += len(doc.Files)
	}
	docTitle := fmt.Sprintf("Documentos (%d itens, %d arquivos)", nDocs, nFiles)
	if toc != nil {
		docTitle = fmt.Sprintf("Sumário dos documentos (%d itens, %d arquivos)", nDocs, nFiles)
	}
	section(docTitle)
	lay.Sections["Documentos"] = lay.Sections[docTitle]
	group := ""
	for _, doc := range d.Documents {
		if doc.Group != group {
			group = doc.Group
			if group != "" {
				if pdf.GetY() > ph-30 {
					pdf.AddPage()
				}
				pdf.Ln(1)
				pdf.SetFont("Helvetica", "B", 9)
				pdf.CellFormat(0, 5, tr(group), "", 1, "L", false, 0, "")
			}
		}
		name := doc.Name
		if doc.AmendmentLabel != "" {
			name += " (" + doc.AmendmentLabel + ")"
		}
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(0, 4.5, tr("• "+name+" — "+doc.Status), "", "L", false)
		if doc.Justification != "" {
			pdf.SetX(pdf.GetX() + 5)
			pdf.SetFont("Helvetica", "I", 8.5)
			pdf.MultiCell(0, 4.2, tr("Justificativa: "+doc.Justification), "", "L", false)
		}
		for _, fl := range doc.Files {
			if toc != nil {
				// Linha do sumário: nome do arquivo à esquerda, página à direita, tudo clicável.
				if pdf.GetY() > ph-22 {
					pdf.AddPage()
				}
				pdf.SetFont("Helvetica", "", 8.5)
				pdf.SetTextColor(0, 0, 160)
				y := pdf.GetY()
				pdf.SetX(23)
				pdf.CellFormat(pw-18-23-22, 4.4, tr(fitText(pdf, fmt.Sprintf("%s (%s)", fl.Name, humanSize(fl.SizeBytes)), pw-18-23-22)), "", 0, "L", false, 0, "")
				pdf.CellFormat(22, 4.4, tr(fmt.Sprintf("p. %d", toc.Start[fl.ID])), "", 1, "R", false, 0, "")
				pdf.SetTextColor(0, 0, 0)
				lay.Links = append(lay.Links, reportLink{Page: pdf.PageNo(), X: 23 * mmToPt, Y: (ph - (y + 4.4)) * mmToPt, W: (pw - 18 - 23) * mmToPt, H: 4.4 * mmToPt, File: fl.ID})
				continue
			}
			pdf.SetX(pdf.GetX() + 5)
			pdf.SetFont("Helvetica", "", 8.5)
			pdf.MultiCell(0, 4.2, tr(fmt.Sprintf("%s (%s)", fl.Name, humanSize(fl.SizeBytes))), "", "L", false)
			if fl.SHA256 != "" {
				pdf.SetX(pdf.GetX() + 5)
				pdf.SetFont("Courier", "", 7)
				pdf.SetTextColor(90, 90, 90)
				pdf.MultiCell(0, 3.6, "SHA-256 "+fl.SHA256, "", "L", false)
				pdf.SetTextColor(0, 0, 0)
			}
		}
	}

	section("Declaração de veracidade")
	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(0, 5, tr(d.Declaration.Text), "", "L", false)
	pdf.Ln(1)
	pdf.SetFont("Helvetica", "", 9)
	pdf.MultiCell(0, 4.5, tr(fmt.Sprintf("Aceita por %s (CPF %s) em %s, a partir do endereço IP %s.",
		d.Declaration.ByName, d.Declaration.ByCPF, d.Declaration.AcceptedAt.In(cuiaba()).Format("02/01/2006 15:04"), d.Declaration.IP)), "", "L", false)

	pdf.Ln(4)
	pdf.SetTextColor(90, 90, 90)
	pdf.SetFont("Helvetica", "I", 9)
	note := "Relatório com as informações cadastradas no projeto, conforme a versão enviada. Não contém o conteúdo dos documentos anexados e não substitui o protocolo oficial."
	if toc != nil {
		note = "Relatório com as informações cadastradas no projeto, conforme a versão enviada, seguido dos documentos anexados, cada um com uma folha de rosto quando solicitado. Não substitui o protocolo oficial."
	}
	pdf.MultiCell(0, 4.5, tr(note), "", "L", false)

	lay.Pages = pdf.PageNo()
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, lay, err
	}
	return buf.Bytes(), lay, nil
}

// ---- Páginas auxiliares do relatório completo ----

// coverSheetPDF gera a folha de rosto de um arquivo: o que é, de onde veio e a integridade (SHA-256).
func coverSheetPDF(d ReportData, doc ReportDocument, fl ReportFile, n, total, pages int) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false)
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(d.GeneratedAt)
	pdf.SetModificationDate(d.GeneratedAt)
	pdf.SetMargins(25, 30, 25)
	pdf.SetAutoPageBreak(false, 0)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, tr(fmt.Sprintf("Projeto %s — arquivo %d de %d", d.Code, n, total)), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(10)
	pdf.SetFont("Helvetica", "B", 22)
	pdf.MultiCell(0, 10, tr(doc.Name), "", "L", false)
	if doc.AmendmentLabel != "" {
		pdf.SetFont("Helvetica", "", 13)
		pdf.MultiCell(0, 7, tr(doc.AmendmentLabel), "", "L", false)
	}
	pdf.Ln(6)
	pdf.SetDrawColor(150, 150, 150)
	pdf.Line(25, pdf.GetY(), 185, pdf.GetY())
	pdf.Ln(8)
	row := func(label, value string) {
		if value == "" {
			return
		}
		pdf.SetFont("Helvetica", "B", 11)
		x, y := pdf.GetX(), pdf.GetY()
		pdf.MultiCell(42, 6, tr(label), "", "L", false)
		end := pdf.GetY()
		pdf.SetXY(x+42, y)
		pdf.SetFont("Helvetica", "", 11)
		pdf.MultiCell(0, 6, tr(value), "", "L", false)
		if end > pdf.GetY() {
			pdf.SetY(end)
		}
		pdf.Ln(1)
	}
	row("Grupo:", doc.Group)
	row("Situação:", doc.Status)
	row("Arquivo:", fl.Name)
	row("Tipo:", fl.ContentType)
	row("Tamanho:", humanSize(fl.SizeBytes))
	if fl.UploadedAt != nil {
		row("Enviado em:", fl.UploadedAt.In(cuiaba()).Format("02/01/2006 15:04"))
	}
	row("Páginas do arquivo:", fmt.Sprintf("%d", pages))
	if fl.SHA256 != "" {
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(42, 6, tr("SHA-256:"), "", 0, "L", false, 0, "")
		pdf.SetFont("Courier", "", 8)
		pdf.MultiCell(0, 5, fl.SHA256, "", "L", false)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// noticePDF é a página que fica no lugar de um arquivo que não pôde entrar no relatório (planejamento D4).
func noticePDF(d ReportData, doc ReportDocument, fl ReportFile, reason string) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false)
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(d.GeneratedAt)
	pdf.SetModificationDate(d.GeneratedAt)
	pdf.SetMargins(25, 30, 25)
	pdf.SetAutoPageBreak(false, 0)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.AddPage()
	pdf.SetFillColor(255, 244, 229)
	pdf.SetDrawColor(200, 120, 0)
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 12, tr("  Arquivo não incorporado ao relatório"), "1", 1, "L", true, 0, "")
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 11)
	name := doc.Name
	if doc.AmendmentLabel != "" {
		name += " (" + doc.AmendmentLabel + ")"
	}
	pdf.MultiCell(0, 6, tr("Documento: "+name), "", "L", false)
	pdf.MultiCell(0, 6, tr("Arquivo: "+fl.Name), "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.MultiCell(0, 6, tr("Motivo: "+reason), "", "L", false)
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(0, 5, tr("O arquivo foi enviado pelo proponente e continua guardado no sistema, mas não pôde ser juntado a este PDF. Consulte-o diretamente no projeto."), "", "L", false)
	if fl.SHA256 != "" {
		pdf.Ln(4)
		pdf.SetFont("Courier", "", 8)
		pdf.MultiCell(0, 4, "SHA-256 "+fl.SHA256, "", "L", false)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
