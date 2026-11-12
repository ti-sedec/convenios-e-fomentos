import type { TokenPair } from '@/lib/api/types';

/**
 * Cliente OIDC do MT Login (Keycloak): Authorization Code + PKCE. Roda só no servidor do Next; o
 * navegador nunca vê os tokens. As URLs vêm de variáveis de ambiente (sem NEXT_PUBLIC_).
 */

interface OidcConfig {
  authorizationUri: string;
  tokenUri: string;
  endSessionUri: string;
  clientId: string;
  /** Só para clients confidenciais; o padrão é um client público com PKCE. */
  clientSecret?: string;
  redirectUri: string;
  /** Escopos pedidos no login. Para assinar com o MT-ID inclua `signature-requester` (precisa estar liberado no client). */
  scope: string;
}

const REQUIRED = [
  'MTLOGIN_AUTHORIZATION_URI',
  'MTLOGIN_TOKEN_URI',
  'MTLOGIN_END_SESSION_URI',
  'MTLOGIN_CLIENT_ID',
  'MTLOGIN_REDIRECT_URI'
] as const;

export class OidcConfigError extends Error {
  constructor(missing: string[]) {
    super(`Login pelo MT Login não configurado: defina ${missing.join(', ')}.`);
    this.name = 'OidcConfigError';
  }
}

export function oidcConfig(): OidcConfig {
  const missing = REQUIRED.filter((name) => !process.env[name]?.trim());
  if (missing.length > 0) throw new OidcConfigError([...missing]);
  return {
    authorizationUri: process.env.MTLOGIN_AUTHORIZATION_URI!,
    tokenUri: process.env.MTLOGIN_TOKEN_URI!,
    endSessionUri: process.env.MTLOGIN_END_SESSION_URI!,
    clientId: process.env.MTLOGIN_CLIENT_ID!,
    clientSecret: process.env.MTLOGIN_CLIENT_SECRET?.trim() || undefined,
    redirectUri: process.env.MTLOGIN_REDIRECT_URI!,
    scope: process.env.MTLOGIN_SCOPE?.trim() || 'openid profile email'
  };
}

/**
 * Origem pública do frontend, derivada da URL de retorno. O `req.nextUrl.origin` não serve atrás
 * do Docker ou de um proxy reverso (aparece o host interno).
 */
export function appOrigin(): string {
  return new URL(oidcConfig().redirectUri).origin;
}

/** Como `appOrigin`, mas devolve `fallback` se o login ainda não estiver configurado (para exibir o erro). */
export function appOriginOr(fallback: string): string {
  try {
    return appOrigin();
  } catch {
    return fallback;
  }
}

function base64url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString('base64url');
}

function randomToken(bytes = 32): string {
  return base64url(crypto.getRandomValues(new Uint8Array(bytes)));
}

async function challengeFor(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}

/** Estado do login em andamento, guardado num cookie curto entre a ida e a volta do Keycloak. */
export interface LoginFlow {
  state: string;
  nonce: string;
  verifier: string;
  /** Destino pós-login (já validado por `safeRedirect` na volta). */
  redirect: string | null;
}

export async function startLogin(
  redirect: string | null
): Promise<{ url: string; flow: LoginFlow }> {
  const cfg = oidcConfig();
  const flow: LoginFlow = {
    state: randomToken(),
    nonce: randomToken(),
    verifier: randomToken(48),
    redirect
  };
  const url = new URL(cfg.authorizationUri);
  url.search = new URLSearchParams({
    response_type: 'code',
    client_id: cfg.clientId,
    redirect_uri: cfg.redirectUri,
    scope: cfg.scope,
    state: flow.state,
    nonce: flow.nonce,
    code_challenge: await challengeFor(flow.verifier),
    code_challenge_method: 'S256'
  }).toString();
  return { url: url.toString(), flow };
}

interface TokenResponse {
  access_token?: string;
  refresh_token?: string;
  id_token?: string;
  token_type?: string;
  expires_in?: number;
  refresh_expires_in?: number;
  error?: string;
}

