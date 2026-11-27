# Navegação e permissões (RBAC)

A visibilidade de menus e botões segue as **permissões efetivas** do administrador logado. É só experiência de uso: a autorização real
é sempre do backend, que responde 403 a qualquer ação sem permissão.

## De onde vêm as permissões

1. O layout do dashboard (`src/app/dashboard/layout.tsx`) chama `getSession()` (`src/lib/auth/session.ts`), que consulta
   `GET /api/admin/auth/me` uma vez por requisição.
2. A resposta (`AdminMe`, em `src/lib/api/types.ts`) traz `isSuperAdmin`, as `roles` e as `permissions` já unidas entre todas as roles:
   `{ action, subject, fields, conditions }`.
3. O `SessionProvider` (`src/features/auth/session-provider.tsx`) repassa isso ao cliente.
4. Quem não é Super Admin e não tem nenhuma permissão vê "Aguardando liberação" em vez do painel.

As ações têm o formato `admin::<recurso>.<verbo>` (ex.: `admin::settings.users.read`, `admin::projects.read`). O catálogo
completo está em `GET /api/admin/catalog/actions`.

## No cliente: `useCan` e `<Can>`

```tsx
const can = useCan();                       // Super Admin sempre passa
{can('admin::settings.users.create') && <NewAdminDialog />}

<Can action='admin::settings.roles.delete'>
  <Button>Excluir</Button>
</Can>
```

`useCan` só confere a **ação**; campos e condições não são avaliados no cliente (o backend filtra e recusa).

## Menu lateral e Cmd+K

Cada item de `src/config/nav-config.ts` pode exigir uma ação:

```ts
{
  title: 'Perfis e permissões',
  url: '/dashboard/administracao/perfis',
  icon: 'shield',
  access: { permission: 'admin::settings.roles.read' },
  items: []
}
```

Quando uma ação só não distingue os perfis, o item pode também declarar `access.when`, um critério sobre a sessão inteira:

| `when` | Quem passa | Por quê |
|---|---|---|
| `project-proponent` | quem cadastra projetos (`admin::projects.create`) | "Meus projetos" é do Proponente; a equipe também tem `projects.read` |
| `project-staff` | quem lê projetos além dos que cadastrou | O Proponente e a equipe têm a mesma ação `projects.read`; o que os separa são as **condições** da permissão (o Proponente só tem "é o criador" e "é membro"; a equipe tem as de projeto enviado). Ver `isProjectStaff` em `src/features/projects/access.ts` |

O Super Admin passa nos dois. Como `useCan`, é só para esconder menu: a autorização real é do backend. Os critérios dependem dos **nomes** das condições do backend (`members.go` e `visibility.go`); se mudarem lá, mudam em `access.ts`.

`useFilteredNavGroups` / `useFilteredNavItems` (`src/hooks/use-nav.ts`) escondem os itens sem permissão e os grupos que ficam vazios.
Os tipos estão em `src/types/index.ts` (`NavItem.access`).

## Páginas

Esconder o menu não protege a rota. As páginas de administração conferem a permissão no servidor e mostram um aviso em português:

```tsx
export default async function UsuariosPage() {
  return (
    <PageContainer access={await hasPermission(adminActions.read)} pageTitle='Usuários'>
      …
    </PageContainer>
  );
}
```

- `hasPermission` (`src/lib/auth/access.ts`) usa a mesma sessão da requisição.
- `PageContainer` mostra `AccessDenied` (`src/components/access-denied.tsx`) quando `access` é `false`.
- Os dados continuam protegidos pela API: sem permissão a consulta devolve 403, e o `QueryError` (`src/components/query-error.tsx`)
  mostra "Você não tem permissão para ver …".

## Constantes de ação

Cada feature declara suas ações em `api/types.ts` (ex.: `adminActions`, `roleActions`, `departmentActions`), para não espalhar strings pela interface.

## Leituras auxiliares

Alguns seletores precisam de listas que pertencem a outro recurso. O backend as libera a quem administra o recurso que as usa:

| Lista | Também liberada para |
|---|---|
| `GET /admin/departments` | `settings.users.read` |
| `GET /admin/roles` (só nomes e contagens) | `settings.users.read` |

As permissões de cada role (`GET /admin/roles/{id}`) e o catálogo de condições continuam exigindo `admin::settings.roles.read`.

## Editor de permissões

`src/features/roles/components/permission-editor.tsx` espelha as regras anti-escalonamento do backend: o Super Admin é somente leitura, roles
que o próprio usuário possui são somente leitura e permissões que ele não tem não podem ser adicionadas. O backend é a garantia: se
recusar (`privilege_escalation`), a tela lista o que não pôde ser concedido.
