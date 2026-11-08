package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Estado civil, como entra nas declarações. O CPF identifica a pessoa: não se pede RG.
const (
	MaritalSingle      = "single"
	MaritalMarried     = "married"
	MaritalStableUnion = "stable_union"
	MaritalSeparated   = "separated"
	MaritalDivorced    = "divorced"
	MaritalWidowed     = "widowed"

	maxNationality = 60
)

// maritalLabels é o texto do estado civil nas declarações; a flexão "(a)" evita supor o gênero de quem assina.
var maritalLabels = map[string]string{
	MaritalSingle: "solteiro(a)", MaritalMarried: "casado(a)", MaritalStableUnion: "em união estável",
	MaritalSeparated: "separado(a)", MaritalDivorced: "divorciado(a)", MaritalWidowed: "viúvo(a)",
}

// PersonalData são os dados pessoais de quem assina as declarações: nacionalidade, estado civil e endereço residencial.
type PersonalData struct {
	Nationality      string `json:"nationality"`
	MaritalStatus    string `json:"maritalStatus"`
	Zip              string `json:"zip"`
	Street           string `json:"street"`
	Number           string `json:"number"`
	Complement       string `json:"complement"`
	District         string `json:"district"`
	UF               string `json:"uf"`
	MunicipalityIBGE string `json:"municipalityIbge"`
}

// normalizePersonal valida e normaliza os dados pessoais. prefix ("personal" ou "legalRepPersonal") compõe o nome dos campos nos erros.
func normalizePersonal(ctx context.Context, q *db.Queries, in PersonalData, prefix string, p *problems) ([]byte, PersonalData) {
	f := func(name string) string { return prefix + "." + name }
	out := PersonalData{
		Nationality: text(in.Nationality), Street: text(in.Street), Number: text(in.Number),
		Complement: text(in.Complement), District: text(in.District),
	}
	if _, ok := maritalLabels[in.MaritalStatus]; ok || in.MaritalStatus == "" {
		out.MaritalStatus = in.MaritalStatus
	} else {
		p.add(f("maritalStatus"), "Escolha um estado civil da lista.")
	}
	if z := digits(in.Zip); z != "" {
		if len(z) != 8 {
			p.add(f("zip"), "CEP deve ter 8 dígitos.")
		}
		out.Zip = z
	}
	for _, c := range []struct {
		v, field string
		max      int
	}{{out.Nationality, "nationality", maxNationality}, {out.Street, "street", maxStreet}, {out.Number, "number", maxNumber},
		{out.Complement, "complement", maxComplement}, {out.District, "district", maxDistrict}} {
		if tooLong(c.v, c.max) {
			p.add(f(c.field), fmt.Sprintf("Use até %d caracteres.", c.max))
		}
	}
	uf, ibge := place(ctx, q, in.UF, in.MunicipalityIBGE, f("uf"), f("municipalityIbge"), p)
	out.UF, out.MunicipalityIBGE = uf.String, str(ibge)
	b, _ := json.Marshal(out)
	return b, out
}

// personalOf lê os dados pessoais guardados; o que não puder ser lido vira vazio.
func personalOf(b []byte) PersonalData {
	var d PersonalData
	_ = json.Unmarshal(b, &d)
	return d
}

// homeAddress escreve o endereço residencial como nas declarações: "Rua A, nº 10, Centro – Cuiabá/MT". city é "Município/UF".
func homeAddress(d PersonalData, city string) string {
	street := d.Street + ", nº " + d.Number
	if d.Complement != "" {
		street += ", " + d.Complement
	}
	return fmt.Sprintf("%s, Bairro %s – %s", street, d.District, city)
}

// personalIssues lista o que falta nos dados pessoais para escrever "nacionalidade, estado civil, residente e domiciliado em…".
// who diz de quem são os dados ("do representante legal"), para a mensagem apontar a pessoa certa.
func personalIssues(d PersonalData, who, field string) []Issue {
	var out []Issue
	add := func(sub, code, what string) {
		out = append(out, declIssue(field+"."+sub, "decl_personal_"+code, fmt.Sprintf("Informe %s %s em Proponente.", what, who)))
	}
	if d.Nationality == "" {
		add("nationality", "nationality", "a nacionalidade")
	}
	if d.MaritalStatus == "" {
		add("maritalStatus", "marital", "o estado civil")
	}
	// Uma pendência por parte do endereço: quem vê rua, bairro e cidade preenchidos precisa saber que só o número falta.
	if strings.TrimSpace(d.Street) == "" {
		add("street", "street", "a rua do endereço residencial")
	}
	if strings.TrimSpace(d.Number) == "" {
		out = append(out, declIssue(field+".number", "decl_personal_number",
			fmt.Sprintf("Informe o número do endereço residencial %s em Proponente (use S/N se não houver).", who)))
	}
	if strings.TrimSpace(d.District) == "" {
		add("district", "district", "o bairro do endereço residencial")
	}
	if d.MunicipalityIBGE == "" {
		add("municipalityIbge", "city", "o município do endereço residencial")
	}
	return out
}
