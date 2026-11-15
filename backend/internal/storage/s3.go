package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configura o armazenamento S3 (MinIO).
type S3Config struct {
	// Endpoint interno (host:porta), usado para ler e apagar objetos.
	Endpoint string
	UseSSL   bool
	// PublicURL é a URL vista pelo navegador, com o prefixo do proxy (ex.: https://app.gov.br/storage).
	PublicURL string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
}

// S3 implementa Store sobre o MinIO.
type S3 struct {
	bucket string
	// internal fala com o MinIO pela rede interna; public só assina URLs para o host público.
	internal *minio.Client
	public   *minio.Client
	prefix   string
}

// NewS3 cria o cliente. Não faz chamadas de rede.
func NewS3(c S3Config) (*S3, error) {
	pub, err := url.Parse(c.PublicURL)
	if err != nil || pub.Host == "" {
		return nil, fmt.Errorf("storage: URL pública inválida %q", c.PublicURL)
	}
	creds := credentials.NewStaticV4(c.AccessKey, c.SecretKey, "")
	// Estilo de caminho e região fixa: a assinatura vale para /<bucket>/<objeto> e não depende de
	// consultar o servidor, o que permite assinar para o host público sem alcançá-lo.
	opts := func(secure bool) *minio.Options {
		return &minio.Options{Creds: creds, Secure: secure, Region: c.Region, BucketLookup: minio.BucketLookupPath}
	}
	internal, err := minio.New(c.Endpoint, opts(c.UseSSL))
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	public, err := minio.New(pub.Host, opts(pub.Scheme == "https"))
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	return &S3{bucket: c.Bucket, internal: internal, public: public, prefix: strings.TrimRight(pub.Path, "/")}, nil
}

// withPrefix acrescenta o prefixo do proxy DEPOIS da assinatura: o proxy o remove antes de chegar
// ao MinIO, que valida a assinatura sobre /<bucket>/<objeto>.
func (s *S3) withPrefix(u *url.URL) string {
	c := *u
	c.Path = s.prefix + c.Path
	c.RawPath = ""
	return c.String()
}

func (s *S3) PresignUpload(ctx context.Context, key, contentType string, maxSize int64, ttl time.Duration) (Upload, error) {
	exp := time.Now().UTC().Add(ttl)
	pol := minio.NewPostPolicy()
	if err := errors.Join(
		pol.SetBucket(s.bucket),
		pol.SetKey(key),
		pol.SetExpires(exp),
		pol.SetContentType(contentType),
		pol.SetContentLengthRange(1, maxSize),
	); err != nil {
		return Upload{}, fmt.Errorf("storage: política de envio: %w", err)
	}
	u, fields, err := s.public.PresignedPostPolicy(ctx, pol)
	if err != nil {
		return Upload{}, fmt.Errorf("storage: assinando envio: %w", err)
	}
	return Upload{URL: s.withPrefix(u), Fields: fields, ExpiresAt: exp}, nil
}

func (s *S3) PresignDownload(ctx context.Context, key, filename, contentType string, ttl time.Duration) (string, time.Time, error) {
	q := url.Values{}
	q.Set("response-content-disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeName(filename)}))
	q.Set("response-content-type", contentType)
	u, err := s.public.PresignedGetObject(ctx, s.bucket, key, ttl, q)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("storage: assinando download: %w", err)
	}
	return s.withPrefix(u), time.Now().UTC().Add(ttl), nil
}

func (s *S3) PresignView(ctx context.Context, key, contentType string, ttl time.Duration) (string, time.Time, error) {
	q := url.Values{}
	q.Set("response-content-disposition", "inline")
	q.Set("response-content-type", contentType)
	u, err := s.public.PresignedGetObject(ctx, s.bucket, key, ttl, q)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("storage: assinando exibição: %w", err)
	}
	return s.withPrefix(u), time.Now().UTC().Add(ttl), nil
}

// safeName reduz ao nome base e troca controles e aspas por "_". Nomes com acento saem como
// filename* (RFC 2231), que o mime.FormatMediaType gera sozinho.
func safeName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return '_'
		}
		return r
	}, name)
}

func isNotFound(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NotFound" || code == "NoSuchBucket"
}

func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	st, err := s.internal.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, fmt.Errorf("storage: stat: %w", err)
	}
	return ObjectInfo{Size: st.Size, ContentType: st.ContentType}, nil
}

func (s *S3) Head(ctx context.Context, key string, n int64) ([]byte, error) {
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(0, n-1); err != nil {
		return nil, err
	}
	obj, err := s.internal.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo: %w", err)
	}
	defer func() { _ = obj.Close() }()
	b, err := io.ReadAll(io.LimitReader(obj, n))
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: lendo: %w", err)
	}
	return b, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.internal.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil && !isNotFound(err) {
		return fmt.Errorf("storage: removendo: %w", err)
	}
	return nil
}

func (s *S3) PutObject(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.internal.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: gravando: %w", err)
	}
	return nil
}

func (s *S3) PutObjectStream(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	// PartSize fixo e pequeno: o cliente guarda uma parte por vez na memória (o padrão é 16 MiB por parte).
	_, err := s.internal.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType, PartSize: 8 << 20, NumThreads: 1})
	if err != nil {
		return fmt.Errorf("storage: gravando: %w", err)
	}
	return nil
}

func (s *S3) Copy(ctx context.Context, src, dst string) error {
	_, err := s.internal.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: s.bucket, Object: dst},
		minio.CopySrcOptions{Bucket: s.bucket, Object: src})
	if err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("storage: copiando: %w", err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	// GetObject é preguiçoso: sem o Stat, um objeto inexistente só apareceria no primeiro Read.
	if _, err := s.Stat(ctx, key); err != nil {
		return nil, err
	}
	obj, err := s.internal.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: lendo: %w", err)
	}
	return obj, nil
}
