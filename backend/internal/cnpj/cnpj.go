// Pacote cnpj normaliza, valida e formata números de CNPJ, nos dois formatos: o numérico (14 dígitos) e o alfanumérico da
// IN RFB nº 2.229/2024 (12 posições de 0-9 e A-Z, mais 2 dígitos verificadores numéricos).
package cnpj

import (
	"errors"
	"strings"
)

// ErrInvalid indica um CNPJ malformado ou com dígitos verificadores incorretos.
var ErrInvalid = errors.New("CNPJ inválido")

const length = 14

// Key remove a pontuação (. - / e espaço) e passa as letras para maiúsculas, sem validar o resultado. O segundo valor é falso
// quando o texto tem algum caractere que não pode fazer parte de um CNPJ.
func Key(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r == '.' || r == '-' || r == '/' || r == ' ':
		default:
			return "", false
		}
	}
	return b.String(), true
}

// Normalize remove a pontuação e valida o CNPJ (numérico ou alfanumérico), devolvendo os 14 caracteres em maiúsculas.
func Normalize(s string) (string, error) {
	key, ok := Key(s)
	if !ok || !valid(key) {
		return "", ErrInvalid
	}
	return key, nil
}

func valid(d string) bool {
	if len(d) != length || strings.Count(d, string(d[0])) == length {
		return false
	}
	// Os dois últimos caracteres são sempre dígitos verificadores numéricos.
	if d[12] < '0' || d[12] > '9' || d[13] < '0' || d[13] > '9' {
		return false
	}
	return checkDigit(d[:12]) == int(d[12]-'0') && checkDigit(d[:13]) == int(d[13]-'0')
}

// checkDigit calcula o dígito verificador dos primeiros len(d) caracteres (pesos 2..9, da direita para a esquerda). O valor de
// cada caractere é o seu código ASCII menos 48: 0-9 valem 0-9 e A-Z valem 17-42, então o cálculo atende aos dois formatos.
func checkDigit(d string) int {
	sum, weight := 0, 2
	for i := len(d) - 1; i >= 0; i-- {
		sum += int(d[i]-'0') * weight
		weight++
		if weight > 9 {
			weight = 2
		}
	}
	r := sum % 11
	if r < 2 {
		return 0
	}
	return 11 - r
}

// Format devolve o CNPJ no formato 12.345.678/0001-95 (ou 12.ABC.345/01DE-35). Entradas que não têm 14 caracteres voltam inalteradas.
func Format(key string) string {
	if len(key) != length {
		return key
	}
	return key[:2] + "." + key[2:5] + "." + key[5:8] + "/" + key[8:12] + "-" + key[12:]
}
