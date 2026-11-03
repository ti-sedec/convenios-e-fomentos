// Pacote rules guarda os parâmetros das regras de negócio (limites, prazos, datas) com vigência.
// Os valores mudam por norma ou por ano (teto atualizado por IPCA, data-limite eleitoral...); por
// isso ficam no banco, versionados, e nunca no código. As chaves conhecidas, o formato de cada uma e
// os valores iniciais são declarados aqui.
package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Chaves dos parâmetros.
const (
	KeyComplementDays     = "complement_days"
	KeyPriceReferenceDays = "price_reference_max_age_days"
	KeyElectoralDeadline  = "electoral_deadline"
	KeyLei12082           = "lei_12082"
	KeyIndirectCostTiers  = "indirect_costs_tiers"
	KeyStalledAfterDays   = "stalled_after_days"
)

// StalledAfterDays são os dias, por etapa de trabalho, a partir dos quais um projeto parado aparece na visão geral do Analista.
// Contam horas corridas com o responsável atual. "Aguardando proponente" não tem limite: já tem o prazo de complementação.
type StalledAfterDays struct {
	AwaitingChecklist int `json:"awaiting_checklist"` // fila de entrada, com a equipe
	InAnalysis        int `json:"in_analysis"`        // com o Analista
	Framing           int `json:"framing"`            // enquadramento pendente, com o departamento
	AwaitingOpinion   int `json:"awaiting_opinion"`   // enquadrado, aguardando parecer
}

// Lei12082 são os limites da Lei Estadual 12.082/2023 (shows e eventos musicais).
type Lei12082 struct {
	CeilingCents               int64 `json:"ceiling_cents"`                 // teto do repasse, atualizado anualmente por IPCA
	MinMunicipalCounterpartPct int   `json:"min_municipal_counterpart_pct"` // contrapartida mínima em convênio com município
	MaxFeesPct                 int   `json:"max_fees_pct"`                  // máximo do repasse em cachês artísticos
	MinRegionalFeesPct         int   `json:"min_regional_fees_pct"`         // mínimo dos cachês para artistas regionais
}

// IndirectCostTier é uma faixa de custos indiretos (IN Conjunta 002/2026, art. 36-A §1º).
type IndirectCostTier struct {
	UpToCents *int64 `json:"up_to_cents"` // nulo = sem limite superior (última faixa)
	MaxPct    int    `json:"max_pct"`
	CapCents  int64  `json:"cap_cents"`
}

// Spec descreve uma chave: o texto mostrado na administração, a validação do valor e o padrão inicial.
type Spec struct {
	Key         string
	Description string
	// Source é a norma que originou o parâmetro.
	Source   string
	Validate func(json.RawMessage) error
	Defaults []Default
}

// Default é um valor inicial, criado só quando a chave ainda não existe.
type Default struct {
	Value     string // JSON
	ValidFrom string // AAAA-MM-DD
	ValidTo   string // AAAA-MM-DD ou vazio
	Note      string
}

// Specs devolve as chaves conhecidas em ordem estável.
func Specs() []Spec {
	return []Spec{
		{
			Key: KeyComplementDays, Source: "Portaria 247/2024/SEDEC, art. 2º, III",
			Description: "Prazo, em dias corridos, para o proponente complementar a documentação devolvida pelo analista.",
			Validate:    intBetween(1, 30),
			Defaults:    []Default{{Value: `3`, ValidFrom: "2024-09-27", Note: "Portaria 247/2024/SEDEC"}},
		},
		{
			Key: KeyPriceReferenceDays, Source: "IN Conjunta 001/2016, art. 10-A §6º (redação da IN 002/2026)",
			Description: "Idade máxima, em dias, das referências de preço contada da apresentação do orçamento do plano de trabalho.",
			Validate:    intBetween(1, 365),
			Defaults:    []Default{{Value: `60`, ValidFrom: "2026-02-24", Note: "IN Conjunta 002/2026"}},
		},
		{
			Key: KeyElectoralDeadline, Source: "Portaria Conjunta SECEL/SEDEC (por ano eleitoral)",
			Description: "Data-limite de protocolo antes da vedação eleitoral para transferências voluntárias. Cadastre a vigência do ano eleitoral; sem valor vigente, não há data-limite.",
			Validate:    isDate,
			Defaults:    []Default{{Value: `"2026-05-15"`, ValidFrom: "2026-01-01", ValidTo: "2026-12-31", Note: "Portaria Conjunta 71/2026/SECEL/SEDEC"}},
		},
		{
			Key: KeyLei12082, Source: "Lei Estadual 12.082/2023",
			Description: "Limites para shows e eventos musicais: teto do repasse (atualizado por IPCA), contrapartida mínima de municípios, máximo em cachês e mínimo de cachês para artistas regionais.",
			Validate:    validateLei12082,
			Defaults: []Default{{Value: `{"ceiling_cents":60000000,"min_municipal_counterpart_pct":10,"max_fees_pct":50,"min_regional_fees_pct":30}`,
				ValidFrom: "2023-04-18", Note: "Lei 12.082/2023, art. 2º (R$ 600.000,00)"}},
		},
		{
			Key: KeyIndirectCostTiers, Source: "IN Conjunta 001/2016, art. 36-A §1º (IN 002/2026)",
			Description: "Faixas de custos indiretos do Termo de Fomento: percentual máximo e teto em reais por faixa do valor total da parceria.",
			Validate:    validateIndirectTiers,
			Defaults: []Default{{Value: `[{"up_to_cents":10000000,"max_pct":20,"cap_cents":1500000},{"up_to_cents":50000000,"max_pct":15,"cap_cents":5000000},{"up_to_cents":null,"max_pct":10,"cap_cents":20000000}]`,
				ValidFrom: "2026-02-24", Note: "IN Conjunta 002/2026, art. 36-A"}},
		},
		{
			Key: KeyStalledAfterDays, Source: "Decisão da equipe (visão geral do Analista)",
			Description: "Dias, por etapa, a partir dos quais um projeto é considerado parado: aguardando análise (awaiting_checklist), em análise (in_analysis), em enquadramento (framing) e aguardando parecer (awaiting_opinion). Contam dias corridos com o responsável atual; aguardando o proponente não tem limite, pois já tem o prazo de complementação.",
			Validate:    validateStalledAfterDays,
			Defaults: []Default{{Value: `{"awaiting_checklist":2,"in_analysis":7,"framing":5,"awaiting_opinion":15}`,
				ValidFrom: "2026-01-01", Note: "Ponto de partida; o Supervisor ajusta conforme a rotina da equipe"}},
		},
	}
}

