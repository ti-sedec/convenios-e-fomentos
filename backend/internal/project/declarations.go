package project

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Declarações que o sistema monta a partir do cadastro, para o proponente conferir o texto, aceitar e (numa etapa seguinte)
// assinar. O texto de cada modelo é o publicado pela SEDEC, sem alteração: só os campos entre chaves vêm do projeto. Mudar o
// texto exige nova versão do modelo (declSpec.version) e a aprovação da SEDEC.

// declSection é a seção das pendências de uma declaração.
const declSection = "declaration"

// GeneratedDeclaration é a prévia de uma declaração: o texto exatamente como será emitido e as pendências que impedem a emissão.
type GeneratedDeclaration struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Annex           string   `json:"annex"`
	TemplateVersion string   `json:"templateVersion"`
	Title           string   `json:"title"`
	Paragraphs      []string `json:"paragraphs"`
	Place           string   `json:"place"`
	SignerName      string   `json:"signerName"`
	SignerRole      string   `json:"signerRole"`
	Institution     string   `json:"institution"`
	// ParamsSHA256 resume tudo o que define o conteúdo (texto, signatário, local, versão do modelo). Se mudar, uma declaração
	// já emitida deixou de refletir o cadastro.
	ParamsSHA256 string  `json:"paramsSha256"`
	Ready        bool    `json:"ready"`
	Issues       []Issue `json:"issues"`
	// Current é a última declaração emitida para este documento (aguardando, assinada, recusada…); nulo se nunca foi emitida.
	Current *DeclarationRecord `json:"current"`
	// signerCPF (só dígitos) nunca sai da API: a equipe só vê o CPF mascarado. Entra no resumo e no pedido de assinatura.
	signerCPF string
	// hideCPF: quem consulta é da equipe e só vê o CPF mascarado; o texto dos parágrafos o mostra mascarado (ver redact).
	hideCPF bool
}

// declEnv reúne o que as declarações usam e não está no Project: o CPF de quem assina e os nomes de municípios, que o Project só
// traz como código (o CPF do Project vem mascarado para a equipe).
type declEnv struct {
	// Place é o município da instituição ("Cuiabá/MT").
	Place string
	// SignerCPF são os 11 dígitos do CPF de quem assina.
	SignerCPF string
	// HomePlace é o município do endereço residencial de quem assina ("Sorriso/MT").
	HomePlace string
}

// declSpec descreve um modelo de declaração.
type declSpec struct {
	code    string
	name    string
	title   string
	version string
	// annex devolve o número do anexo do modelo oficial para o instrumento.
	annex func(instrument string) string
	// check devolve as pendências próprias da declaração (as do signatário são comuns e checadas à parte).
	check func(p Project, e declEnv) []Issue
	// body devolve os parágrafos do texto oficial.
	body func(p Project, e declEnv) []string
}

func declIssue(field, code, msg string) Issue {
	return Issue{Section: declSection, Field: field, Code: code, Severity: SeverityBlock, Message: msg}
}

