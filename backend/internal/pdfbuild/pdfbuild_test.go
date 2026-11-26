package pdfbuild

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func textPDF(t *testing.T, path string, pages int, protect string) {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	if protect != "" {
		pdf.SetProtection(fpdf.CnProtectPrint, protect, "dono-secreto")
	}
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 12)
		pdf.Text(20, 30, fmt.Sprintf("pagina %d de %d", i, pages))
	}
	require.NoError(t, pdf.OutputFileAndClose(path))
}

// hostilePDF monta à mão um PDF com abertura em JavaScript, JavaScript nomeado, anexo embutido e links (web, abrir programa,
// JavaScript e interno).
func hostilePDF(t *testing.T, path string) {
	t.Helper()
	var b bytes.Buffer
	offs := map[int]int{}
	obj := func(n int, body string) {
		offs[n] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R /OpenAction 5 0 R /AA << /WC 5 0 R >> /Names << /JavaScript << /Names [(init) 5 0 R] >> /EmbeddedFiles << /Names [(segredo.txt) 6 0 R] >> >> >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> /AA << /O 5 0 R >> /Annots [8 0 R 9 0 R 10 0 R 11 0 R] >>")
	stream := "BT /F1 18 Tf 50 780 Td (PDF hostil) Tj ET"
	obj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	obj(5, "<< /S /JavaScript /JS (app.alert\\('x'\\);) >>")
	obj(6, "<< /Type /Filespec /F (segredo.txt) /UF (segredo.txt) /EF << /F 7 0 R >> >>")
	obj(7, "<< /Type /EmbeddedFile /Length 5 >>\nstream\nabcde\nendstream")
	obj(8, "<< /Type /Annot /Subtype /Link /Rect [50 700 300 730] /Border [0 0 1] /A << /S /URI /URI (http://exemplo.org/verificar) >> >>")
	obj(9, "<< /Type /Annot /Subtype /Link /Rect [50 650 300 680] /Border [0 0 1] /A << /S /Launch /F (cmd.exe) >> >>")
	obj(10, "<< /Type /Annot /Subtype /Link /Rect [50 600 300 630] /Border [0 0 1] /A 5 0 R >>")
	obj(11, "<< /Type /Annot /Subtype /Link /Rect [50 550 300 580] /Border [0 0 1] /Dest [3 0 R /Fit] >>")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 12\n0000000000 65535 f \n")
	for i := 1; i <= 11; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 12 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", x)
	require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
}

func TestPageCountClassificaOsErros(t *testing.T) {
	dir := t.TempDir()
	ok, enc, trunc, fake := filepath.Join(dir, "ok.pdf"), filepath.Join(dir, "enc.pdf"), filepath.Join(dir, "trunc.pdf"), filepath.Join(dir, "fake.pdf")
	textPDF(t, ok, 3, "")
	textPDF(t, enc, 2, "senha")
	b, _ := os.ReadFile(ok)
	require.NoError(t, os.WriteFile(trunc, b[:len(b)/2], 0o644))
	require.NoError(t, os.WriteFile(fake, append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("lixo "), 400)...), 0o644))

	n, err := PageCount(ctx, ok)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	_, err = PageCount(ctx, enc)
	assert.ErrorIs(t, err, ErrEncrypted)
	for _, p := range []string{trunc, fake} {
		_, err = PageCount(ctx, p)
		assert.ErrorIs(t, err, ErrUnreadable, p)
		assert.False(t, errors.Is(err, ErrEncrypted))
	}
}

// A junção leva o conteúdo ativo dos arquivos de terceiros para o PDF final; sanear antes de juntar o impede.
func TestSanearAntesDeJuntar(t *testing.T) {
	dir := t.TempDir()
	hostile, clean, normal := filepath.Join(dir, "h.pdf"), filepath.Join(dir, "h-limpo.pdf"), filepath.Join(dir, "n.pdf")
	hostilePDF(t, hostile)
	textPDF(t, normal, 2, "")

	active, err := ActiveContent(ctx, hostile)
	require.NoError(t, err)
	assert.NotEmpty(t, active, "o arquivo hostil tem conteúdo ativo")

	// Sem sanear, a junção carrega o conteúdo ativo para o resultado.
	raw := filepath.Join(dir, "raw.pdf")
	require.NoError(t, Merge(ctx, []string{normal, hostile}, raw))
	leaked, err := ActiveContent(ctx, raw)
	require.NoError(t, err)
	assert.NotEmpty(t, leaked, "juntar sem sanear leva conteúdo ativo para o PDF final")

	removed, err := Sanitize(ctx, hostile, clean)
	require.NoError(t, err)
	assert.NotEmpty(t, removed)
	active, err = ActiveContent(ctx, clean)
	require.NoError(t, err)
	assert.Empty(t, active, "depois de sanear não sobra nada ativo")

	final := filepath.Join(dir, "final.pdf")
	require.NoError(t, Merge(ctx, []string{normal, clean}, final))
	active, err = ActiveContent(ctx, final)
	require.NoError(t, err)
	assert.Empty(t, active)
	n, err := PageCount(ctx, final)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	// Os links internos e de endereço da web sobrevivem.
	f, _ := os.Open(clean)
	defer func() { _ = f.Close() }()
	m, err := api.Annotations(ctx, f, []string{"1"}, conf())
	require.NoError(t, err)
	total := 0
	for _, pa := range m {
		for _, a := range pa {
			total += len(a.Map)
		}
	}
	assert.Equal(t, 2, total, "ficam o link da web e o interno")
}

