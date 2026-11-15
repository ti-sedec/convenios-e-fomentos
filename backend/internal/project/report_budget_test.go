package project

import "testing"

func TestOrcamentoDeMemoriaDoRelatorioCompleto(t *testing.T) {
	const mib = 1 << 20
	cases := []struct {
		mem  int64
		want int64 // em MiB, aproximado
	}{{0, 0}, {512 * mib, 108}, {1024 * mib, 236}, {2048 * mib, 492}, {64 * mib, 0}}
	for _, c := range cases {
		got := inputBudget(c.mem) / mib
		if got < c.want-1 || got > c.want+1 {
			t.Errorf("memória %d MiB: limite %d MiB, esperava ~%d MiB", c.mem/mib, got, c.want)
		}
	}
	// O limite de 512 MB tem de ficar ABAIXO do que derrubou a API na medição (150 MB) e acima do que passou (125 MB não deve ser exigido).
	if b := inputBudget(512*mib) / mib; b >= 125 {
		t.Errorf("com 512 MB o limite (%d MiB) tem de ficar abaixo do que foi medido como seguro (125 MB)", b)
	}
}
