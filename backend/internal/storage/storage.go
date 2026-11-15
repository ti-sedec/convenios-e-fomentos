// Pacote storage isola o armazenamento de objetos (S3/MinIO) dos arquivos dos documentos.
// O navegador envia e baixa os arquivos direto do armazenamento, por URLs pré-assinadas; a API
// só emite as URLs e confere o resultado.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound indica que o objeto não existe.
var ErrNotFound = errors.New("storage: objeto não encontrado")

// ObjectInfo descreve um objeto armazenado.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Upload é o envio autorizado: o navegador faz um POST multipart para URL com os campos Fields
// (e o arquivo no último campo "file"). O armazenamento recusa o que violar tipo ou tamanho.
type Upload struct {
	URL       string            `json:"url"`
	Fields    map[string]string `json:"fields"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// Store é o contrato do armazenamento.
type Store interface {
	// PresignUpload autoriza um único envio ao objeto key, com exatamente contentType e tamanho
	// entre 1 byte e maxSize.
	PresignUpload(ctx context.Context, key, contentType string, maxSize int64, ttl time.Duration) (Upload, error)
	// PresignDownload devolve uma URL de download temporária, que baixa como anexo com o nome filename.
	PresignDownload(ctx context.Context, key, filename, contentType string, ttl time.Duration) (string, time.Time, error)
	// PresignView devolve uma URL temporária para exibir o objeto no navegador (inline, sem forçar o download), com o tipo dado.
	PresignView(ctx context.Context, key, contentType string, ttl time.Duration) (string, time.Time, error)
	// Stat devolve tamanho e tipo do objeto, ou ErrNotFound.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Head lê até n bytes iniciais do objeto (para conferir a assinatura do formato).
	Head(ctx context.Context, key string, n int64) ([]byte, error)
	// Get abre o objeto para leitura em sequência (hash, verificação de vírus). Quem chama fecha o leitor. Inexistente: ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Copy duplica o objeto src em dst (mesmo conteúdo e tipo), sem passar os bytes pela API. src inexistente: ErrNotFound.
	Copy(ctx context.Context, src, dst string) error
	// PutObject grava o objeto key com o conteúdo dado, sem passar pelo navegador (arquivos que o próprio servidor gera ou recebe de
	// outro sistema). Substitui o objeto se já existir.
	PutObject(ctx context.Context, key, contentType string, data []byte) error
	// PutObjectStream grava o objeto lendo de r (de tamanho size, conhecido), sem carregá-lo inteiro na memória: serve a arquivos
	// grandes que o servidor monta em disco (o relatório completo do projeto).
	PutObjectStream(ctx context.Context, key, contentType string, r io.Reader, size int64) error
	// Delete remove o objeto; remover um objeto inexistente não é erro.
	Delete(ctx context.Context, key string) error
}
