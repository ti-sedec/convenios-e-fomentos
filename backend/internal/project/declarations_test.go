package project

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

const testCPF = "12345678909"

func ptrTrue() *bool  { v := true; return &v }
func ptrFalse() *bool { v := false; return &v }

func municipalConvenio() Project {
	var p Project
	p.Institution = InstitutionSection{Instrument: InstrumentConvenio, Name: "Prefeitura Municipal de Sorriso", EntityTypeCode: "municipal_government"}
	p.Proponent = ProponentSection{Name: "Maria da Silva", IsLegalRepresentative: ptrTrue(), Contact: Contact{JobTitle: "Prefeita"}}
	p.Funding = FundingSection{CounterpartKind: CounterpartFinancial, CounterpartCents: 1234567}
	return p
}

func issueCodes(d GeneratedDeclaration) []string {
	var out []string
	for _, i := range d.Issues {
		out = append(out, i.Code)
	}
	return out
}

func TestContrapartidaUsaOTextoOficialComOValorDoCadastro(t *testing.T) {
	d := buildDeclaration(declarationSpecs["declaracao_contrapartida"], municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})

	assert.True(t, d.Ready, d.Issues)
	assert.Equal(t, "Anexo IV", d.Annex)
	require.Len(t, d.Paragraphs, 1)
	assert.Contains(t, d.Paragraphs[0], "DECLARO que este município depositará o valor de R$ 12.345,67, na conta exclusiva para execução deste objeto.")
	assert.Contains(t, d.Paragraphs[0], "Instrução Normativa SEPLAN/SEFAZ/CGE nº 001/2015, de 23 de fevereiro de 2015")
	assert.Equal(t, "Maria da Silva", d.SignerName)
	assert.Equal(t, "Prefeita", d.SignerRole)
}

func TestContrapartidaBloqueiaOQueOModeloNaoCobre(t *testing.T) {
	spec := declarationSpecs["declaracao_contrapartida"]

	inKind := municipalConvenio()
	inKind.Funding.CounterpartKind = CounterpartInKind
	assert.Contains(t, issueCodes(buildDeclaration(spec, inKind, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})), "decl_counterpart_not_financial", "o modelo declara depósito: não serve para bens e serviços")

	noValue := municipalConvenio()
	noValue.Funding.CounterpartCents = 0
	assert.Contains(t, issueCodes(buildDeclaration(spec, noValue, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})), "decl_counterpart_value")

	osc := municipalConvenio()
	osc.Institution.EntityTypeCode = "osc"
	assert.Contains(t, issueCodes(buildDeclaration(spec, osc, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})), "decl_municipality_only", "o modelo diz \"este município\"")
}

func TestSignatarioEORepresentanteLegal(t *testing.T) {
	spec := declarationSpecs["declaracao_nao_duplicidade"]

	// Quem preenche não é o representante: assina quem foi informado, nunca o proponente.
	p := municipalConvenio()
	p.Proponent.IsLegalRepresentative = ptrFalse()
	p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle = "João Souza", "Prefeito"
	d := buildDeclaration(spec, p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	assert.True(t, d.Ready, d.Issues)
	assert.Equal(t, "João Souza", d.SignerName)
	assert.Equal(t, "Prefeito", d.SignerRole)

	// Sem a informação de quem é o representante, ou sem o nome dele, não emite.
	p.Proponent.IsLegalRepresentative = nil
	assert.Contains(t, issueCodes(buildDeclaration(spec, p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})), "decl_signer_unknown")
	p.Proponent.IsLegalRepresentative = ptrFalse()
	p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle = "", ""
	assert.ElementsMatch(t, []string{"decl_signer_name", "decl_signer_role"}, issueCodes(buildDeclaration(spec, p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})))
}

func TestNaoDuplicidadeEOModeloOficialESemMunicipioNaoEmite(t *testing.T) {
	spec := declarationSpecs["declaracao_nao_duplicidade"]
	p := municipalConvenio()

	d := buildDeclaration(spec, p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	assert.True(t, d.Ready)
	assert.Equal(t, "Anexo VI", d.Annex)
	assert.Contains(t, d.Paragraphs[0], "não possui parceria (s) vigentes (s), celebrado (s) com qualquer órgão da União/Estado ou Município")
	assert.Contains(t, d.Paragraphs[0], "sob pena de responsabilidade civil, penal e administrativa.")

	p.Institution.Instrument = InstrumentFomento
	assert.Equal(t, "Anexo V", buildDeclaration(spec, p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF}).Annex, "a numeração do anexo muda entre prefeitura e OSC")

	d = buildDeclaration(spec, p, declEnv{Place: "", SignerCPF: testCPF})
	assert.False(t, d.Ready)
	assert.Contains(t, issueCodes(d), "decl_place")
}

