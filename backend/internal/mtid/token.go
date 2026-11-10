package mtid

import "context"

type tokenKey struct{}

// WithToken guarda no contexto o access token do MT Login de quem está fazendo a chamada. O MT-ID autentica o pedido com o token
// do usuário (escopo signature-requester), por isso ele viaja pelo contexto até o cliente em vez de mudar a assinatura de cada
// método do contrato.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// TokenFrom devolve o token guardado por WithToken.
func TokenFrom(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenKey{}).(string)
	return t, ok && t != ""
}
