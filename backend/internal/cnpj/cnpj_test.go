package cnpj

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"11.222.333/0001-81":  "11222333000181",
		"11222333000181":      "11222333000181",
		" 11 222 333 0001 81": "11222333000181",
		"00.000.000/0001-91":  "00000000000191", // CNPJ do Banco do Brasil
		"12.ABC.345/01DE-35":  "12ABC34501DE35", // exemplo da Receita Federal (alfanumérico)
		"12abc34501de35":      "12ABC34501DE35", // minúsculas viram maiúsculas
		"A1.B2C.3D4/E5F6-68":  "A1B2C3D4E5F668",
	} {
		got, err := Normalize(in)
		assert.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestNormalizeRecusaInvalidos(t *testing.T) {
	for _, in := range []string{
		"", "123", "11222333000182", // dígito errado
		"11111111111111",     // repetidos
		"11.222.333/0001-8A", // letra no dígito verificador
		"12.ABC.345/01DE-36", // dígito errado no alfanumérico
		"12.ABC.345/01DF-35", // letra alterada
		"12.ABC.345/01D!-35", // caractere inválido
		"112223330001810",    // longo demais
		"00000000000000",     // zeros
	} {
		_, err := Normalize(in)
		assert.ErrorIs(t, err, ErrInvalid, in)
	}
}

func TestFormat(t *testing.T) {
	assert.Equal(t, "11.222.333/0001-81", Format("11222333000181"))
	assert.Equal(t, "12.ABC.345/01DE-35", Format("12ABC34501DE35"))
	assert.Equal(t, "123", Format("123"))
}

func TestKey(t *testing.T) {
	k, ok := Key("12.abc.345/01de-35")
	assert.True(t, ok)
	assert.Equal(t, "12ABC34501DE35", k)
	_, ok = Key("12.ABC_345")
	assert.False(t, ok)
}