// declarationSpecs são os modelos disponíveis. Os que dependem dos dados pessoais de quem assina (gratuidade, publicidade) e a
// compatibilidade de preços estão em declaration_texts.go. As outras declarações do checklist (ex.: não impedimento, só do Termo
// de Fomento) ainda não têm modelo aqui.
var declarationSpecs = map[string]declSpec{
	"declaracao_contrapartida": {
		code: "declaracao_contrapartida", name: "Declaração de contrapartida", title: "DECLARAÇÃO DE CONTRAPARTIDA", version: "2026-10",
		annex: func(string) string { return "Anexo IV" },
		check: func(p Project, _ declEnv) []Issue {
			var out []Issue
			if !municipalTypes[p.Institution.EntityTypeCode] {
				out = append(out, declIssue("entityTypeCode", "decl_municipality_only",
					"O modelo oficial desta declaração é para município (\"este município depositará…\"). Para outro tipo de entidade, anexe a declaração manualmente."))
			}
			if p.Funding.CounterpartKind != CounterpartFinancial {
				out = append(out, declIssue("counterpartKind", "decl_counterpart_not_financial",
					"O modelo oficial declara um depósito na conta exclusiva. Informe a contrapartida financeira em Recursos, ou anexe a declaração manualmente."))
			}
			if p.Funding.CounterpartCents <= 0 {
				out = append(out, declIssue("counterpartCents", "decl_counterpart_value", "Informe o valor da contrapartida em Recursos."))
			}
			return out
		},
		body: func(p Project, _ declEnv) []string {
			return []string{fmt.Sprintf("Conforme Instrução Normativa SEPLAN/SEFAZ/CGE nº 001/2015, de 23 de fevereiro de 2015 e Portaria vigente "+
				"(disponibilizada pela SEFAZ), que determina os indicadores de contrapartida referente à transferência voluntária de recursos "+
				"financiados pelo Estado de Mato Grosso, DECLARO que este município depositará o valor de %s, na conta exclusiva para execução deste objeto.",
				brl(p.Funding.CounterpartCents))}
		},
	},
	"declaracao_nao_duplicidade": {
		code: "declaracao_nao_duplicidade", name: "Declaração de não duplicidade de objeto", title: "DECLARAÇÃO DE NÃO DUPLICIDADE DE OBJETO", version: "2026-10",
		annex: func(instrument string) string {
			if instrument == InstrumentFomento {
				return "Anexo V"
			}
			return "Anexo VI"
		},
		check: func(Project, declEnv) []Issue { return nil },
		body: func(Project, declEnv) []string {
			return []string{"Declaro que esta instituição não possui parceria (s) vigentes (s), celebrado (s) com qualquer órgão da União/Estado ou Município, " +
				"para execução de objeto idêntico ou similar ao proposto, sob pena de responsabilidade civil, penal e administrativa."}
		},
	},
}

// signerOf devolve quem assina: o próprio proponente, se for o representante legal, ou o representante informado.
func signerOf(p Project) (name, role string, issues []Issue) {
	switch {
	case p.Proponent.IsLegalRepresentative == nil:
		return "", "", []Issue{declIssue("isLegalRepresentative", "decl_signer_unknown", "Informe em Proponente se você é o representante legal da instituição.")}
	case *p.Proponent.IsLegalRepresentative:
		name, role = p.Proponent.Name, p.Proponent.JobTitle
	default:
		name, role = p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle
	}
	if strings.TrimSpace(name) == "" {
		issues = append(issues, declIssue("legalRepName", "decl_signer_name", "Informe o nome do representante legal em Proponente."))
	}
	if strings.TrimSpace(role) == "" {
		issues = append(issues, declIssue("legalRepJobTitle", "decl_signer_role", "Informe o cargo do representante legal em Proponente."))
	}
	return name, role, issues
}

// buildDeclaration monta a prévia a partir do projeto e do contexto (ver declEnv); o que faltar vira pendência.
func buildDeclaration(spec declSpec, p Project, e declEnv) GeneratedDeclaration {
	name, role, issues := signerOf(p)
	if len(e.SignerCPF) != 11 {
		issues = append(issues, declIssue("legalRepCpf", "decl_signer_cpf", "Informe o CPF de quem assina (o representante legal) em Proponente."))
	}
	issues = append(issues, spec.check(p, e)...)
	if strings.TrimSpace(p.Institution.Name) == "" {
		issues = append(issues, declIssue("institutionName", "decl_institution", "Informe o nome da instituição em Instituição."))
	}
	if e.Place == "" {
		issues = append(issues, declIssue("municipalityIbge", "decl_place", "Informe o município da instituição em Instituição."))
	}
	place, signerCPF := e.Place, e.SignerCPF
	d := GeneratedDeclaration{
		Code: spec.code, Name: spec.name, Annex: spec.annex(p.Institution.Instrument), TemplateVersion: spec.version, Title: spec.title,
		Paragraphs: spec.body(p, e), Place: place, SignerName: name, SignerRole: role, Institution: p.Institution.Name,
		Ready: len(issues) == 0, Issues: issues, signerCPF: signerCPF,
	}
	if d.Issues == nil {
		d.Issues = []Issue{}
	}
	h := sha256.New()
	for _, part := range append([]string{spec.code, spec.version, d.Place, d.SignerName, d.SignerRole, d.Institution, signerCPF, e.HomePlace}, d.Paragraphs...) {
		_, _ = fmt.Fprintf(h, "%d:%s\n", len(part), part) // hash.Hash.Write nunca devolve erro
	}
	d.ParamsSHA256 = hex.EncodeToString(h.Sum(nil))
	return d
}

