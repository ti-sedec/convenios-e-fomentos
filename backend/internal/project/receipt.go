package project

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cnpj"
)

// renderReceipt gera o comprovante de recebimento em PDF (A4). Não é o protocolo oficial: atesta o que a SEDEC recebeu.
func renderReceipt(snap VersionSnapshot) ([]byte, error) {
	p := snap.Project
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false) // textos legíveis no arquivo: facilita conferência e testes
	pdf.SetTitle("Comprovante de recebimento "+p.Code, true)
	pdf.SetAuthor("SEDEC/MT", true)
	pdf.SetMargins(18, 16, 18)
	pdf.SetAutoPageBreak(true, 16)
	tr := pdf.UnicodeTranslatorFromDescriptor("") // fontes padrão do PDF usam Latin-1
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 15)
	pdf.CellFormat(0, 8, tr("Comprovante de recebimento de projeto"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, tr("Secretaria de Estado de Desenvolvimento Econômico de Mato Grosso (SEDEC/MT)"), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(3)

	// Quadro principal.
	pdf.SetFillColor(240, 240, 240)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 8, tr("  Código do projeto: "+p.Code), "1", 1, "L", true, 0, "")
	at := ""
	if p.SubmittedAt != nil {
		at = p.SubmittedAt.In(cuiaba()).Format("02/01/2006 às 15:04")
	}
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, tr("  Recebido em "+at+" (horário de Cuiabá) — versão 1 do projeto"), "LRB", 1, "L", false, 0, "")
	if p.LateSubmission {
		pdf.SetTextColor(150, 60, 0)
		pdf.MultiCell(0, 5, tr("Enviado depois da data-limite de protocolo antes da vedação eleitoral: o projeto segue apenas com atos preparatórios e preliminares; o repasse fica para depois do período de restrição."), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(3)

	section := func(title string) {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(0, 6, tr(title), "B", 1, "L", false, 0, "")
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
	}
	line := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(46, 5, tr(label), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(0, 5, tr(value), "", "L", false)
	}

	section("Projeto")
	line("Instrumento:", map[string]string{InstrumentConvenio: "Convênio", InstrumentFomento: "Termo de Fomento"}[p.Institution.Instrument])
	line("Título:", p.Proposal.Title)
	line("Nº no SIGCon:", p.Proposal.SigconNumber)
	line("Objeto:", p.Proposal.Object)
	line("Vigência prevista:", brDate(p.Proposal.StartDate)+" a "+brDate(p.Proposal.EndDate))
	label := "Execução em:"
	for _, t := range executionPeriodsText(p.Proposal.ExecutionPeriods) {
		line(label, t)
		label = ""
	}

	section("Instituição")
	line("Nome:", p.Institution.Name)
	line("CNPJ:", formatCNPJ(p.Institution.CNPJ))
	line("Endereço:", fmt.Sprintf("%s, %s — %s — CEP %s", p.Institution.Street, p.Institution.Number, p.Institution.District, p.Institution.Zip))
	line("Certidão de Habilitação:", fmt.Sprintf("nº %s, válida até %s", p.Institution.HabilitationNumber, brDate(p.Institution.HabilitationValidUntil)))

	section("Proponente")
	line("Nome:", p.Proponent.Name)
	line("Cargo:", p.Proponent.JobTitle)
	if p.Proponent.IsLegalRepresentative != nil && !*p.Proponent.IsLegalRepresentative {
		line("Representante legal:", fmt.Sprintf("%s — %s", p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle))
	}

	section("Recursos")
	f := p.Funding
	line("Tipo de recurso:", map[string]string{FundingAmendment: "Emenda parlamentar", FundingOwn: "Recurso próprio da SEDEC", FundingMixed: "Emenda parlamentar e recurso próprio da SEDEC"}[f.Type])
	line("Recurso próprio:", brlIf(f.OwnAmountCents))
	line("Emendas:", brlIf(f.AmendmentTotalCents))
	line("Total solicitado:", brl(f.TotalCents))
	line("Contrapartida:", brlIf(f.CounterpartCents))
	line("Custos indiretos:", brlIf(f.IndirectCostsCents))

	section(fmt.Sprintf("Documentos (%d entregues, %d com justificativa de não envio, de %d)", snap.Documents.Summary.Done, snap.Documents.Summary.Justified, snap.Documents.Summary.Total))
	pdf.SetFont("Helvetica", "", 9)
	for _, it := range snap.Documents.Items {
		status := "entregue"
		if it.Status == ItemJustified {
			status = "justificado"
		}
		name := it.Name
		if it.AmendmentLabel != "" {
			name += " (" + it.AmendmentLabel + ")"
		}
		n := 0
		for _, fl := range it.Files {
			if fl.Status == FileConfirmed {
				n++
			}
		}
		suffix := status
		if n > 0 {
			suffix = fmt.Sprintf("%s, %d arquivo(s)", status, n)
		}
		pdf.MultiCell(0, 4.5, tr("• "+name+" — "+suffix), "", "L", false)
	}

	section("Declaração de veracidade")
	pdf.MultiCell(0, 5, tr(snap.Declaration.Text), "", "L", false)
	pdf.Ln(1)
	pdf.SetFont("Helvetica", "", 9)
	pdf.MultiCell(0, 4.5, tr(fmt.Sprintf("Aceita por %s (CPF %s) em %s, a partir do endereço IP %s.",
		snap.Declaration.ByName, snap.Declaration.ByCPF, snap.Declaration.AcceptedAt.In(cuiaba()).Format("02/01/2006 15:04"), snap.Declaration.IP)), "", "L", false)

	pdf.Ln(4)
	pdf.SetTextColor(90, 90, 90)
	pdf.SetFont("Helvetica", "I", 9)
	pdf.MultiCell(0, 4.5, tr("Este comprovante atesta o recebimento do projeto e dos documentos listados. Não é o número de protocolo oficial: o processo no SIGADOC será aberto depois da conferência dos documentos pela equipe da SEDEC."), "", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func formatCNPJ(d string) string { return cnpj.Format(d) }

func brlIf(c int64) string {
	if c == 0 {
		return ""
	}
	return brl(c)
}