func TestParamsSHA256MudaQuandoOConteudoMuda(t *testing.T) {
	spec := declarationSpecs["declaracao_contrapartida"]
	base := buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})

	same := buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	assert.Equal(t, base.ParamsSHA256, same.ParamsSHA256, "mesmo cadastro, mesmo resumo")

	other := municipalConvenio()
	other.Funding.CounterpartCents++
	assert.NotEqual(t, base.ParamsSHA256, buildDeclaration(spec, other, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF}).ParamsSHA256, "valor diferente invalida a declaração emitida")

	other = municipalConvenio()
	other.Proponent.Name = "Outra Pessoa"
	assert.NotEqual(t, base.ParamsSHA256, buildDeclaration(spec, other, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF}).ParamsSHA256, "signatário diferente também")
	assert.NotEqual(t, base.ParamsSHA256, buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Cuiabá/MT", SignerCPF: testCPF}).ParamsSHA256, "e o local")
}

func TestSignatarioSemCPFNaoEmite(t *testing.T) {
	spec := declarationSpecs["declaracao_nao_duplicidade"]
	assert.Contains(t, issueCodes(buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: ""})), "decl_signer_cpf")
	assert.Contains(t, issueCodes(buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: "123"})), "decl_signer_cpf")

	// Trocar quem assina muda o resumo, mesmo com o mesmo nome e cargo.
	a := buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	b := buildDeclaration(spec, municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: "98765432100"})
	assert.NotEqual(t, a.ParamsSHA256, b.ParamsSHA256)
}