// signerCPFOf devolve os dígitos do CPF de quem assina: o proponente, se for o representante legal, ou o representante informado.
func signerCPFOf(r db.Project) string {
	if r.IsLegalRepresentative.Valid && !r.IsLegalRepresentative.Bool {
		return str(r.LegalRepCpf)
	}
	return r.ProponentCpf
}

// placeOf devolve "Município/UF" a partir do código IBGE; vazio se não há código.
func placeOf(ctx context.Context, q *db.Queries, ibge string) (string, error) {
	if ibge == "" {
		return "", nil
	}
	m, err := q.GetMunicipality(ctx, ibge)
	if err != nil {
		return "", notFound(err)
	}
	return m.Name + "/" + m.Uf, nil
}

// signerPersonal devolve os dados pessoais de quem assina: os do proponente, se é o representante legal, ou os do representante.
func signerPersonal(p Project) PersonalData {
	if p.Proponent.IsLegalRepresentative != nil && !*p.Proponent.IsLegalRepresentative {
		return p.Proponent.LegalRepPersonal
	}
	return p.Proponent.Personal
}

// declarationEnv reúne o contexto das declarações do projeto.
func declarationEnv(ctx context.Context, q *db.Queries, r db.Project, p Project) (declEnv, error) {
	place, err := placeOf(ctx, q, p.Institution.MunicipalityIBGE)
	if err != nil {
		return declEnv{}, err
	}
	home, err := placeOf(ctx, q, signerPersonal(p).MunicipalityIBGE)
	if err != nil {
		return declEnv{}, err
	}
	return declEnv{Place: place, SignerCPF: signerCPFOf(r), HomePlace: home}, nil
}

// draftDeclaration monta a declaração com o cadastro atual do projeto. 409 se ela não se aplica às respostas atuais.
func (s *Service) draftDeclaration(ctx context.Context, q *db.Queries, r db.Project, spec declSpec, actor Actor) (GeneratedDeclaration, error) {
	p, cl, err := s.viewWithChecklist(ctx, q, r, actor)
	if err != nil {
		return GeneratedDeclaration{}, err
	}
	applies := false
	for _, it := range cl.Items {
		applies = applies || it.Code == spec.code
	}
	if !applies {
		return GeneratedDeclaration{}, httpapi.ErrConflict("document_not_applicable", "Este documento não se aplica ao projeto com as respostas atuais.")
	}
	env, err := declarationEnv(ctx, q, r, *p)
	if err != nil {
		return GeneratedDeclaration{}, err
	}
	d := buildDeclaration(spec, *p, env)
	d.hideCPF = p.Access == ""
	return d, nil
}

// redact troca o CPF por sua forma mascarada nos parágrafos, para quem só vê o CPF mascarado. O resumo (ParamsSHA256) já foi calculado
// com o texto completo, então não depende de quem consulta.
func (d *GeneratedDeclaration) redact() {
	if !d.hideCPF || len(d.signerCPF) != 11 {
		return
	}
	full, masked := cpf.Format(d.signerCPF), cpf.Mask(d.signerCPF)
	paras := make([]string, len(d.Paragraphs))
	for i, para := range d.Paragraphs {
		paras[i] = strings.ReplaceAll(para, full, masked)
	}
	d.Paragraphs = paras
}

// DeclarationPreview devolve o texto da declaração como será emitido, com as pendências e a situação da última emissão. Se a
// assinatura está pendente, consulta o MT-ID antes e conclui o que já foi assinado. 404 se o projeto está fora do alcance da
// leitura ou não há modelo para o código; 409 se a declaração não se aplica às respostas atuais do projeto.
func (s *Service) DeclarationPreview(ctx context.Context, actor Actor, id uuid.UUID, code string, read authz.Decision) (*GeneratedDeclaration, error) {
	spec, ok := declarationSpecs[code]
	if !ok {
		return nil, httpapi.ErrNotFound()
	}
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return nil, httpapi.ErrNotFound()
	}
	d, err := s.draftDeclaration(ctx, s.q, r, spec, actor)
	if err != nil {
		return nil, err
	}
	rec, err := s.q.GetLatestProjectDeclaration(ctx, db.GetLatestProjectDeclarationParams{ProjectID: id, RequirementCode: code})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if rec.Status == DeclAwaiting {
			rec = s.refreshDeclaration(ctx, rec)
		}
		v := recordView(rec, d)
		d.Current = &v
	}
	d.redact()
	return &d, nil
}

