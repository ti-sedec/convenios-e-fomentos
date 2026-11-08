package project

import (
	"fmt"
	"strings"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
)

// Modelos de declaração que dependem dos dados pessoais de quem assina. Os textos são os dos modelos oficiais da SEDEC, com duas
// diferenças deliberadas, a aprovar pela SEDEC (docs/planejamento-declaracoes.md): sem o RG ("portador da Cédula de Identidade
// nº … e do CPF nº …" vira "portador(a) do CPF nº …"; o CPF identifica a pessoa) e com a flexão "(a)" onde o modelo supõe o gênero.

func init() {
	declarationSpecs["declaracao_gratuidade"] = declSpec{
		code: "declaracao_gratuidade", name: "Declaração de gratuidade", title: "DECLARAÇÃO DE GRATUIDADE", version: "2026-10",
		annex: byInstrument("Anexo VII", "Anexo VI"),
		check: func(p Project, e declEnv) []Issue {
			out := identificationIssues(p, e)
			if strings.TrimSpace(p.Proposal.Title) == "" {
				out = append(out, declIssue("title", "decl_title", "Informe o título do projeto em Proposta: ele é o nome da programação."))
			}
			if len(p.Proposal.ExecutionPeriods) == 0 {
				out = append(out, declIssue("executionPeriods", "decl_execution_place", "Informe o município de execução em Proposta."))
				out = append(out, declIssue("executionPeriods", "decl_period", "Informe o início e o término da execução em Proposta."))
			}
			for i, e := range p.Proposal.ExecutionPeriods {
				if e.MunicipalityIBGE == "" {
					out = append(out, declIssue(fmt.Sprintf("executionPeriods[%d].municipalityIbge", i), "decl_execution_place", fmt.Sprintf("Informe o município de execução do período %d em Proposta.", i+1)))
				}
				if e.StartDate == "" || e.EndDate == "" {
					out = append(out, declIssue(fmt.Sprintf("executionPeriods[%d].startDate", i), "decl_period", fmt.Sprintf("Informe o início e o término do período %d em Proposta.", i+1)))
				}
			}
			return out
		},
		body: func(p Project, e declEnv) []string {
			return []string{fmt.Sprintf("%s, DECLARO que a Programação %s a ser realizada %s, terá entrada gratuita, "+
				"sob pena de responsabilidade civil, penal e administrativa.",
				identification(p, e), p.Proposal.Title, executionSchedule(p.Proposal.ExecutionPeriods))}
		},
	}
	declarationSpecs["declaracao_publicidade"] = declSpec{
		code: "declaracao_publicidade", name: "Declaração de publicidade da parceria", title: "DECLARAÇÃO DE PUBLICIDADE DA PARCERIA", version: "2026-10",
		annex: byInstrument("Anexo VIII", "Anexo VII"),
		check: func(p Project, e declEnv) []Issue {
			return personalIssues(signerPersonal(p), whoSigns(p), personalField(p))
		},
		body: func(p Project, e declEnv) []string {
			return []string{fmt.Sprintf("%s, COMPROMETO-ME a manter um perfil na Plataforma Mapas, devidamente atualizado com as ações promovidas no museu, "+
				"publicar na internet e em locais visíveis na sede da organização a parceria celebrada com a Secretaria de Estado de Desenvolvimento "+
				"Econômico/SEDEC, com todas as informações dispostas no art. 11 da Lei 13.019/2014, sob pena de responsabilidade civil, penal e administrativa.",
				signerIntro(p, e))}
		},
	}
	declarationSpecs["declaracao_compatibilidade_precos"] = declSpec{
		code: "declaracao_compatibilidade_precos", name: "Declaração de compatibilidade de preços", title: "DECLARAÇÃO DE COMPATIBILIDADE DE PREÇOS", version: "2026-10",
		annex: byInstrument("Anexo V", "Anexo IV"),
		check: func(p Project, _ declEnv) []Issue {
			if strings.TrimSpace(p.Proposal.SigconNumber) == "" {
				return []Issue{declIssue("sigconNumber", "decl_sigcon", "Informe o número da proposta no SIGCon em Proposta.")}
			}
			return nil
		},
		body: func(p Project, _ declEnv) []string {
			return []string{fmt.Sprintf("Declaro para os devidos fins, perante a Secretaria de Estado de Desenvolvimento Econômico/SEDEC (SEDEC-MT), "+
				"que as propostas orçamentárias apresentadas por %s, referentes à proposta do %s n.º %s, contemplam preços compatíveis aos "+
				"praticados no mercado local, sob pena de responsabilidade civil, penal e administrativa.",
				p.Institution.Name, instrumentName(p.Institution.Instrument), p.Proposal.SigconNumber)}
		},
	}
}