func TestBookmarksLinksENumeracao(t *testing.T) {
	dir := t.TempDir()
	rep, a, b := filepath.Join(dir, "rep.pdf"), filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf")
	textPDF(t, rep, 2, "")
	textPDF(t, a, 3, "")
	textPDF(t, b, 2, "")
	merged := filepath.Join(dir, "m.pdf")
	require.NoError(t, Merge(ctx, []string{rep, a, b}, merged))

	// Títulos repetidos e com acento ("Ofício") têm de resolver para a página certa.
	bms := []Bookmark{
		{Title: "Relatório do projeto", Page: 1, Kids: []Bookmark{{Title: "Identificação", Page: 1}, {Title: "Documentos (sumário)", Page: 2}}},
		{Title: "Documentos", Page: 3, Kids: []Bookmark{{Title: "Ofício", Page: 3}, {Title: "Ofício", Page: 6}}},
	}
	withOutline := filepath.Join(dir, "o.pdf")
	require.NoError(t, WriteOutline(ctx, merged, withOutline, bms))
	withLinks := filepath.Join(dir, "l.pdf")
	require.NoError(t, AddLinks(ctx, withOutline, withLinks, []Link{{Page: 2, X: 50, Y: 700, W: 400, H: 16, Target: 3}, {Page: 2, X: 50, Y: 680, W: 400, H: 16, Target: 6}}))
	final := filepath.Join(dir, "f.pdf")
	require.NoError(t, StampPageNumbers(ctx, withLinks, final))

	n, err := PageCount(ctx, final)
	require.NoError(t, err)
	assert.Equal(t, 7, n)
	f, _ := os.Open(final)
	defer func() { _ = f.Close() }()
	list, err := api.ListBookmarks(ctx, f, conf())
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(list), "Ofício")
	assert.Len(t, list, 6)
	ok, err := api.HasWatermarks(ctx, f, conf())
	require.NoError(t, err)
	assert.True(t, ok, "a numeração foi carimbada")
	active, err := ActiveContent(ctx, final)
	require.NoError(t, err)
	assert.Empty(t, active)
}

// jpegWithOrientation insere a tag EXIF de orientação num JPEG.
func jpegWithOrientation(t *testing.T, img image.Image, o uint16) []byte {
	t.Helper()
	var jb bytes.Buffer
	require.NoError(t, jpeg.Encode(&jb, img, nil))
	raw := jb.Bytes()
	var tiff bytes.Buffer
	tiff.WriteString("II*\x00")
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(&tiff, binary.LittleEndian, o)
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0))
	var seg bytes.Buffer
	seg.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&seg, binary.BigEndian, uint16(2+6+tiff.Len()))
	seg.WriteString("Exif\x00\x00")
	seg.Write(tiff.Bytes())
	return append(append(append([]byte{}, raw[:2]...), seg.Bytes()...), raw[2:]...)
}

func pageSize(t *testing.T, path string) (w, h float64) {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	dims, err := api.PageDims(ctx, f, conf())
	require.NoError(t, err)
	require.Len(t, dims, 1)
	return dims[0].Width, dims[0].Height
}