func TestRenderDeclarationGeraPDFComOTextoOficial(t *testing.T) {
	d := buildDeclaration(declarationSpecs["declaracao_contrapartida"], municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	at := time.Date(2026, time.October, 24, 15, 0, 0, 0, time.UTC)

	pdf, spot, err := renderDeclaration(d, at)
	require.NoError(t, err)
	assert.Equal(t, 0, spot.Page)
	require.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
	body := string(pdf)
	for _, want := range []string{"ANEXO IV", "DECLARACAO DE CONTRAPARTIDA", "Sorriso/MT, 24 de outubro de 2026.", "Maria da Silva", "Prefeita"} {
		// A fonte padrão é Latin-1: acentos viram bytes únicos. Compara sem eles os trechos acentuados.
		assert.Contains(t, stripAccents(body), stripAccents(want), want)
	}
}

// A assinatura fica centralizada na página e no espaço entre a data e o nome: nunca em cima do título.
func TestSignatureSpotFicaNoEspacoDaAssinatura(t *testing.T) {
	// Layout medido: com o texto mais curto (nao duplicidade) a data termina perto de 107 mm do topo.
	spot := signatureSpotAt(0, 107)
	assert.Equal(t, 186, spot.X) // carimbo de 217 pt centralizado em 595 pt
	assert.InDelta(t, (107+14)*72/25.4, float64(spot.Y), 3)
	assert.Equal(t, 0, spot.Page)
}

func TestRenderDeclarationPosicaoAcompanhaOTamanhoDoTexto(t *testing.T) {
	short := buildDeclaration(declarationSpecs["declaracao_nao_duplicidade"], municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	long := buildDeclaration(declarationSpecs["declaracao_contrapartida"], municipalConvenio(), declEnv{Place: "Sorriso/MT", SignerCPF: testCPF})
	_, a, err := renderDeclaration(short, time.Now())
	require.NoError(t, err)
	_, b, err := renderDeclaration(long, time.Now())
	require.NoError(t, err)
	assert.Less(t, a.Y, b.Y, "texto mais longo empurra a assinatura para baixo")
	for _, sp := range []signatureSpot{a, b} {
		assert.Greater(t, sp.Y, 250, "abaixo do título e do texto")
		assert.Less(t, sp.Y, 842-60, "dentro da página")
	}
}

func stripAccents(s string) string {
	return strings.NewReplacer("Ç", "C", "ç", "c", "Ã", "A", "ã", "a", "Á", "A", "á", "a", "É", "E", "é", "e", "Í", "I", "í", "i", "Ó", "O", "ó", "o", "Ú", "U", "ú", "u",
		"\xc7", "C", "\xe7", "c", "\xc3", "A", "\xe3", "a", "\xe1", "a", "\xe9", "e", "\xed", "i", "\xf3", "o", "\xfa", "u").Replace(s)
}

func TestLongDate(t *testing.T) {
	assert.Equal(t, "1 de março de 2027", longDate(time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)))
}

// signerWithPersonalData é um convênio municipal em que a Prefeita (representante legal) tem os dados pessoais preenchidos.
func signerWithPersonalData() (Project, declEnv) {
	p := municipalConvenio()
	p.Institution.CNPJ = "11222333000181"
	p.Proponent.Personal = PersonalData{Nationality: "brasileira", MaritalStatus: MaritalMarried, Street: "Rua das Flores", Number: "100",
		District: "Centro", UF: "MT", MunicipalityIBGE: "5107925"}
	p.Proposal = ProposalSection{Title: "Festival de Inverno", SigconNumber: "3216/2026", ExecutionPeriods: []ExecutionPeriod{
		{StartDate: "2026-11-01", EndDate: "2027-05-01", MunicipalityIBGE: "5107925", Municipality: &Municipality{IBGECode: "5107925", Name: "Sorriso", UF: "MT"}}}}
	return p, declEnv{Place: "Sorriso/MT", SignerCPF: testCPF, HomePlace: "Sorriso/MT"}
}

func TestGratuidadeUsaOsDadosDoCadastroSemRG(t *testing.T) {
	p, e := signerWithPersonalData()
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)

	assert.True(t, d.Ready, d.Issues)
	assert.Equal(t, "Anexo VII", d.Annex)
	require.Len(t, d.Paragraphs, 1)
	got := d.Paragraphs[0]
	assert.Contains(t, got, "Eu, Maria da Silva, brasileira, casado(a), portador(a) do CPF nº 123.456.789-09, residente e domiciliado em Rua das Flores, nº 100, Bairro Centro – Sorriso/MT,")
	assert.Contains(t, got, "na qualidade de representante da Prefeitura Municipal de Sorriso, CNPJ nº 11.222.333/0001-81, DECLARO que a Programação Festival de Inverno")
	assert.Contains(t, got, "a ser realizada no município de Sorriso, no período de 01/11/2026 a 01/05/2027, terá entrada gratuita, sob pena de responsabilidade civil, penal e administrativa.")
	assert.NotContains(t, got, "Identidade", "o CPF identifica a pessoa: não há RG")
}

func TestGratuidadeComUmDia(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proposal.ExecutionPeriods[0].EndDate = p.Proposal.ExecutionPeriods[0].StartDate
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)
	assert.Contains(t, d.Paragraphs[0], "no município de Sorriso, no dia 01/11/2026,")
}

func TestGratuidadeEnumeraTodosOsPeriodosELocais(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proposal.ExecutionPeriods = []ExecutionPeriod{
		{StartDate: "2026-11-01", EndDate: "2026-11-03", MunicipalityIBGE: "5107925", Municipality: &Municipality{Name: "Sorriso"}, PlaceDetail: "Ginásio"},
		{StartDate: "2026-11-10", EndDate: "2026-11-10", MunicipalityIBGE: "5107909", Municipality: &Municipality{Name: "Sinop"}},
	}
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)
	assert.True(t, d.Ready, d.Issues)
	assert.Contains(t, d.Paragraphs[0], "a ser realizada nos seguintes locais e períodos: "+
		"no município de Sorriso (Ginásio), no período de 01/11/2026 a 03/11/2026; no município de Sinop, no dia 10/11/2026, terá entrada gratuita")
}

func TestGratuidadeExigeMunicipioEDatasEmCadaPeriodo(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proposal.ExecutionPeriods = append(p.Proposal.ExecutionPeriods, ExecutionPeriod{StartDate: "2026-12-01"})
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)
	assert.False(t, d.Ready)
	fields := map[string]string{}
	for _, i := range d.Issues {
		fields[i.Field] = i.Code
	}
	assert.Equal(t, "decl_execution_place", fields["executionPeriods[1].municipalityIbge"])
	assert.Equal(t, "decl_period", fields["executionPeriods[1].startDate"])
}