// renderDeclaration gera o PDF da declaração (A4), no formato do modelo oficial: anexo, título, texto, "Local e data", nome e cargo
// do responsável com a instituição. Deixa livre o espaço da assinatura e devolve onde ele ficou (signatureSpot): o texto varia de
// uma declaração para outra, então a posição sai do próprio layout, não de uma constante. at é a data da emissão (horário de Cuiabá).
func renderDeclaration(d GeneratedDeclaration, at time.Time) ([]byte, signatureSpot, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false) // textos legíveis no arquivo: facilita conferência e testes
	pdf.SetTitle(d.Name, true)
	pdf.SetAuthor(d.Institution, true)
	pdf.SetMargins(25, 25, 25)
	pdf.SetAutoPageBreak(true, 20)
	tr := pdf.UnicodeTranslatorFromDescriptor("") // fontes padrão do PDF usam Latin-1
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 6, tr(strings.ToUpper(d.Annex)), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, tr(d.Institution), "", 1, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(14)

	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, tr(d.Title), "", 1, "C", false, 0, "")
	pdf.Ln(10)

	pdf.SetFont("Helvetica", "", 12)
	for _, para := range d.Paragraphs {
		pdf.MultiCell(0, 7, tr(para), "", "J", false)
		pdf.Ln(3)
	}
	pdf.Ln(10)
	pdf.CellFormat(0, 7, tr(fmt.Sprintf("%s, %s.", d.Place, longDate(at.In(cuiaba())))), "", 1, "L", false, 0, "")
	// O espaço da assinatura e o bloco do nome (12 mm) precisam caber na mesma página: se não couberem, vão para a próxima.
	const pageH, bottomMargin = 297.0, 20.0
	if pdf.GetY()+signatureGapMM+12 > pageH-bottomMargin {
		pdf.AddPage()
	}
	spot := signatureSpotAt(pdf.PageNo()-1, pdf.GetY())
	pdf.Ln(signatureGapMM) // espaço da assinatura

	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 6, tr(d.SignerName), "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(0, 6, tr(d.SignerRole+" — "+d.Institution), "", 1, "C", false, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, signatureSpot{}, err
	}
	return buf.Bytes(), spot, nil
}

// signatureGapMM é a altura (mm) deixada em branco entre a data e o nome do responsável para a assinatura.
const signatureGapMM = 28

// signatureSpot é onde o MT-ID deve pôr a assinatura: página (a primeira é 0) e coordenadas em pontos (1/72"), com origem no
// canto superior esquerdo da página.
type signatureSpot struct{ Page, X, Y int }

// Calibração contra o MT-ID de desenvolvimento (novembro de 2026): o pedido com x=200, y=150 gerou um carimbo visível de cerca de
// 217 x 28 pt com a borda esquerda em x+3 e o centro vertical em y-2 (o campo de assinatura do PDF tem 260 x 80 pt, e o carimbo
// ocupa a parte de baixo dele). Ou seja: x é a borda esquerda e y o centro vertical do carimbo, medidos a partir do topo.
const (
	signatureStampWidthPt = 217
	signatureStampXOffset = 3
	signatureStampYOffset = -2
	pageWidthPt           = 595.28
	ptPerMM               = 72 / 25.4
)

// signatureSpotAt centraliza o carimbo, na horizontal, na página e, na vertical, no meio do espaço que começa em topMM (mm).
func signatureSpotAt(page int, topMM float64) signatureSpot {
	x := (pageWidthPt-signatureStampWidthPt)/2 - signatureStampXOffset
	y := (topMM+signatureGapMM/2)*ptPerMM - signatureStampYOffset
	return signatureSpot{Page: page, X: int(x + 0.5), Y: int(y + 0.5)}
}

var months = [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

// longDate escreve a data por extenso: "24 de outubro de 2026".
func longDate(t time.Time) string {
	return fmt.Sprintf("%d de %s de %d", t.Day(), months[t.Month()-1], t.Year())
}