func TestImagemParaPDF(t *testing.T) {
	dir := t.TempDir()
	land := image.NewRGBA(image.Rect(0, 0, 1600, 1000))
	for x := 0; x < 1600; x++ {
		for y := 0; y < 1000; y++ {
			land.Set(x, y, color.RGBA{250, 250, 235, 255})
		}
	}
	// PNG deitado: página deitada.
	var pb bytes.Buffer
	require.NoError(t, png.Encode(&pb, land))
	pngPath, out := filepath.Join(dir, "a.png"), filepath.Join(dir, "a.pdf")
	require.NoError(t, os.WriteFile(pngPath, pb.Bytes(), 0o644))
	require.NoError(t, ImageToPDF(pngPath, out))
	w, h := pageSize(t, out)
	assert.Greater(t, w, h)

	// Foto com EXIF "girar 90°": entra em pé.
	jpgPath := filepath.Join(dir, "b.jpg")
	require.NoError(t, os.WriteFile(jpgPath, jpegWithOrientation(t, land, 6), 0o644))
	out2 := filepath.Join(dir, "b.pdf")
	require.NoError(t, ImageToPDF(jpgPath, out2))
	w, h = pageSize(t, out2)
	assert.Less(t, w, h, "a orientação EXIF foi aplicada")

	// Sem orientação, a mesma foto fica deitada.
	jpg1 := filepath.Join(dir, "c.jpg")
	require.NoError(t, os.WriteFile(jpg1, jpegWithOrientation(t, land, 1), 0o644))
	out3 := filepath.Join(dir, "c.pdf")
	require.NoError(t, ImageToPDF(jpg1, out3))
	w, h = pageSize(t, out3)
	assert.Greater(t, w, h)

	// Lixo não vira PDF.
	bad := filepath.Join(dir, "d.png")
	require.NoError(t, os.WriteFile(bad, []byte("isto não é uma imagem"), 0o644))
	assert.ErrorIs(t, ImageToPDF(bad, filepath.Join(dir, "d.pdf")), ErrImage)
}

// Em contêiner sem diretório de configuração gravável o pdfcpu padrão dava PÂNICO (e derrubava a API). A configuração sem estado
// não toca o disco.
func TestFuncionaSemDiretorioDeConfiguracao(t *testing.T) {
	t.Setenv("HOME", "/nao/existe")
	t.Setenv("XDG_CONFIG_HOME", "/proc/nao-gravavel")
	p := filepath.Join(t.TempDir(), "a.pdf")
	textPDF(t, p, 2, "")
	n, err := PageCount(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	_, err = Sanitize(ctx, p, filepath.Join(t.TempDir(), "s.pdf"))
	require.NoError(t, err)
}

// Um pânico da biblioteca vira erro: arquivo de terceiros nunca derruba o processo.
func TestPanicoViraErro(t *testing.T) {
	err := safe(func() error { panic("boom") })
	assert.ErrorIs(t, err, ErrUnreadable)
}

// noisyPDF cria um PDF cujas páginas têm conteúdo pouco compressível, para o tamanho crescer com as páginas.
func noisyPDF(t *testing.T, path string, pages, pageKB int) {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false)
	pdf.SetFont("Helvetica", "", 8)
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		for y := 10.0; y < 280 && pdf.Err() == false; y += 4 {
			pdf.Text(10, y, fmt.Sprintf("p%d %0*d", i, pageKB*20, i*7919+int(y)))
		}
	}
	require.NoError(t, pdf.OutputFileAndClose(path))
}

func TestSplitDivideEmPartesQueCabem(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "grande.pdf")
	noisyPDF(t, in, 12, 5)
	st, err := os.Stat(in)
	require.NoError(t, err)
	limit := st.Size() / 3

	t.Run("cabe inteiro: devolve o próprio arquivo", func(t *testing.T) {
		out := t.TempDir()
		parts, err := Split(ctx, in, out, st.Size()+1)
		require.NoError(t, err)
		assert.Equal(t, []string{in}, parts)
	})
	t.Run("divide em partes de até o limite, sem perder páginas", func(t *testing.T) {
		out := t.TempDir()
		parts, err := Split(ctx, in, out, limit)
		require.NoError(t, err)
		require.Greater(t, len(parts), 1)
		total := 0
		for i, p := range parts {
			fi, err := os.Stat(p)
			require.NoError(t, err)
			assert.LessOrEqual(t, fi.Size(), limit, "parte %d", i+1)
			n, err := PageCount(ctx, p)
			require.NoError(t, err)
			total += n
		}
		assert.Equal(t, 12, total, "todas as páginas, uma só vez")
		assert.NoFileExists(t, filepath.Join(out, "probe.pdf"), "arquivo de teste removido")
	})
	t.Run("página sozinha acima do limite", func(t *testing.T) {
		_, err := Split(ctx, in, t.TempDir(), 500)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrPageTooLarge))
	})
	t.Run("limite inválido e arquivo ruim", func(t *testing.T) {
		_, err := Split(ctx, in, t.TempDir(), 0)
		assert.Error(t, err)
		bad := filepath.Join(dir, "lixo.pdf")
		require.NoError(t, os.WriteFile(bad, bytes.Repeat([]byte("x"), 4096), 0o644))
		_, err = Split(ctx, bad, t.TempDir(), 100)
		assert.True(t, errors.Is(err, ErrUnreadable))
	})
}
