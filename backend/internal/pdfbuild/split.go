package pdfbuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// ErrPageTooLarge: uma única página, sozinha, já passa do limite de tamanho: não há como dividir.
var ErrPageTooLarge = errors.New("uma página sozinha passa do limite de tamanho")

// Split divide o PDF em partes de, no máximo, maxBytes cada, por páginas inteiras e na ordem, gravando-as em dir (part-001.pdf,
// part-002.pdf...). Cada parte leva o maior número de páginas que cabe no limite (busca binária sobre o tamanho real do arquivo
// gerado). Se o arquivo já cabe, devolve só o próprio in, sem copiar. Se uma página sozinha passa do limite, devolve ErrPageTooLarge.
func Split(ctx context.Context, in, dir string, maxBytes int64) ([]string, error) {
	if maxBytes <= 0 {
		return nil, errors.New("limite de tamanho inválido")
	}
	st, err := os.Stat(in)
	if err != nil {
		return nil, err
	}
	if st.Size() <= maxBytes {
		return []string{in}, nil
	}
	pages, err := PageCount(ctx, in)
	if err != nil {
		return nil, err
	}
	// extract grava as páginas a..b em path e devolve o tamanho.
	extract := func(a, b int, path string) (int64, error) {
		if err := safe(func() error {
			return classify(api.TrimFile(ctx, in, path, []string{fmt.Sprintf("%d-%d", a, b)}, conf()))
		}); err != nil {
			return 0, err
		}
		fi, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		return fi.Size(), nil
	}
	var parts []string
	probe := filepath.Join(dir, "probe.pdf")
	defer func() { _ = os.Remove(probe) }()
	for start := 1; start <= pages; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out := filepath.Join(dir, fmt.Sprintf("part-%03d.pdf", len(parts)+1))
		// Tenta o resto de uma vez; senão, a maior quantidade de páginas que cabe.
		size, err := extract(start, pages, out)
		if err != nil {
			return nil, err
		}
		end := pages
		if size > maxBytes {
			lo, hi := start, pages-1 // lo é o menor fim possível (uma página); hi, o maior a testar
			best := 0
			for lo <= hi {
				mid := (lo + hi) / 2
				sz, err := extract(start, mid, probe)
				if err != nil {
					return nil, err
				}
				if sz <= maxBytes {
					best = mid
					lo = mid + 1
				} else {
					hi = mid - 1
				}
			}
			if best == 0 {
				_ = os.Remove(out)
				return nil, fmt.Errorf("%w (página %d, limite de %d bytes)", ErrPageTooLarge, start, maxBytes)
			}
			end = best
			if _, err := extract(start, end, out); err != nil {
				return nil, err
			}
		}
		parts = append(parts, out)
		start = end + 1
	}
	return parts, nil
}
