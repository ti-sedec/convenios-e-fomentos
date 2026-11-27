import { NavGroup } from '@/types';

/**
 * Configuração da navegação (sidebar e Cmd+K).
 *
 * Cada item pode exigir uma ação do backend em `access.permission`; o Super Admin sempre vê tudo.
 * Ex.: `access: { permission: 'admin::settings.users.read' }`.
 */
export const navGroups: NavGroup[] = [
  {
    label: 'Geral',
    items: [
      {
        title: 'Visão geral',
        url: '/dashboard/overview',
        icon: 'dashboard',
        isActive: false,
        shortcut: ['d', 'd'],
        items: []
      }
    ]
  },
  {
    label: 'Projetos',
    items: [
      {
        title: 'Meus projetos',
        url: '/dashboard/projetos',
        icon: 'kanban',
        isActive: false,
        shortcut: ['m', 'p'],
        // Só quem cadastra projetos: a equipe também tem `projects.read`, e veria todos os projetos sob este título.
        access: { permission: 'admin::projects.read', when: 'project-proponent' },
        items: []
      },
      {
        title: 'Análise de projetos',
        url: '/dashboard/analise',
        icon: 'forms',
        isActive: false,
        shortcut: ['a', 'n'],
        access: { permission: 'admin::projects.read', when: 'project-staff' },
        items: []
      },
      {
        title: 'Perfil de proponente',
        url: '/dashboard/perfil-proponente',
        icon: 'userPen',
        isActive: false,
        shortcut: ['p', 'f'],
        access: { permission: 'admin::proponent-profile.read' },
        items: []
      }
    ]
  },
  {
    label: 'Administração',
    items: [
      {
        title: 'Usuários',
        url: '/dashboard/administracao/usuarios',
        icon: 'teams',
        isActive: false,
        shortcut: ['a', 'u'],
        access: { permission: 'admin::settings.users.read' },
        items: []
      },
      {
        title: 'Perfis e permissões',
        url: '/dashboard/administracao/perfis',
        icon: 'shield',
        isActive: false,
        shortcut: ['a', 'p'],
        access: { permission: 'admin::settings.roles.read' },
        items: []
      },
      {
        title: 'Departamentos',
        url: '/dashboard/administracao/departamentos',
        icon: 'building',
        isActive: false,
        shortcut: ['a', 'd'],
        access: { permission: 'admin::settings.departments.read' },
        items: []
      },
      {
        title: 'Tipos de entidade',
        url: '/dashboard/administracao/tipos-entidade',
        icon: 'building',
        isActive: false,
        shortcut: ['a', 'e'],
        // Quem só lê (o proponente) não vê a tela de manutenção: o menu pede a permissão de criar.
        access: { permission: 'admin::catalog.entity-types.create' },
        items: []
      },
      {
        title: 'Parlamentares',
        url: '/dashboard/administracao/parlamentares',
        icon: 'teams',
        isActive: false,
        shortcut: ['a', 'l'],
        access: { permission: 'admin::catalog.parliamentarians.create' },
        items: []
      },
      {
        title: 'Parâmetros de regras',
        url: '/dashboard/administracao/parametros',
        icon: 'settings',
        isActive: false,
        shortcut: ['a', 'r'],
        access: { permission: 'admin::settings.rule-parameters.create' },
        items: []
      },
      {
        title: 'Auditoria',
        url: '/dashboard/administracao/auditoria',
        icon: 'history',
        isActive: false,
        shortcut: ['a', 'a'],
        access: { permission: 'admin::settings.audit-logs.read' },
        items: []
      }
    ]
  },
  {
    label: 'Conta',
    items: [
      {
        title: 'Meu perfil',
        url: '/dashboard/profile',
        icon: 'account',
        isActive: false,
        shortcut: ['m', 'm'],
        items: []
      }
    ]
  }
];