func TestPublicidadeEOTextoOficialComOsDadosDoSignatario(t *testing.T) {
	p, e := signerWithPersonalData()
	d := buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)

	assert.True(t, d.Ready, d.Issues)
	assert.Equal(t, "Anexo VIII", d.Annex)
	got := d.Paragraphs[0]
	assert.True(t, strings.HasPrefix(got, "Eu, Maria da Silva, brasileira, casado(a), portador(a) do CPF nº 123.456.789-09, residente e domiciliado em Rua das Flores, nº 100, Bairro Centro – Sorriso/MT, COMPROMETO-ME a manter um perfil na Plataforma Mapas"), got)
	assert.Contains(t, got, "com todas as informações dispostas no art. 11 da Lei 13.019/2014, sob pena de responsabilidade civil, penal e administrativa.")

	p.Institution.Instrument = InstrumentFomento
	assert.Equal(t, "Anexo VII", buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e).Annex)
}

func TestDadosPessoaisFaltandoBloqueiamGratuidadeEPublicidade(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proponent.Personal = PersonalData{}
	for _, code := range []string{"declaracao_gratuidade", "declaracao_publicidade"} {
		d := buildDeclaration(declarationSpecs[code], p, e)
		assert.False(t, d.Ready, code)
		assert.ElementsMatch(t, []string{"decl_personal_nationality", "decl_personal_marital", "decl_personal_street", "decl_personal_number", "decl_personal_district", "decl_personal_city"}, issueCodes(d), code)
		assert.Contains(t, d.Issues[0].Message, "de quem assina")
	}

	// Quando assina outra pessoa, os dados que contam são os dela, e a mensagem aponta o representante legal.
	p, e = signerWithPersonalData()
	p.Proponent.IsLegalRepresentative = ptrFalse()
	p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle = "João Souza", "Prefeito"
	d := buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)
	assert.False(t, d.Ready, "os dados da Maria não valem para o João")
	assert.Contains(t, d.Issues[0].Message, "do representante legal")
	p.Proponent.LegalRepPersonal = PersonalData{Nationality: "brasileiro", MaritalStatus: MaritalSingle, Street: "Av. Brasil", Number: "S/N", District: "Centro", MunicipalityIBGE: "5107925"}
	d = buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)
	assert.True(t, d.Ready, d.Issues)
	assert.Contains(t, d.Paragraphs[0], "Eu, João Souza, brasileiro, solteiro(a),")
}

// Rua, bairro e município preenchidos e só o número faltando: a pendência aponta o número, no campo certo.
func TestPendenciaDoEnderecoApontaSoOQueFalta(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proponent.Personal.Number = "  "
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)
	assert.Equal(t, []string{"decl_personal_number"}, issueCodes(d))
	assert.Equal(t, "personal.number", d.Issues[0].Field)
	assert.Contains(t, d.Issues[0].Message, "S/N")

	p, e = signerWithPersonalData()
	p.Proponent.IsLegalRepresentative = ptrFalse()
	p.Proponent.LegalRepName, p.Proponent.LegalRepJobTitle = "João Souza", "Prefeito"
	p.Proponent.LegalRepPersonal = PersonalData{Nationality: "brasileiro", MaritalStatus: MaritalSingle, Street: "Av. Brasil", District: "Centro", MunicipalityIBGE: "5107925"}
	d = buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)
	assert.Equal(t, []string{"decl_personal_number"}, issueCodes(d))
	assert.Equal(t, "legalRepPersonal.number", d.Issues[0].Field, "os dados do representante legal são outro campo")
	assert.Contains(t, d.Issues[0].Message, "do representante legal")
}

func TestGratuidadeExigeProjetoEInstituicaoCompletos(t *testing.T) {
	p, e := signerWithPersonalData()
	p.Proposal = ProposalSection{}
	p.Institution.CNPJ = ""
	d := buildDeclaration(declarationSpecs["declaracao_gratuidade"], p, e)
	assert.ElementsMatch(t, []string{"decl_title", "decl_execution_place", "decl_period", "decl_cnpj"}, issueCodes(d))
}

