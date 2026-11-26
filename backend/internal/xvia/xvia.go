// Pacote xvia monta o cliente HTTP que fala com o X-Via (a barramento de serviços do governo de MT). O X-Via de desenvolvimento usa
// certificado próprio e o nome nele (SAN) não é o host pelo qual se acessa, então o transporte aceita um PEM extra e um nome de
// servidor a conferir. A verificação do certificado fica SEMPRE ligada. Quem usa (MT-ID, SIGADOC) acrescenta o serviço e o
// cabeçalho x-road-client.
package xvia

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// TLS configura o transporte.
type TLS struct {
	// CAFile é um PEM com o certificado a confiar além dos do sistema. Vazio = só os do sistema.
	CAFile string
	// ServerName, se definido, é o nome conferido no certificado (e enviado no SNI) no lugar do host da URL.
	ServerName string
	// Timeout vale para a chamada inteira; zero ou negativo usa 30 s.
	Timeout time.Duration
}

// NewHTTPClient cria o cliente. Não faz chamada de rede.
func NewHTTPClient(c TLS) (*http.Client, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.ServerName}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("lendo o certificado do X-Via: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s não contém certificado PEM", c.CAFile)
		}
		cfg.RootCAs = pool
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}
