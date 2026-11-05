package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/outbox"
)

// E-mails do projeto. Nenhum deles é enviado aqui: cada função só GRAVA a mensagem na caixa de saída (internal/outbox), usando as
// consultas q da transação da ação que a causou. Assim o e-mail só existe se a ação foi confirmada, e um SMTP fora do ar ou lento
// nunca atrasa nem derruba a ação. O envio, com tentativas, é do Worker da caixa de saída.

// Códigos dos perfis de equipe que recebem avisos. Os perfis nascem nas migrações 00022 a 00024.
const (
	roleReviewer   = "department-reviewer"
	roleAnalyst    = "analyst"
	roleSupervisor = "administrative-supervisor"
)

// Tipos de e-mail (para diagnóstico na caixa de saída).
const (
	kindSubmitted           = "project.submitted"
	kindComplementRequested = "project.complement-requested"
	kindResubmitted         = "project.resubmitted"
	kindArchived            = "project.archived"
	kindInvited             = "project.invited"
)

func primaryEmail(p *Project) string {
	to := ""
	for _, e := range p.Proponent.Emails {
		if to == "" || e.Primary {
			to = e.Address
		}
	}
	return to
}

func (s *Service) link(id uuid.UUID) string {
	return fmt.Sprintf("%s/dashboard/projetos/%s", s.publicURL, id)
}

// enqueue grava um e-mail na caixa de saída, na transação de q.
func (s *Service) enqueue(ctx context.Context, q *db.Queries, kind string, projectID uuid.UUID, to, subject, body, dedup string) error {
	_, err := outbox.Enqueue(ctx, q, outbox.Message{Kind: kind, ProjectID: &projectID, To: to, Subject: subject, Body: body, DedupKey: dedup})
	return err
}

// toProponent grava o e-mail para o e-mail principal do proponente, com saudação e o link do projeto.
func (s *Service) toProponent(ctx context.Context, q *db.Queries, kind, dedup string, p *Project, subject, body string) error {
	greeting := fmt.Sprintf("Olá, %s.\n\n", p.Proponent.Name)
	footer := fmt.Sprintf("\n\nAcompanhe o projeto em:\n%s\n", s.link(p.ID))
	return s.enqueue(ctx, q, kind, p.ID, primaryEmail(p), subject, greeting+body+footer, dedup)
}

