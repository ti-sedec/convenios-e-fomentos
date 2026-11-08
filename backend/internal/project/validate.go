package project

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/wagner/convenios-e-fomentos/backend/internal/catalog"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// problems acumula os erros de validação de um corpo, para devolver todos de uma vez (422).
type problems []httpapi.FieldError

func (p *problems) add(field, msg string) {
	*p = append(*p, httpapi.FieldError{Field: field, Message: msg})
}

func (p problems) err() error {
	if len(p) == 0 {
		return nil
	}
	return httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError(p)})
}

// Limites de tamanho (em caracteres).
const (
	maxContacts         = 5
	maxJobTitle         = 100
	maxPersonName       = 150
	maxInstitution      = 200
	maxOtherType        = 100
	maxStreet           = 150
	maxNumber           = 20
	maxComplement       = 100
	maxDistrict         = 100
	maxHabilitation     = 50
	maxTitle            = 200
	maxObject           = 5000
	maxJustify          = 10000
	maxPlaceDetail      = 500
	maxExecutionPeriods = 50
)

func tooLong(s string, max int) bool { return len([]rune(s)) > max }

// text normaliza um texto livre de uma linha: apara e junta os espaços repetidos.
func text(s string) string { return strings.Join(strings.Fields(s), " ") }

// multiline apara o texto sem mexer nas quebras de linha internas.
func multiline(s string) string { return strings.TrimSpace(s) }

var (
	nonDigits  = regexp.MustCompile(`\D`)
	sigconRe   = regexp.MustCompile(`^[0-9]{1,6}/[0-9]{4}$`)
	validAreas = map[string]bool{}
)

func init() {
	for _, a := range strings.Fields("11 12 13 14 15 16 17 18 19 21 22 24 27 28 31 32 33 34 35 37 38 41 42 43 44 45 46 47 48 49 51 53 54 55 61 62 63 64 65 66 67 68 69 71 73 74 75 77 79 81 82 83 84 85 86 87 88 89 91 92 93 94 95 96 97 98 99") {
		validAreas[a] = true
	}
}

// digits devolve só os dígitos do texto.
func digits(s string) string { return nonDigits.ReplaceAllString(s, "") }

// phoneNumber normaliza um telefone brasileiro (aceita +55 e pontuação) e confere DDD e formato.
func phoneNumber(raw string) (string, bool) {
	d := digits(raw)
	if len(d) == 13 && strings.HasPrefix(d, "55") {
		d = d[2:]
	}
	if (len(d) != 10 && len(d) != 11) || !validAreas[d[:2]] {
		return "", false
	}
	return d, true
}

// phones valida a lista de telefones do proponente: DDD válido, celular com 11 dígitos começando por 9,
// fixo com 10 dígitos, sem repetidos.
func phones(in []Phone, field string, p *problems) []Phone {
	if len(in) > maxContacts {
		p.add(field, "Informe no máximo 5 telefones.")
	}
	out := make([]Phone, 0, len(in))
	seen := map[string]bool{}
	for i, ph := range in {
		f := field + "[" + strconv.Itoa(i) + "]"
		n, ok := phoneNumber(ph.Number)
		switch {
		case !ok:
			p.add(f+".number", "Telefone inválido. Use DDD e número, por exemplo (65) 9 9999-8888.")
			continue
		case ph.Type != PhoneMobile && ph.Type != PhoneLandline:
			p.add(f+".type", "Use celular ou fixo.")
			continue
		case ph.Type == PhoneMobile && (len(n) != 11 || n[2] != '9'):
			p.add(f+".number", "Celular deve ter 11 dígitos e começar por 9 depois do DDD.")
			continue
		case ph.Type == PhoneLandline && len(n) != 10:
			p.add(f+".number", "Telefone fixo deve ter 10 dígitos com o DDD.")
			continue
		case seen[n]:
			p.add(f+".number", "Telefone repetido.")
			continue
		}
		seen[n] = true
		out = append(out, Phone{Number: n, Type: ph.Type})
	}
	return out
}

// emails valida a lista de e-mails: formato, sem repetidos, exatamente um principal (o primeiro, se nenhum foi marcado).
func emails(in []Email, field string, p *problems) []Email {
	if len(in) > maxContacts {
		p.add(field, "Informe no máximo 5 e-mails.")
	}
	out := make([]Email, 0, len(in))
	seen := map[string]bool{}
	primary := -1
	for i, e := range in {
		f := field + "[" + strconv.Itoa(i) + "]"
		addr, ok := emailAddress(e.Address)
		switch {
		case !ok:
			p.add(f+".address", "E-mail inválido.")
			continue
		case seen[addr]:
			p.add(f+".address", "E-mail repetido.")
			continue
		}
		seen[addr] = true
		if e.Primary {
			if primary >= 0 {
				p.add(f+".primary", "Marque só um e-mail como principal.")
			}
			primary = len(out)
		}
		out = append(out, Email{Address: addr, Primary: e.Primary})
	}
	if len(out) > 0 && primary < 0 {
		out[0].Primary = true
	}
	return out
}

// emailAddress confere o formato de um e-mail simples (sem nome nem comentários).
func emailAddress(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 254 {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", false
	}
	return s, true
}

// place confere UF e município: o município precisa existir e pertencer à UF. Devolve os valores prontos para o banco.
func place(ctx context.Context, q *db.Queries, uf, ibge, ufField, ibgeField string, p *problems) (pgtype.Text, *string) {
	uf = strings.ToUpper(strings.TrimSpace(uf))
	ibge = strings.TrimSpace(ibge)
	var ufOut pgtype.Text
	var ibgeOut *string
	if uf != "" {
		if !isUF(uf) {
			p.add(ufField, "Informe a sigla de uma unidade da federação.")
			return ufOut, ibgeOut
		}
		ufOut = pgtype.Text{String: uf, Valid: true}
	}
	if ibge == "" {
		return ufOut, nil
	}
	m, err := q.GetMunicipality(ctx, ibge)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p.add(ibgeField, "Município não encontrado.")
	case err != nil:
		p.add(ibgeField, "Não foi possível consultar o município.")
	case uf == "":
		p.add(ufField, "Informe a UF do município.")
	case m.Uf != uf:
		p.add(ibgeField, "O município não pertence à UF informada.")
	default:
		ibgeOut = &m.IbgeCode
	}
	return ufOut, ibgeOut
}

func isUF(s string) bool {
	for _, u := range catalog.UFs {
		if u == s {
			return true
		}
	}
	return false
}

// date lê uma data AAAA-MM-DD; vazio vira nulo.
func date(s, field string, p *problems) pgtype.Date {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		p.add(field, "Informe uma data válida no formato AAAA-MM-DD.")
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func dateString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format(DateLayout)
}

func nullText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
