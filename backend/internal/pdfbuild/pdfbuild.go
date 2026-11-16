// Pacote pdfbuild monta o PDF do relatório completo do projeto: valida, saneia e junta os arquivos enviados, grava a árvore de
// bookmarks, os links do sumário e a numeração das páginas. Tudo trabalha com ARQUIVOS em disco (nunca com o conjunto na memória) e
// os arquivos de entrada são conteúdo não confiável de terceiros: o saneamento tira dele tudo o que executa ou abre algo.
//
// Escolhas que vieram do estudo da Fase 0 (docs/planejamento-relatorio-projeto.md, §10):
//   - bookmarks com destino DIRETO (página), não nomeado: o pdfcpu grava o nome do destino igual ao título e, com certos acentos
//     ("Ofício"), leitores estritos deixam de resolver o destino;
//   - os links do sumário são gravados DEPOIS de juntar, já com as páginas finais;
//   - sanear é obrigatório antes de juntar: a junção leva para o PDF final JavaScript, abertura de programa, anexos e ações de página.
package pdfbuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Erros que a página de aviso traduz para quem lê.
var (
	// ErrEncrypted: o PDF tem senha.
	ErrEncrypted = errors.New("pdf protegido por senha")
	// ErrUnreadable: o arquivo está corrompido, truncado ou não é um PDF.
	ErrUnreadable = errors.New("pdf ilegível")
)

// conf é a configuração sem estado: a padrão tenta criar ~/.config/pdfcpu e, sem diretório gravável (contêiner somente leitura),
// dá PÂNICO. Descoberto na medição em contêiner (o pânico derrubou a API).
func conf() *model.Configuration { return model.NewStatelessConfiguration() }

// safe executa fn convertendo o pânico da biblioteca (que não devolve erro em todos os caminhos) em erro: um arquivo defeituoso
// de terceiros nunca pode derrubar o processo da API.
func safe(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: falha interna ao processar o PDF: %v", ErrUnreadable, r)
		}
	}()
	return fn()
}

// classify traduz o erro da biblioteca para os dois casos que interessam.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "password") {
		return fmt.Errorf("%w", ErrEncrypted)
	}
	return fmt.Errorf("%w: %s", ErrUnreadable, err.Error())
}

func withFiles(in, out string, fn func(io.ReadSeeker, io.Writer) error) error {
	r, err := os.Open(in) //nolint:gosec // caminhos montados pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	w, err := os.Create(out) //nolint:gosec // idem
	if err != nil {
		return err
	}
	if err := fn(r, w); err != nil {
		_ = w.Close()
		_ = os.Remove(out)
		return err
	}
	return w.Close()
}

// PageCount valida o PDF e devolve o número de páginas.
func PageCount(ctx context.Context, path string) (n int, err error) {
	err = safe(func() error {
		f, err := os.Open(path) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		var perr error
		n, perr = api.PageCount(ctx, f, conf())
		return classify(perr)
	})
	return n, err
}

// Merge junta os PDFs, na ordem, em out.
func Merge(ctx context.Context, inputs []string, out string) error {
	return safe(func() error { return merge(ctx, inputs, out) })
}

func merge(ctx context.Context, inputs []string, out string) error {
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	rss := make([]io.ReadSeeker, 0, len(inputs))
	for _, p := range inputs {
		f, err := os.Open(p) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
		if err != nil {
			return err
		}
		closers = append(closers, f)
		rss = append(rss, f)
	}
	w, err := os.Create(out) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return err
	}
	if err := api.MergeRaw(ctx, rss, w, false, conf()); err != nil {
		_ = w.Close()
		_ = os.Remove(out)
		return fmt.Errorf("juntando os PDFs: %w", err)
	}
	return w.Close()
}

// Bookmark é um item da árvore de bookmarks: Page é a página (1-based) do PDF final.
type Bookmark struct {
	Title string
	Page  int
	Kids  []Bookmark
}

// WriteOutline grava a árvore de bookmarks em out, com destino direto e título em UTF-16.
func WriteOutline(ctx context.Context, in, out string, bms []Bookmark) error {
	return safe(func() error { return writeOutline(ctx, in, out, bms) })
}