func TestCompatibilidadeDePrecosCitaAProposta(t *testing.T) {
	p, e := signerWithPersonalData()
	d := buildDeclaration(declarationSpecs["declaracao_compatibilidade_precos"], p, e)

	assert.True(t, d.Ready, d.Issues)
	assert.Equal(t, "Anexo V", d.Annex)
	assert.Equal(t, "Declaro para os devidos fins, perante a Secretaria de Estado de Desenvolvimento Econômico/SEDEC (SEDEC-MT), que as propostas orçamentárias "+
		"apresentadas por Prefeitura Municipal de Sorriso, referentes à proposta do convênio n.º 3216/2026, contemplam preços compatíveis aos praticados no "+
		"mercado local, sob pena de responsabilidade civil, penal e administrativa.", d.Paragraphs[0])

	p.Institution.Instrument = InstrumentFomento
	d = buildDeclaration(declarationSpecs["declaracao_compatibilidade_precos"], p, e)
	assert.Equal(t, "Anexo IV", d.Annex)
	assert.Contains(t, d.Paragraphs[0], "referentes à proposta do termo de fomento n.º 3216/2026")

	p.Proposal.SigconNumber = ""
	assert.Contains(t, issueCodes(buildDeclaration(declarationSpecs["declaracao_compatibilidade_precos"], p, e)), "decl_sigcon")
	// Não depende dos dados pessoais.
	p.Proponent.Personal = PersonalData{}
	p.Proposal.SigconNumber = "1/2026"
	assert.True(t, buildDeclaration(declarationSpecs["declaracao_compatibilidade_precos"], p, e).Ready)
}

func TestEquipeVeOCPFMascaradoNoTextoMasOResumoENoMesmo(t *testing.T) {
	p, e := signerWithPersonalData()
	d := buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)
	hash := d.ParamsSHA256

	d.hideCPF = true
	d.redact()
	assert.NotContains(t, d.Paragraphs[0], "123.456.789-09")
	assert.Contains(t, d.Paragraphs[0], "portador(a) do CPF nº ***.456.789-**")
	assert.Equal(t, hash, d.ParamsSHA256, "o resumo não depende de quem consulta")

	other := buildDeclaration(declarationSpecs["declaracao_publicidade"], p, e)
	assert.Equal(t, hash, other.ParamsSHA256)
	assert.Contains(t, other.Paragraphs[0], "123.456.789-09", "quem edita vê o texto completo")
}

func TestStaleReasonsDizOQueMudou(t *testing.T) {
	p, e := signerWithPersonalData()
	spec := declarationSpecs["declaracao_gratuidade"]
	issued := buildDeclaration(spec, p, e)
	rec := db.ProjectDeclaration{
		RequirementCode: spec.code, TemplateVersion: spec.version, ParamsSha256: issued.ParamsSHA256, SignerName: issued.SignerName,
		SignerRole: issued.SignerRole, SignerCpf: issued.signerCPF, RenderedText: strings.Join(issued.Paragraphs, "\n\n"),
	}
	assert.Empty(t, staleReasons(rec, spec, issued), "nada mudou")

	cases := map[string]struct {
		change func(*Project, *declEnv)
		want   []string
	}{
		"cargo":    {func(p *Project, _ *declEnv) { p.Proponent.JobTitle = "Prefeito" }, []string{StaleSignerRole}},
		"nome":     {func(p *Project, _ *declEnv) { p.Proponent.Name = "Maria de Souza" }, []string{StaleSignerName, StaleText}},
		"texto":    {func(p *Project, _ *declEnv) { p.Proposal.Title = "Festival de Verão" }, []string{StaleText}},
		"endereço": {func(p *Project, e *declEnv) { p.Proponent.Personal.Number = "200" }, []string{StaleText}},
		"local":    {func(_ *Project, e *declEnv) { e.Place = "Cuiabá/MT" }, []string{StalePlace}},
		"cpf":      {func(_ *Project, e *declEnv) { e.SignerCPF = "98765432100" }, []string{StaleSignerCPF, StaleText}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p2, e2 := signerWithPersonalData()
			c.change(&p2, &e2)
			now := buildDeclaration(spec, p2, e2)
			require.NotEqual(t, issued.ParamsSHA256, now.ParamsSHA256)
			assert.ElementsMatch(t, c.want, staleReasons(rec, spec, now))
		})
	}

	t.Run("modelo atualizado", func(t *testing.T) {
		old := rec
		old.TemplateVersion = "2020-01"
		old.ParamsSha256 = "outro"
		assert.Contains(t, staleReasons(old, spec, issued), StaleTemplate)
	})
}
