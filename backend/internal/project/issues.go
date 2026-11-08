package project

import (
	"strings"
	"time"

	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
)

// Severidades das pendências.
const (
	SeverityBlock = "block"
	SeverityWarn  = "warn"
)

// habilitationWarnDays é a antecedência do aviso de certidão perto de vencer.
const habilitationWarnDays = 30

type issueList []Issue

func (l *issueList) add(section, field, code, severity, msg string) {
	*l = append(*l, Issue{Section: section, Field: field, Code: code, Severity: severity, Message: msg})
}

func (l *issueList) required(section, field, msg string, missing bool) {
	if missing {
		l.add(section, field, "required", SeverityBlock, msg)
	}
}

// computeIssues lista o que falta ou está em desacordo para enviar o projeto. Não é validação de gravação
// (o rascunho aceita dados incompletos): é o que o assistente mostra como pendência e o que o envio vai exigir.
// duplicatedSigcon indica que outro projeto já usa o mesmo número do SIGCon.
func computeIssues(p Project, duplicatedSigcon bool, today time.Time, snap *rules.Snapshot) []Issue {
	var l issueList
	proponentIssues(&l, p.Proponent)
	institutionIssues(&l, p.Institution, today)
	proposalIssues(&l, p.Proposal, duplicatedSigcon)
	fundingIssues(&l, p, snap, today)
	if l == nil {
		return []Issue{}
	}
	return l
}

func proponentIssues(l *issueList, s ProponentSection) {
	const sec = "proponent"
	hasMobile := false
	for _, ph := range s.Phones {
		hasMobile = hasMobile || ph.Type == PhoneMobile
	}
	l.required(sec, "phones", "Informe pelo menos um celular.", !hasMobile)
	l.required(sec, "emails", "Informe pelo menos um e-mail.", len(s.Emails) == 0)
	l.required(sec, "municipalityIbge", "Informe o município do proponente.", s.MunicipalityIBGE == "")
	l.required(sec, "jobTitle", "Informe o cargo na instituição.", s.JobTitle == "")
	if s.IsLegalRepresentative == nil {
		l.add(sec, "isLegalRepresentative", "required", SeverityBlock, "Informe se você é o representante legal da instituição.")
		return
	}
	if !*s.IsLegalRepresentative {
		l.required(sec, "legalRepName", "Informe o nome do representante legal.", s.LegalRepName == "")
		l.required(sec, "legalRepCpf", "Informe o CPF do representante legal.", s.LegalRepCPF == "")
		l.required(sec, "legalRepJobTitle", "Informe o cargo do representante legal.", s.LegalRepJobTitle == "")
	}
}

func institutionIssues(l *issueList, s InstitutionSection, today time.Time) {
	const sec = "institution"
	l.required(sec, "instrument", "Escolha o instrumento: convênio ou termo de fomento.", s.Instrument == "")
	l.required(sec, "cnpj", "Informe o CNPJ.", s.CNPJ == "")
	l.required(sec, "name", "Informe o nome do órgão ou instituição.", s.Name == "")
	l.required(sec, "entityTypeId", "Escolha o tipo de entidade.", s.EntityTypeID == "")
	if s.EntityTypeID != "" && s.Nature == "" {
		l.add(sec, "nature", "required", SeverityBlock, "Informe se a entidade é pública ou privada.")
	}
	if s.EntityTypeIsOther && s.EntityTypeOther == "" {
		l.add(sec, "entityTypeOther", "required", SeverityBlock, "Descreva o tipo de entidade.")
	}
	l.required(sec, "municipalityIbge", "Informe o município da sede.", s.MunicipalityIBGE == "")
	l.required(sec, "zip", "Informe o CEP.", s.Zip == "")
	l.required(sec, "street", "Informe o logradouro.", s.Street == "")
	l.required(sec, "number", "Informe o número (ou S/N).", s.Number == "")
	l.required(sec, "district", "Informe o bairro.", s.District == "")
	l.required(sec, "email", "Informe o e-mail institucional.", s.Email == "")
	l.required(sec, "phone", "Informe o telefone institucional.", s.Phone == "")

	switch {
	case s.HabilitationStatus == "":
		l.add(sec, "habilitationStatus", "required", SeverityBlock, "Informe a situação da Certidão de Habilitação do SIGCon.")
	case s.HabilitationStatus != HabilitationFull:
		l.add(sec, "habilitationStatus", "habilitation_not_full", SeverityBlock, "O envio exige a Certidão de Habilitação Plena do SIGCon. Regularize as pendências no SIFI/MT.")
	}
	l.required(sec, "habilitationNumber", "Informe o número da certidão.", s.HabilitationNumber == "")
	if s.HabilitationValidUntil == "" {
		l.add(sec, "habilitationValidUntil", "required", SeverityBlock, "Informe a validade da certidão.")
	} else if until, err := time.Parse(DateLayout, s.HabilitationValidUntil); err == nil {
		switch {
		case until.Before(today):
			l.add(sec, "habilitationValidUntil", "habilitation_expired", SeverityBlock, "A Certidão de Habilitação está vencida.")
		case until.Before(today.AddDate(0, 0, habilitationWarnDays)):
			l.add(sec, "habilitationValidUntil", "habilitation_expiring", SeverityWarn, "A Certidão de Habilitação vence em menos de 30 dias; renove para não perder a validade durante a análise.")
		}
	}
}

func proposalIssues(l *issueList, s ProposalSection, duplicatedSigcon bool) {
	const sec = "proposal"
	l.required(sec, "sigconNumber", "Informe o número da proposta no SIGCon (ex.: 3216/2026).", s.SigconNumber == "")
	l.required(sec, "sigconRegisteredOn", "Informe a data do cadastro da proposta no SIGCon.", s.SigconRegisteredOn == "")
	l.required(sec, "title", "Informe o título do projeto.", s.Title == "")
	l.required(sec, "object", "Descreva o objeto.", s.Object == "")
	l.required(sec, "justification", "Informe a justificativa.", s.Justification == "")
	l.required(sec, "objectNature", "Informe a natureza do objeto.", s.ObjectNature == "")
	executionPeriodIssues(l, sec, s.ExecutionPeriods)

	if s.SigconNumber != "" && len(s.SigconRegisteredOn) >= 4 {
		if _, year, ok := strings.Cut(s.SigconNumber, "/"); ok && year != s.SigconRegisteredOn[:4] {
			l.add(sec, "sigconNumber", "sigcon_year_mismatch", SeverityBlock, "O ano do número da proposta não coincide com o ano da data de cadastro no SIGCon.")
		}
	}
	if duplicatedSigcon {
		l.add(sec, "sigconNumber", "sigcon_duplicated", SeverityWarn, "Já existe outro projeto com este número de proposta do SIGCon. O analista vai conferir.")
	}
}