func writeOutline(ctx context.Context, in, out string, bms []Bookmark) error {
	if len(bms) == 0 {
		return copyFile(in, out)
	}
	return withFiles(in, out, func(r io.ReadSeeker, w io.Writer) error {
		xt, err := api.ReadAndValidate(ctx, r, conf())
		if err != nil {
			return err
		}
		cat, err := xt.Catalog()
		if err != nil {
			return err
		}
		rootRef, err := xt.IndRefForNewObject(types.Dict{})
		if err != nil {
			return err
		}
		var build func(items []Bookmark, parent *types.IndirectRef) (first, last *types.IndirectRef, count int, err error)
		build = func(items []Bookmark, parent *types.IndirectRef) (*types.IndirectRef, *types.IndirectRef, int, error) {
			refs := make([]*types.IndirectRef, len(items))
			dicts := make([]types.Dict, len(items))
			total := 0
			for i, it := range items {
				_, pgRef, _, err := xt.PageDict(ctx, it.Page, false)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("bookmark %q: página %d: %w", it.Title, it.Page, err)
				}
				d := types.Dict{
					"Title":  types.StringLiteral(types.EncodeUTF16String(it.Title)),
					"Parent": *parent,
					"Dest":   types.Array{*pgRef, types.Name("Fit")},
				}
				ref, err := xt.IndRefForNewObject(d)
				if err != nil {
					return nil, nil, 0, err
				}
				refs[i], dicts[i] = ref, d
				total++
			}
			for i, it := range items {
				if i > 0 {
					dicts[i]["Prev"] = *refs[i-1]
				}
				if i < len(items)-1 {
					dicts[i]["Next"] = *refs[i+1]
				}
				if len(it.Kids) > 0 {
					f, l, n, err := build(it.Kids, refs[i])
					if err != nil {
						return nil, nil, 0, err
					}
					dicts[i]["First"], dicts[i]["Last"], dicts[i]["Count"] = *f, *l, types.Integer(n)
					total += n
				}
			}
			return refs[0], refs[len(refs)-1], total, nil
		}
		f, l, n, err := build(bms, rootRef)
		if err != nil {
			return err
		}
		root, _ := xt.DereferenceDict(*rootRef)
		root["Type"], root["First"], root["Last"], root["Count"] = types.Name("Outlines"), *f, *l, types.Integer(n)
		cat["Outlines"] = *rootRef
		cat["PageMode"] = types.Name("UseOutlines") // abre o painel de bookmarks
		return api.WriteContext(ctx, xt, w)
	})
}

// Link é uma área clicável de uma página que leva a outra página do mesmo PDF. As medidas são em pontos, com a origem no canto
// inferior esquerdo da página.
type Link struct {
	Page       int
	X, Y, W, H float64
	Target     int
}

// AddLinks grava os links em out.
func AddLinks(ctx context.Context, in, out string, links []Link) error {
	return safe(func() error { return addLinks(ctx, in, out, links) })
}

func addLinks(ctx context.Context, in, out string, links []Link) error {
	if len(links) == 0 {
		return copyFile(in, out)
	}
	m := map[int][]model.AnnotationRenderer{}
	for i, l := range links {
		dest := &model.Destination{Typ: model.DestXYZ, PageNr: l.Target, Left: -1, Top: -1}
		ann := model.NewLinkAnnotation(*types.NewRectangle(l.X, l.Y, l.X+l.W, l.Y+l.H), 0, "", fmt.Sprintf("toc-%d", i), "", 0, nil, dest, "", nil, false, 0, model.BSSolid)
		m[l.Page] = append(m[l.Page], ann)
	}
	return withFiles(in, out, func(r io.ReadSeeker, w io.Writer) error { return api.AddAnnotationsMap(ctx, r, w, m, conf()) })
}

// StampPageNumbers escreve "Página X de Y" no canto inferior direito de TODAS as páginas (Helvetica 8 pt, sem contorno).
func StampPageNumbers(ctx context.Context, in, out string) error {
	return safe(func() error { return stampPageNumbers(ctx, in, out) })
}

func stampPageNumbers(ctx context.Context, in, out string) error {
	wm, err := pdfcpu.ParseTextWatermarkDetails(ctx, "Página %p de %P", "font:Helvetica, points:8, pos:br, off:-20 12, rot:0, fillc:#303030, scale:1 abs, mo:0", true, types.POINTS, conf())
	if err != nil {
		return err
	}
	return withFiles(in, out, func(r io.ReadSeeker, w io.Writer) error { return api.AddWatermarks(ctx, r, w, nil, wm, conf()) })
}

func copyFile(a, b string) error {
	return withFiles(a, b, func(r io.ReadSeeker, w io.Writer) error { _, err := io.Copy(w, r); return err })
}

// ---- Saneamento ----

func deref(xt *model.Context, o types.Object) types.Object {
	r, err := xt.Dereference(o)
	if err != nil {
		return nil
	}
	return r
}

func dictOf(xt *model.Context, o types.Object) types.Dict {
	d, _ := deref(xt, o).(types.Dict)
	return d
}

func nameOf(xt *model.Context, o types.Object) string {
	n, _ := deref(xt, o).(types.Name)
	return string(n)
}

func actionKind(xt *model.Context, a types.Object) string {
	if d := dictOf(xt, a); d != nil {
		return nameOf(xt, d["S"])
	}
	return ""
}

// Anotações que só marcam o texto ou a página; as demais (anexo de arquivo, som, filme, tela, mídia rica, 3D) são removidas.
var safeSubtypes = map[string]bool{"Link": true, "Widget": true, "Text": true, "FreeText": true, "Highlight": true, "Underline": true,
	"StrikeOut": true, "Squiggly": true, "Square": true, "Circle": true, "Line": true, "Polygon": true, "PolyLine": true, "Ink": true,
	"Stamp": true, "Caret": true, "Popup": true}