// SpecFor devolve a especificação da chave.
func SpecFor(key string) (Spec, bool) {
	for _, s := range Specs() {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

var errBadValue = errors.New("valor inválido")

func intBetween(lo, hi int) func(json.RawMessage) error {
	return func(raw json.RawMessage) error {
		var n int
		if err := strictUnmarshal(raw, &n); err != nil || n < lo || n > hi {
			return fmt.Errorf("informe um número inteiro entre %d e %d", lo, hi)
		}
		return nil
	}
}

func isDate(raw json.RawMessage) error {
	var s string
	if err := strictUnmarshal(raw, &s); err != nil {
		return errors.New("informe uma data no formato AAAA-MM-DD")
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return errors.New("informe uma data válida no formato AAAA-MM-DD")
	}
	return nil
}

func pct(name string, v int) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("%s deve estar entre 0 e 100", name)
	}
	return nil
}

func validateLei12082(raw json.RawMessage) error {
	var v Lei12082
	if err := strictUnmarshal(raw, &v); err != nil {
		return errors.New("informe ceiling_cents, min_municipal_counterpart_pct, max_fees_pct e min_regional_fees_pct")
	}
	if v.CeilingCents <= 0 {
		return errors.New("ceiling_cents deve ser maior que zero")
	}
	for _, c := range []struct {
		name string
		v    int
	}{{"min_municipal_counterpart_pct", v.MinMunicipalCounterpartPct}, {"max_fees_pct", v.MaxFeesPct}, {"min_regional_fees_pct", v.MinRegionalFeesPct}} {
		if err := pct(c.name, c.v); err != nil {
			return err
		}
	}
	return nil
}

func validateIndirectTiers(raw json.RawMessage) error {
	var tiers []IndirectCostTier
	if err := strictUnmarshal(raw, &tiers); err != nil || len(tiers) == 0 {
		return errors.New("informe a lista de faixas com up_to_cents, max_pct e cap_cents")
	}
	var prev int64
	for i, t := range tiers {
		if err := pct("max_pct", t.MaxPct); err != nil {
			return err
		}
		if t.CapCents <= 0 {
			return fmt.Errorf("faixa %d: cap_cents deve ser maior que zero", i+1)
		}
		last := i == len(tiers)-1
		switch {
		case last && t.UpToCents != nil:
			return errors.New("a última faixa não tem limite superior (up_to_cents nulo)")
		case !last && t.UpToCents == nil:
			return fmt.Errorf("faixa %d: só a última faixa pode ficar sem limite superior", i+1)
		case !last && *t.UpToCents <= prev:
			return fmt.Errorf("faixa %d: up_to_cents deve ser maior que o da faixa anterior", i+1)
		}
		if t.UpToCents != nil {
			prev = *t.UpToCents
		}
	}
	return nil
}

func validateStalledAfterDays(raw json.RawMessage) error {
	var v StalledAfterDays
	if err := strictUnmarshal(raw, &v); err != nil {
		return errors.New("informe awaiting_checklist, in_analysis, framing e awaiting_opinion, em dias")
	}
	for _, c := range []struct {
		name string
		v    int
	}{{"awaiting_checklist", v.AwaitingChecklist}, {"in_analysis", v.InAnalysis}, {"framing", v.Framing}, {"awaiting_opinion", v.AwaitingOpinion}} {
		if c.v < 1 || c.v > 365 {
			return fmt.Errorf("%s deve ser um número inteiro de dias entre 1 e 365", c.name)
		}
	}
	return nil
}

// strictUnmarshal rejeita campos desconhecidos e conteúdo extra, para que o valor guardado tenha só o formato esperado.
func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errBadValue
	}
	return nil
}