// byInstrument escolhe o número do anexo: o primeiro vale para a prefeitura (convênio), o segundo para a OSC (termo de fomento).
func byInstrument(convenio, fomento string) func(string) string {
	return func(instrument string) string {
		if instrument == InstrumentFomento {
			return fomento
		}
		return convenio
	}
}

func instrumentName(instrument string) string {
	if instrument == InstrumentFomento {
		return "termo de fomento"
	}
	return "convênio"
}

// whoSigns diz de quem são os dados pessoais, para as mensagens de pendência apontarem a pessoa certa.
func whoSigns(p Project) string {
	if p.Proponent.IsLegalRepresentative != nil && !*p.Proponent.IsLegalRepresentative {
		return "do representante legal"
	}
	return "de quem assina"
}

// personalField é o campo do projeto que guarda os dados pessoais de quem assina: os do proponente ou os do representante legal.
func personalField(p Project) string {
	if p.Proponent.IsLegalRepresentative != nil && !*p.Proponent.IsLegalRepresentative {
		return "legalRepPersonal"
	}
	return "personal"
}

// signerIntro é "Eu, NOME, NACIONALIDADE, ESTADO CIVIL, portador(a) do CPF nº …, residente e domiciliado em ENDEREÇO".
func signerIntro(p Project, e declEnv) string {
	name, _, _ := signerOf(p)
	d := signerPersonal(p)
	return fmt.Sprintf("Eu, %s, %s, %s, portador(a) do CPF nº %s, residente e domiciliado em %s",
		name, d.Nationality, maritalLabels[d.MaritalStatus], cpf.Format(e.SignerCPF), homeAddress(d, e.HomePlace))
}

// identification acrescenta a quem o signatário representa: "…, na qualidade de representante da INSTITUIÇÃO, CNPJ nº …".
func identification(p Project, e declEnv) string {
	return fmt.Sprintf("%s, na qualidade de representante da %s, CNPJ nº %s", signerIntro(p, e), p.Institution.Name, formatCNPJ(p.Institution.CNPJ))
}

// identificationIssues são as pendências de "identification": dados pessoais, instituição e CNPJ.
func identificationIssues(p Project, e declEnv) []Issue {
	out := personalIssues(signerPersonal(p), whoSigns(p), personalField(p))
	if strings.TrimSpace(p.Institution.CNPJ) == "" {
		out = append(out, declIssue("cnpj", "decl_cnpj", "Informe o CNPJ da instituição em Instituição."))
	}
	return out
}

// executionPlaces escreve os municípios de execução: "no município de A" ou "nos municípios de A, B e C".
func executionPlaces(ms []Municipality) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	switch len(names) {
	case 0:
		return "no município de —"
	case 1:
		return "no município de " + names[0]
	}
	return "nos municípios de " + strings.Join(names[:len(names)-1], ", ") + " e " + names[len(names)-1]
}

// executionPeriod escreve o período de uma linha de execução ("no dia 01/11/2026" ou "no período de 01/11/2026 a 01/05/2027"). O
// cadastro não tem a data do evento; o período previsto é o que há mais próximo e a SEDEC precisa confirmar
// (docs/planejamento-declaracoes.md).
func executionPeriod(start, end string) string {
	if start == "" || end == "" {
		return "no período de — a —"
	}
	if start == end {
		return "no dia " + formatDateBR(start)
	}
	return "no período de " + formatDateBR(start) + " a " + formatDateBR(end)
}

// executionSchedule escreve onde e quando o projeto será executado. Com uma linha, é o texto de sempre ("no município de A, no
// período de …"). Com várias, enumera cada período com o seu local, e o detalhe do local, quando houver, vai entre parênteses.
func executionSchedule(periods []ExecutionPeriod) string {
	place := func(e ExecutionPeriod) string {
		if e.Municipality == nil {
			return executionPlaces(nil)
		}
		return executionPlaces([]Municipality{*e.Municipality})
	}
	switch len(periods) {
	case 0:
		return executionPlaces(nil) + ", " + executionPeriod("", "")
	case 1:
		return place(periods[0]) + ", " + executionPeriod(periods[0].StartDate, periods[0].EndDate)
	}
	items := make([]string, len(periods))
	for i, e := range periods {
		where := place(e)
		if e.PlaceDetail != "" {
			where += " (" + e.PlaceDetail + ")"
		}
		items[i] = where + ", " + executionPeriod(e.StartDate, e.EndDate)
	}
	return "nos seguintes locais e períodos: " + strings.Join(items, "; ")
}

// formatDateBR converte AAAA-MM-DD em DD/MM/AAAA.
func formatDateBR(iso string) string {
	y, rest, ok := strings.Cut(iso, "-")
	m, d, ok2 := strings.Cut(rest, "-")
	if !ok || !ok2 {
		return iso
	}
	return d + "/" + m + "/" + y
}