/** O provedor recusou o pedido (código/refresh token inválido ou expirado): erro 4xx, sem retry. */
export class OidcRejectedError extends Error {
  constructor(readonly oauthError: string) {
    super(`MT Login recusou o pedido: ${oauthError}`);
    this.name = 'OidcRejectedError';
  }
}

async function tokenRequest(params: Record<string, string>): Promise<TokenResponse> {
  const cfg = oidcConfig();
  const body = new URLSearchParams({ client_id: cfg.clientId, ...params });
  if (cfg.clientSecret) body.set('client_secret', cfg.clientSecret);
  const res = await fetch(cfg.tokenUri, {
    method: 'POST',
    cache: 'no-store',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
    body
  });
  const json = (await res.json().catch(() => ({}))) as TokenResponse;
  if (res.status >= 400 && res.status < 500)
    throw new OidcRejectedError(json.error ?? String(res.status));
  if (!res.ok) throw new Error(`MT Login indisponível (${res.status}).`);
  return json;
}

/** Payload (não verificado) de um JWT recebido diretamente do endpoint de token, via TLS. */
function jwtPayload(token: string): Record<string, unknown> {
  try {
    return JSON.parse(Buffer.from(token.split('.')[1] ?? '', 'base64url').toString('utf8'));
  } catch {
    return {};
  }
}

/** A documentação do MT Login mostra a resposta do /token sem `expires_in`: cai no `exp` do JWT. */
function secondsUntilExpiry(accessToken: string): number {
  const exp = jwtPayload(accessToken).exp;
  return typeof exp === 'number' ? Math.max(exp - Math.floor(Date.now() / 1000), 1) : 60;
}

/**
 * `refresh_token` e `expires_in` podem não vir (a documentação do MT Login os omite): sem refresh
 * token a sessão dura o access token e depois pede novo login.
 */
function toPair(res: TokenResponse, fallbackRefresh?: string): TokenPair {
  if (!res.access_token) throw new Error('Resposta do MT Login sem access token.');
  return {
    accessToken: res.access_token,
    refreshToken: res.refresh_token ?? fallbackRefresh,
    tokenType: res.token_type ?? 'Bearer',
    expiresIn: res.expires_in ?? secondsUntilExpiry(res.access_token),
    refreshExpiresIn: res.refresh_expires_in
  };
}

/** Troca o `code` do callback pelos tokens; confere o `nonce` do id_token quando ele vem. */
export async function exchangeCode(code: string, flow: LoginFlow): Promise<TokenPair> {
  const cfg = oidcConfig();
  const res = await tokenRequest({
    grant_type: 'authorization_code',
    code,
    redirect_uri: cfg.redirectUri,
    code_verifier: flow.verifier
  });
  if (res.id_token && jwtPayload(res.id_token).nonce !== flow.nonce) {
    throw new OidcRejectedError('nonce_mismatch');
  }
  return toPair(res);
}

/**
 * Renova o par de tokens. Sem "Revoke Refresh Token" no Keycloak o refresh token volta igual (ou
 * ausente): nesse caso mantemos o atual.
 */
export async function refreshWithToken(refreshToken: string): Promise<TokenPair> {
  const res = await tokenRequest({ grant_type: 'refresh_token', refresh_token: refreshToken });
  return toPair(res, refreshToken);
}

/**
 * URL de logout do MT Login (Keycloak 25; encerra também o SSO) e volta para a tela de entrada. Sem
 * `id_token_hint` o Keycloak pede uma confirmação antes de sair; guardar o id_token num cookie só
 * aumentaria o risco de estourar o limite de tamanho.
 */
export function endSessionUrl(): string {
  const cfg = oidcConfig();
  const back = `${appOrigin()}/auth/sign-in`;
  const url = new URL(cfg.endSessionUri);
  url.search = new URLSearchParams({
    client_id: cfg.clientId,
    post_logout_redirect_uri: back
  }).toString();
  return url.toString();
}
