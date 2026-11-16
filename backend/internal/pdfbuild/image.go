package pdfbuild

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registra o decodificador de PNG
	"os"

	"github.com/go-pdf/fpdf"
	_ "golang.org/x/image/webp" // registra o decodificador de WebP
)

// ErrImage: a imagem não pôde ser lida ou é grande demais.
var ErrImage = errors.New("imagem ilegível ou grande demais")

// MaxImagePixels é o maior número de pixels aceito (a imagem decodificada ocupa 4 bytes por pixel na memória).
const MaxImagePixels = 40_000_000

// exifOrientation lê a tag Orientation (0x0112) de um JPEG; 1 se não houver. O pdfcpu não trata a orientação EXIF: sem isto, a foto
// de celular tirada em pé entraria deitada no PDF.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 < len(b) {
		if b[i] != 0xFF {
			return 1
		}
		m := b[i+1]
		l := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if m == 0xE1 && i+10 < len(b) && string(b[i+4:i+8]) == "Exif" {
			t := b[i+10 : min(i+2+l, len(b))]
			if len(t) < 8 {
				return 1
			}
			var o binary.ByteOrder = binary.LittleEndian
			if t[0] == 'M' {
				o = binary.BigEndian
			}
			ifd := int(o.Uint32(t[4:8]))
			if ifd < 0 || ifd+2 > len(t) {
				return 1
			}
			n := int(o.Uint16(t[ifd : ifd+2]))
			for k := 0; k < n; k++ {
				e := ifd + 2 + k*12
				if e+12 > len(t) {
					break
				}
				if o.Uint16(t[e:e+2]) == 0x0112 {
					return int(o.Uint16(t[e+8 : e+10]))
				}
			}
			return 1
		}
		if m == 0xDA {
			return 1
		}
		i += 2 + l
	}
	return 1
}

// orient aplica a orientação EXIF (1 a 8).
func orient(src image.Image, o int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 && o <= 8 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx, ny := x, y
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// ImageToPDF gera um PDF de uma página A4 (em pé ou deitada, conforme a imagem) com a imagem centralizada e escalada para caber nas
// margens de 10 mm. PNG, JPEG e WebP; a transparência vira fundo branco.
func ImageToPDF(inPath, outPath string) error {
	return safe(func() error { return imageToPDF(inPath, outPath) })
}

func imageToPDF(inPath, outPath string) error {
	data, err := os.ReadFile(inPath) //nolint:gosec // caminho montado pelo servidor dentro do diretório temporário da montagem
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrImage, err.Error())
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > MaxImagePixels {
		return fmt.Errorf("%w: %d x %d pixels", ErrImage, cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrImage, err.Error())
	}
	if o := exifOrientation(data); o != 1 {
		img = orient(img, o)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Over)
	data, img = nil, nil //nolint:ineffassign,wastedassign // solta as referências para o coletor antes de recodificar
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, rgba, &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	w, h := float64(rgba.Bounds().Dx()), float64(rgba.Bounds().Dy())
	pdf := fpdf.New(map[bool]string{true: "L", false: "P"}[w > h], "mm", "A4", "")
	pdf.AddPage()
	pw, ph := pdf.GetPageSize()
	const m = 10.0
	sc := min((pw-2*m)/w, (ph-2*m)/h)
	dw, dh := w*sc, h*sc
	opt := fpdf.ImageOptions{ImageType: "JPG"}
	pdf.RegisterImageOptionsReader("img", opt, bytes.NewReader(jb.Bytes()))
	pdf.ImageOptions("img", (pw-dw)/2, (ph-dh)/2, dw, dh, false, opt, 0, "")
	return pdf.OutputFileAndClose(outPath)
}
