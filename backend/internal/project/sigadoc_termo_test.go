package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTermoFieldsUsaOsNomesDaEntrevistaDoModelo(t *testing.T) {
	p := &Project{Code: "CF-2026-0001"}
	p.Institution.Name = "Associação Exemplo"
	p.Institution.CNPJ = "11222333000181"
	p.Institution.Instrument = InstrumentFomento
	p.Institution.Phone = "65999990000"
	p.Institution.Email = "contato@exemplo.org"
	p.Proposal.Title = "Projeto Esporte"

	f := TermoFields(p)
	assert.Equal(t, "Associação Exemplo", f["docGovMT_interessado_nome"])
	assert.Equal(t, "11.222.333/0001-81", f["docGovMT_cpf-cnpj"])
	assert.Equal(t, "(65) 99999-0000", f["docGovMT_telefone"])
	assert.Equal(t, "contato@exemplo.org", f["docGovMT_email"])
	assert.Equal(t, "Solicitação de Termo de Fomento CF-2026-0001: Projeto Esporte", f["docGovMT_resumoAssunto"])
	assert.NotContains(t, f, "docGovMT_atendimento", "o projeto chega pelo sistema: o campo, opcional, fica em branco")
	assert.Equal(t, "interessado", f["auxGovMT_interessadoProcurador"])
	assert.Equal(t, "Nao", f["docGovMT_anexoFisico"])
	assert.Equal(t, "não", f["auxGovMT_deveJuntar"], "o valor do rádio no SIGADOC leva acento")
	assert.Empty(t, termoMissing(f))
}

func TestTermoFieldsCaiNoContatoDoProponente(t *testing.T) {
	p := &Project{}
	p.Institution.Name, p.Institution.CNPJ = "Associação Exemplo", "11222333000181"
	p.Proponent.Phones = []Phone{{Number: "6533334444", Type: PhoneLandline}}
	p.Proponent.Emails = []Email{{Address: "pessoa@exemplo.org"}}

	f := TermoFields(p)
	assert.Equal(t, "(65) 3333-4444", f["docGovMT_telefone"])
	assert.Equal(t, "pessoa@exemplo.org", f["docGovMT_email"])
}

func TestTermoMissingPedeOsObrigatorios(t *testing.T) {
	f := TermoFields(&Project{})
	assert.Equal(t, "docGovMT_interessado_nome", termoMissing(f))
	f["docGovMT_interessado_nome"], f["docGovMT_cpf-cnpj"], f["docGovMT_telefone"], f["docGovMT_resumoAssunto"] = "A", "B", "C", "D"
	assert.Equal(t, "docGovMT_email", termoMissing(f))
	f["docGovMT_email"] = " x@y.z "
	assert.Empty(t, termoMissing(f))
}
