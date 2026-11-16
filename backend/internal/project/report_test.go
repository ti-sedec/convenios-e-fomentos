package project

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// pageTexts separa o texto do PDF (sem compressão) por página.
func pageTexts(pdf []byte) []string {
	parts := bytes.Split(pdf, []byte("/Type /Page\n"))
	var out []string
	for _, p := range parts[1:] {
		out = append(out, latin1(p))
	}
	return out
}

func sampleData(fields int) ReportData {
	at := time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)
	d := ReportData{Schema: reportSchema, Kind: ReportSummary, Code: "2026-000001", GeneratedAt: at, GeneratedBy: "Fulano", Declaration: Declaration{Text: "Declaro.", AcceptedAt: at, ByName: "Fulano", ByCPF: "***.456.789-**", IP: "10.0.0.1"}}
	sec := ReportSection{Key: "x", Title: "Seção longa"}
	for i := 1; i <= fields; i++ {
		sec.Fields = append(sec.Fields, ReportField{Label: fmt.Sprintf("Campo %03d", i), Value: fmt.Sprintf("Valor %03d %s", i, strings.Repeat("texto ", i%9))})
	}
	d.Sections = []ReportSection{sec}
	return d
}

// O rótulo e o valor de cada linha ficam na mesma página, não importa onde caia a quebra.
func TestRelatorioNaoSeparaRotuloEValorNaQuebraDePagina(t *testing.T) {
	pdf, pages, err := renderReportPDF(sampleData(120), ReportOptions{PageNumbers: true})
	if err != nil {
		t.Fatal(err)
	}
	if pages < 3 {
		t.Fatalf("esperava várias páginas, veio %d", pages)
	}
	label := regexp.MustCompile(`Campo (\d{3}):`)
	value := regexp.MustCompile(`Valor (\d{3})`)
	for i, txt := range pageTexts(pdf) {
		labels, values := map[string]bool{}, map[string]bool{}
		for _, m := range label.FindAllStringSubmatch(txt, -1) {
			labels[m[1]] = true
		}
		for _, m := range value.FindAllStringSubmatch(txt, -1) {
			values[m[1]] = true
		}
		for k := range labels {
			if !values[k] {
				t.Errorf("página %d: o rótulo do campo %s ficou sem o valor", i+1, k)
			}
		}
		for k := range values {
			if !labels[k] {
				t.Errorf("página %d: o valor do campo %s ficou sem o rótulo", i+1, k)
			}
		}
	}
}

// Mesmos dados e mesma data: mesmos bytes. É o que permite refazer o PDF do histórico.
func TestRelatorioEhDeterministico(t *testing.T) {
	a, _, _ := renderReportPDF(sampleData(30), ReportOptions{PageNumbers: true})
	b, _, _ := renderReportPDF(sampleData(30), ReportOptions{PageNumbers: true})
	if !bytes.Equal(a, b) {
		t.Fatal("o PDF refeito não é idêntico")
	}
	c, _, _ := renderReportPDF(sampleData(30), ReportOptions{PageNumbers: false})
	if bytes.Equal(a, c) {
		t.Fatal("a opção de numeração não teve efeito")
	}
}
