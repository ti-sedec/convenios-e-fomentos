package storage

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"
)

// Memory é um Store em memória, para testes: as URLs são fictícias e o "envio" é simulado por Put.
type Memory struct {
	mu      sync.Mutex
	objects map[string]memObject
	// Issued guarda o último envio autorizado por chave.
	Issued map[string]Upload
}

type memObject struct {
	data        []byte
	contentType string
}

// NewMemory cria o armazenamento em memória.
func NewMemory() *Memory {
	return &Memory{objects: map[string]memObject{}, Issued: map[string]Upload{}}
}

// Put simula o envio do navegador.
func (m *Memory) Put(key, contentType string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: data, contentType: contentType}
}

// PutObject grava o objeto, como o servidor faria no armazenamento real.
func (m *Memory) PutObject(_ context.Context, key, contentType string, data []byte) error {
	m.Put(key, contentType, append([]byte(nil), data...))
	return nil
}

func (m *Memory) PutObjectStream(_ context.Context, key, contentType string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.Put(key, contentType, data)
	return nil
}

// Has indica se o objeto existe.
func (m *Memory) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

// Len devolve a quantidade de objetos.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

func (m *Memory) PresignUpload(_ context.Context, key, contentType string, maxSize int64, ttl time.Duration) (Upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := Upload{URL: "http://storage.test/bucket", ExpiresAt: time.Now().Add(ttl),
		Fields: map[string]string{"key": key, "Content-Type": contentType}}
	m.Issued[key] = u
	return u, nil
}

func (m *Memory) PresignDownload(_ context.Context, key, filename, _ string, ttl time.Duration) (string, time.Time, error) {
	return "http://storage.test/bucket/" + key + "?filename=" + filename, time.Now().Add(ttl), nil
}

func (m *Memory) PresignView(_ context.Context, key, _ string, ttl time.Duration) (string, time.Time, error) {
	return "http://storage.test/bucket/" + key + "?inline=1", time.Now().Add(ttl), nil
}

func (m *Memory) Stat(_ context.Context, key string) (ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return ObjectInfo{}, ErrNotFound
	}
	return ObjectInfo{Size: int64(len(o.data)), ContentType: o.contentType}, nil
}

func (m *Memory) Head(_ context.Context, key string, n int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	if int64(len(o.data)) > n {
		return o.data[:n], nil
	}
	return o.data, nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) Copy(_ context.Context, src, dst string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[src]
	if !ok {
		return ErrNotFound
	}
	m.objects[dst] = memObject{data: append([]byte(nil), o.data...), contentType: o.contentType}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), o.data...))), nil
}