// notifySubmitted confirma o recebimento ao proponente.
func (s *Service) notifySubmitted(ctx context.Context, q *db.Queries, p *Project) error {
	at := ""
	if p.SubmittedAt != nil {
		at = p.SubmittedAt.In(cuiaba()).Format("02/01/2006 15:04")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Olá, %s.\n\nRecebemos o seu projeto na Secretaria de Estado de Desenvolvimento Econômico (SEDEC).\n\n", p.Proponent.Name)
	fmt.Fprintf(&b, "Código do projeto: %s\nTítulo: %s\nInstituição: %s\nEnviado em: %s (horário de Cuiabá)\n\n", p.Code, p.Proposal.Title, p.Institution.Name, at)
	fmt.Fprintf(&b, "Acompanhe o projeto e baixe o comprovante de recebimento em:\n%s\n\n", s.link(p.ID))
	b.WriteString("Este e-mail confirma o recebimento. O número de protocolo oficial (SIGADOC) será informado depois da conferência dos documentos pela equipe da SEDEC.\n")
	if p.LateSubmission {
		b.WriteString("\nAtenção: o projeto foi enviado depois da data-limite de protocolo antes da vedação eleitoral. Ele seguirá apenas com atos preparatórios e preliminares; o repasse de recursos fica para depois do período de restrição.\n")
	}
	return s.enqueue(ctx, q, kindSubmitted, p.ID, primaryEmail(p), fmt.Sprintf("Projeto %s recebido pela SEDEC", p.Code), b.String(), "project-submitted:"+p.ID.String())
}

func (s *Service) notifyComplementRequested(ctx context.Context, q *db.Queries, p *Project) error {
	if p.Complement == nil {
		return nil
	}
	c := p.Complement
	var b strings.Builder
	fmt.Fprintf(&b, "A equipe da SEDEC analisou o projeto %s e pediu correções.\n\n", p.Code)
	fmt.Fprintf(&b, "Prazo para enviar as correções: %s (horário de Cuiabá). Se o prazo vencer sem o reenvio, o projeto será arquivado e será preciso começar um novo.\n\n", c.DueAt.In(cuiaba()).Format("02/01/2006 15:04"))
	if c.Message != "" {
		fmt.Fprintf(&b, "Mensagem da equipe:\n%s\n\n", c.Message)
	}
	b.WriteString("Itens devolvidos:\n")
	for _, it := range c.Items {
		fmt.Fprintf(&b, "• %s", it.Label)
		if it.Note != "" {
			fmt.Fprintf(&b, ": %s", it.Note)
		}
		b.WriteString("\n")
	}
	subject := fmt.Sprintf("Projeto %s: correções solicitadas (prazo %s)", p.Code, c.DueAt.In(cuiaba()).Format("02/01/2006"))
	return s.toProponent(ctx, q, kindComplementRequested, fmt.Sprintf("complement-requested:%s:%d", p.ID, c.Number), p, subject, b.String())
}

func (s *Service) notifyResubmitted(ctx context.Context, q *db.Queries, p *Project, version int) error {
	return s.toProponent(ctx, q, kindResubmitted, fmt.Sprintf("project-resubmitted:%s:%d", p.ID, version), p,
		fmt.Sprintf("Projeto %s: correções recebidas", p.Code),
		fmt.Sprintf("Recebemos as correções do projeto %s (versão %d). Ele voltou para a análise da equipe da SEDEC.", p.Code, version))
}

// Quem arquivou, para o aviso da equipe: dito pelo papel, nunca pelo nome da pessoa.
var archivedByText = map[string]string{
	ArchiveComplementDeadline: "o sistema, porque o prazo de complementação venceu",
	ArchiveWithdrawn:          "o Proponente (desistência)",
	ArchiveDenied:             "o Secretário de Estado (negação)",
	ArchiveMissingDocuments:   "a equipe de análise, por falta de documentos obrigatórios",
	ArchiveOther:              "a equipe de análise",
}

// Motivo, no texto para o proponente.
var archiveReasonText = map[string]string{
	ArchiveComplementDeadline: "o prazo para enviar as correções venceu",
	ArchiveWithdrawn:          "a pedido do próprio proponente (desistência)",
	ArchiveDenied:             "a Secretaria decidiu não dar continuidade ao projeto",
	ArchiveMissingDocuments:   "faltaram documentos obrigatórios",
	ArchiveOther:              "decisão da equipe da SEDEC",
}

// notifyArchived avisa o arquivamento (de qualquer motivo, inclusive a desistência) ao proponente, aos Revisores do departamento do
// projeto (se já havia um), ao Analista responsável (ou, sem responsável ou se ele não pode receber, a todos os Analistas) e aos
// Supervisores. Quem tem mais de um papel, ou o mesmo e-mail em dois deles, recebe uma só
// mensagem; o proponente recebe a dele, com a orientação de duplicar o projeto.
//
// A mensagem da equipe traz só a identificação do projeto, o motivo (a categoria) e o link: o texto livre do motivo e os dados
// pessoais ficam no sistema, atrás de login.
func (s *Service) notifyArchived(ctx context.Context, q *db.Queries, p *Project, departmentID, analystID *uuid.UUID) error {
	if p.Archive == nil {
		return nil
	}
	reason := p.Archive.Reason
	seen := map[string]bool{}
	claim := func(addr string) bool { // true na primeira vez que o endereço aparece
		k := strings.ToLower(strings.TrimSpace(addr))
		if k == "" || seen[k] {
			return false
		}
		seen[k] = true
		return true
	}

	// Proponente.
	if to := primaryEmail(p); claim(to) {
		body := fmt.Sprintf("O projeto %s foi arquivado: %s.\n", p.Code, archiveReasonText[reason])
		if p.Archive.Note != "" {
			body += "\nMotivo registrado:\n" + p.Archive.Note + "\n"
		}
		body += "\nProjeto arquivado não volta a andar. Se ainda quiser a parceria, abra o projeto e use \"Duplicar projeto\" para começar um novo com os mesmos dados."
		if err := s.toProponent(ctx, q, kindArchived, fmt.Sprintf("project-archived:%s:%s", p.ID, strings.ToLower(to)), p, fmt.Sprintf("Projeto %s arquivado", p.Code), body); err != nil {
			return err
		}
	}

	// Equipe.
	var staff []string
	if departmentID != nil {
		list, err := q.ListStaffEmailsByRoleAndDepartment(ctx, db.ListStaffEmailsByRoleAndDepartmentParams{Code: roleReviewer, DepartmentID: departmentID})
		if err != nil {
			return err
		}
		staff = append(staff, list...)
	}
	analysts, err := s.analystRecipients(ctx, q, analystID)
	if err != nil {
		return err
	}
	staff = append(staff, analysts...)
	supervisors, err := q.ListStaffEmailsByRole(ctx, roleSupervisor)
	if err != nil {
		return err
	}
	staff = append(staff, supervisors...)
	var b strings.Builder
	fmt.Fprintf(&b, "O projeto %s foi arquivado por %s.\n\n", p.Code, archivedByText[reason])
	fmt.Fprintf(&b, "Título: %s\nInstituição: %s\n\n", p.Proposal.Title, p.Institution.Name)
	fmt.Fprintf(&b, "O motivo registrado e os demais dados estão no sistema:\n%s\n", s.link(p.ID))
	for _, to := range staff {
		if !claim(to) {
			continue
		}
		if err := s.enqueue(ctx, q, kindArchived, p.ID, to, fmt.Sprintf("Projeto %s arquivado", p.Code), b.String(), fmt.Sprintf("project-archived:%s:%s", p.ID, strings.ToLower(to))); err != nil {
			return err
		}
	}
	return nil
}

// analystRecipients: o Analista responsável, se houver e puder receber (ativo e não bloqueado); senão, todos os Analistas ativos.
func (s *Service) analystRecipients(ctx context.Context, q *db.Queries, analystID *uuid.UUID) ([]string, error) {
	if analystID != nil {
		email, err := q.GetNotifiableAdminEmailByID(ctx, *analystID)
		if err == nil && email != "" {
			return []string{email}, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return q.ListStaffEmailsByRole(ctx, roleAnalyst)
}

// notifyInvited avisa o convidado por e-mail. O e-mail da conta que tem o CPF tem prioridade e nunca é revelado ao titular; sem conta,
// vale o e-mail que o titular informou; sem nenhum dos dois, não há aviso. Cada convite é uma mensagem própria (sem deduplicação):
// convidar de novo depois de revogar manda outro aviso.
func (s *Service) notifyInvited(ctx context.Context, q *db.Queries, actor Actor, projectID uuid.UUID, title string, m db.ProjectMember) error {
	to := str(m.Email)
	if acct, err := q.GetNotifiableAdminEmail(ctx, &m.Cpf); err == nil && acct != "" {
		to = acct
	}
	if to == "" {
		return nil
	}
	if title == "" {
		title = "(projeto sem título)"
	}
	what := "para consultar (somente leitura)"
	if m.Level == LevelEdit {
		what = "para consultar e editar"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Olá.\n\n%s deu a você acesso %s ao projeto \"%s\" no sistema de convênios e termos de fomento da SEDEC-MT.\n\n", actor.Name, what, title)
	fmt.Fprintf(&b, "Para acessar, entre no sistema pelo MT Login com o CPF %s:\n%s\n\n", maskCPF(m.Cpf), s.link(projectID))
	b.WriteString("Se você não esperava este convite, ignore esta mensagem: o titular do projeto pode revogar o acesso a qualquer momento.\n")
	return s.enqueue(ctx, q, kindInvited, projectID, to, "Você recebeu acesso a um projeto na SEDEC-MT", b.String(), "")
}

// ---- Enquadramento: avisos aos Supervisores ----

const (
	kindFramingIndicated = "project.framing-indicated"
	kindFramingRefused   = "project.framing-refused"

	// maxRefusalInEmail limita a justificativa do Revisor no e-mail; o texto inteiro fica no sistema.
	maxRefusalInEmail = 500
)

var (
	instrumentText = map[string]string{InstrumentConvenio: "Convênio", InstrumentFomento: "Termo de Fomento"}
	fundingText    = map[string]string{"amendment": "Emenda parlamentar", "own": "Recurso próprio", "mixed": "Misto (recurso próprio e emenda)"}
	natureText     = map[string]string{"public": "Pública", "private": "Privada"}
)

func labelOr(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return key
}

// framingSummary é o resumo do projeto que vai nos e-mails aos Supervisores. É uma LISTA PERMITIDA: só entra o que está aqui, então um
// campo novo no cadastro nunca vaza por padrão. Ficam de fora CPF, telefones, e-mails de contato, endereços, dados de quem assina,
// justificativa, documentos e anexos: o resto está no sistema, atrás de login (link no fim da mensagem).
func (s *Service) framingSummary(ctx context.Context, q *db.Queries, p *Project) (string, error) {
	entityType := ""
	if id, err := uuid.Parse(p.Institution.EntityTypeID); err == nil {
		t, err := q.GetEntityType(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		entityType = t.Name
	}
	var munis []string
	for _, m := range p.Proposal.Municipalities {
		munis = append(munis, m.Name)
	}
	f := p.Funding
	var b strings.Builder
	fmt.Fprintf(&b, "Código: %s\nInstrumento: %s\nTítulo: %s\n", p.Code, labelOr(instrumentText, p.Institution.Instrument), p.Proposal.Title)
	if p.Proposal.Object != "" {
		fmt.Fprintf(&b, "Objeto: %s\n", p.Proposal.Object)
	}
	if p.Proposal.SigconNumber != "" {
		fmt.Fprintf(&b, "Nº SIGCon: %s\n", p.Proposal.SigconNumber)
	}
	if p.Proposal.StartDate != "" || p.Proposal.EndDate != "" {
		fmt.Fprintf(&b, "Vigência: %s a %s\n", brDate(p.Proposal.StartDate), brDate(p.Proposal.EndDate))
	}
	fmt.Fprintf(&b, "Tipo de recurso: %s\nValor total: %s\n", labelOr(fundingText, f.Type), brl(f.TotalCents))
	if f.AmendmentTotalCents > 0 {
		fmt.Fprintf(&b, "Emendas: %s\n", brl(f.AmendmentTotalCents))
	}
	if f.OwnAmountCents > 0 {
		fmt.Fprintf(&b, "Recurso próprio: %s\n", brl(f.OwnAmountCents))
	}
	if f.CounterpartCents > 0 {
		fmt.Fprintf(&b, "Contrapartida: %s\n", brl(f.CounterpartCents))
	}
	if len(munis) > 0 {
		fmt.Fprintf(&b, "Municípios de execução: %s\n", strings.Join(munis, ", "))
	}
	fmt.Fprintf(&b, "\nInstituição: %s\nCNPJ: %s\n", p.Institution.Name, formatCNPJ(p.Institution.CNPJ))
	if entityType != "" {
		fmt.Fprintf(&b, "Tipo de entidade: %s\n", entityType)
	}
	fmt.Fprintf(&b, "Natureza: %s\nUF: %s\n", labelOr(natureText, p.Institution.Nature), p.Institution.UF)
	fmt.Fprintf(&b, "\nProponente: %s", p.Proponent.Name)
	if p.Proponent.JobTitle != "" {
		fmt.Fprintf(&b, " (%s)", p.Proponent.JobTitle)
	}
	b.WriteString("\n")
	return b.String(), nil
}

// toSupervisors grava o e-mail para cada Supervisor ativo (uma vez por endereço).
func (s *Service) toSupervisors(ctx context.Context, q *db.Queries, kind, dedupPrefix string, p *Project, subject, body string) error {
	list, err := q.ListStaffEmailsByRole(ctx, roleSupervisor)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, to := range list {
		k := strings.ToLower(strings.TrimSpace(to))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if err := s.enqueue(ctx, q, kind, p.ID, to, subject, body, dedupPrefix+":"+k); err != nil {
			return err
		}
	}
	return nil
}

// notifyFramingIndicated: a documentação está regular e o projeto foi encaminhado ao Revisor do Departamento indicado.
func (s *Service) notifyFramingIndicated(ctx context.Context, q *db.Queries, p *Project, round int, department string) error {
	summary, err := s.framingSummary(ctx, q, p)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "A documentação do projeto %s está regular e ele foi encaminhado ao Revisor do Departamento, para confirmar o enquadramento.\n\n", p.Code)
	fmt.Fprintf(&b, "Departamento indicado: %s (indicação nº %d)\n\n%s\n", department, round, summary)
	fmt.Fprintf(&b, "Os documentos e os demais dados estão no sistema:\n%s\n", s.link(p.ID))
	return s.toSupervisors(ctx, q, kindFramingIndicated, fmt.Sprintf("framing-indicated:%s:%d", p.ID, round), p,
		fmt.Sprintf("Projeto %s encaminhado ao departamento %s", p.Code, department), b.String())
}

// notifyFramingRefused: o Revisor do Departamento recusou o enquadramento; o projeto voltou ao Analista.
func (s *Service) notifyFramingRefused(ctx context.Context, q *db.Queries, p *Project, round int, department, justification string) error {
	if r := []rune(justification); len(r) > maxRefusalInEmail {
		justification = string(r[:maxRefusalInEmail]) + "…"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "O Revisor do Departamento recusou o enquadramento do projeto %s.\n\n", p.Code)
	fmt.Fprintf(&b, "Título: %s\nInstituição: %s\nDepartamento recusado: %s (indicação nº %d)\n\n", p.Proposal.Title, p.Institution.Name, department, round)
	fmt.Fprintf(&b, "Justificativa:\n%s\n\n", justification)
	b.WriteString("O projeto voltou ao Analista, que indicará outro departamento.\n")
	fmt.Fprintf(&b, "Detalhes no sistema:\n%s\n", s.link(p.ID))
	return s.toSupervisors(ctx, q, kindFramingRefused, fmt.Sprintf("framing-refused:%s:%d", p.ID, round), p,
		fmt.Sprintf("Projeto %s: enquadramento recusado pelo departamento %s", p.Code, department), b.String())
}