// Ações aceitas: ir para outra página e abrir um endereço da web. Abrir programa, JavaScript, enviar formulário, abrir outro arquivo
// e as demais saem.
var safeActions = map[string]bool{"GoTo": true, "URI": true, "": true}

// Sanitize regrava o PDF sem o que executa ou abre algo: ações de abertura (OpenAction/AA), JavaScript, anexos embutidos e as
// anotações e links perigosos. Mantém links internos e de endereços da web. Devolve o que removeu.
func Sanitize(ctx context.Context, in, out string) (removed []string, err error) {
	err = safe(func() error { removed, err = sanitize(ctx, in, out); return err })
	return removed, err
}

func sanitize(ctx context.Context, in, out string) (removed []string, err error) {
	err = withFiles(in, out, func(r io.ReadSeeker, w io.Writer) error {
		xt, err := api.ReadAndValidate(ctx, r, conf())
		if err != nil {
			return classify(err)
		}
		cat, _ := xt.Catalog()
		for _, k := range []string{"OpenAction", "AA"} {
			if _, ok := cat[k]; ok {
				delete(cat, k)
				removed = append(removed, "catálogo /"+k)
			}
		}
		if names := dictOf(xt, cat["Names"]); names != nil {
			for _, k := range []string{"JavaScript", "EmbeddedFiles"} {
				if _, ok := names[k]; ok {
					delete(names, k)
					removed = append(removed, "Names /"+k)
				}
			}
		}
		// O pdfcpu reescreve as árvores de nomes a partir deste cache: sem apagá-lo, o JavaScript e os anexos voltam.
		for _, k := range []string{"JavaScript", "EmbeddedFiles"} {
			if _, ok := xt.Names[k]; ok {
				delete(xt.Names, k)
				removed = append(removed, "árvore de nomes /"+k)
			}
		}
		for n := 1; n <= xt.PageCount; n++ {
			pd, _, _, err := xt.PageDict(ctx, n, false)
			if err != nil {
				return classify(err)
			}
			if _, ok := pd["AA"]; ok {
				delete(pd, "AA")
				removed = append(removed, fmt.Sprintf("página %d /AA", n))
			}
			arr, ok := deref(xt, pd["Annots"]).(types.Array)
			if !ok {
				continue
			}
			keep := types.Array{}
			for _, a := range arr {
				ad := dictOf(xt, a)
				if ad == nil {
					continue
				}
				sub, act := nameOf(xt, ad["Subtype"]), actionKind(xt, ad["A"])
				if !safeSubtypes[sub] {
					removed = append(removed, fmt.Sprintf("página %d anotação %s", n, sub))
					continue
				}
				if !safeActions[act] {
					removed = append(removed, fmt.Sprintf("página %d anotação %s ação %s", n, sub, act))
					if sub == "Link" {
						continue // link sem ação segura não serve para nada
					}
					delete(ad, "A")
				}
				if _, has := ad["AA"]; has {
					delete(ad, "AA")
					removed = append(removed, fmt.Sprintf("página %d anotação %s /AA", n, sub))
				}
				keep = append(keep, a)
			}
			pd["Annots"] = keep
		}
		return api.WriteContext(ctx, xt, w)
	})
	return removed, err
}

// ActiveContent lista o que há de ativo no PDF além de links internos e de endereços da web. Vazio = limpo. É a conferência final
// do relatório completo: se sobrar algo, a montagem falha em vez de entregar um PDF que executa.
func ActiveContent(ctx context.Context, path string) (out []string, err error) {
	err = safe(func() error { out, err = activeContent(ctx, path); return err })
	return out, err
}

func activeContent(ctx context.Context, path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	xt, err := api.ReadAndValidate(ctx, f, conf())
	if err != nil {
		return nil, classify(err)
	}
	var out []string
	cat, _ := xt.Catalog()
	for _, k := range []string{"OpenAction", "AA"} {
		if _, ok := cat[k]; ok {
			out = append(out, "catálogo /"+k)
		}
	}
	if names := dictOf(xt, cat["Names"]); names != nil {
		for _, k := range []string{"JavaScript", "EmbeddedFiles"} {
			if _, ok := names[k]; ok {
				out = append(out, "Names /"+k)
			}
		}
	}
	for n := 1; n <= xt.PageCount; n++ {
		pd, _, _, err := xt.PageDict(ctx, n, false)
		if err != nil {
			continue
		}
		if _, ok := pd["AA"]; ok {
			out = append(out, fmt.Sprintf("página %d /AA", n))
		}
		arr, _ := deref(xt, pd["Annots"]).(types.Array)
		for _, a := range arr {
			ad := dictOf(xt, a)
			if ad == nil {
				continue
			}
			sub, act := nameOf(xt, ad["Subtype"]), actionKind(xt, ad["A"])
			if !safeSubtypes[sub] || !safeActions[act] {
				out = append(out, fmt.Sprintf("página %d anotação %s ação %q", n, sub, act))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
