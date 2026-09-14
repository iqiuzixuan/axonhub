// 路由权限配置
export type ScopeLevel = 'system' | 'project' | 'any';

export interface RouteConfig {
  path: string;
  requiredScopes?: string[];
  requiredAllScopes?: string[]; // 页面查询依赖的权限，必须全部满足
  scopeLevel?: ScopeLevel; // 权限级别：system 只检查系统级权限，project 只检查项目级权限，any 检查两者
  mode?: 'hidden' | 'disabled'; // 当没有权限时的处理方式
  children?: RouteConfig[];
  requireRequestDetails?: boolean;
  requireProjectOwner?: boolean; // 是否需要 project owner (或 system owner)
}

export interface RouteGroup {
  title: string;
  scopeLevel?: ScopeLevel; // 路由组的默认权限级别
  routes: RouteConfig[];
}

// 定义所有路由的权限配置
export const routeConfigs: RouteGroup[] = [
  {
    title: 'Admin',
    scopeLevel: 'system', // Admin 路由组只能通过 system-level 权限访问
    routes: [
      {
        path: '/',
        requiredScopes: ['read_dashboard'],
        mode: 'hidden',
      },
      {
        path: '/projects',
        requiredScopes: ['read_projects'],
        mode: 'hidden',
      },
      {
        path: '/users',
        requiredScopes: ['read_users'],
        mode: 'hidden',
      },
      {
        path: '/roles',
        requiredScopes: ['read_roles'],
        mode: 'hidden',
      },
      {
        path: '/channels',
        requiredScopes: ['read_channels'],
        mode: 'hidden',
      },
      {
        path: '/models',
        requiredScopes: ['read_channels'],
        mode: 'hidden',
      },
      {
        path: '/prompt-protection-rules',
        requiredScopes: ['read_channels'],
        mode: 'hidden',
      },
      {
        path: '/data-storages',
        requiredScopes: ['read_data_storages'],
        mode: 'hidden',
      },
      {
        path: '/api-keys',
        requiredScopes: ['read_api_keys'],
        mode: 'hidden',
      },
      {
        path: '/system',
        requiredScopes: ['read_settings'],
        mode: 'hidden',
      },
      {
        path: '/permission-demo',
        // 权限演示页面所有用户都可以访问
      },
    ],
  },
  {
    title: 'Personal',
    routes: [
      { path: '/me/dashboard', mode: 'hidden' },
      { path: '/me/models', mode: 'hidden' },
    ],
  },
  {
    title: 'Project',
    scopeLevel: 'any', // Project 路由组可以通过 system-level 或 project-level 权限访问
    routes: [
      {
        path: '/project/api-keys',
        requiredScopes: ['read_api_keys'],
        mode: 'hidden',
      },
      {
        path: '/project/prompts',
        requiredScopes: ['read_prompts'],
        mode: 'hidden',
      },
      {
        path: '/project/requests',
        requiredScopes: ['read_requests'],
        mode: 'hidden',
      },
      {
        path: '/project/usage-logs',
        requiredScopes: ['read_requests'],
        mode: 'hidden',
      },
      {
        path: '/project/usage-stats',
        requiredScopes: ['read_requests'],
        mode: 'hidden',
        requireProjectOwner: true,
      },
      {
        path: '/project/traces',
        requiredScopes: ['read_requests'],
        mode: 'hidden',
      },
      {
        path: '/project/threads',
        requiredScopes: ['read_requests'],
        mode: 'hidden',
      },
      {
        path: '/project/users',
        // ProjectUsers queries the Project node before reading its members.
        requiredAllScopes: ['read_projects', 'read_users'],
        mode: 'hidden',
      },
      {
        path: '/project/roles',
        requiredScopes: ['read_roles'],
        mode: 'hidden',
      },
      {
        path: '/project/playground',
        requiredScopes: ['write_requests', 'read_channels'],
        mode: 'hidden',
      },
    ],
  },
  {
    title: 'Settings',
    routes: [
      {
        path: '/settings',
        // Profile 设置所有用户都可以访问
      },
      {
        path: '/settings/profile',
        // Profile 设置所有用户都可以访问
      },
      {
        path: '/settings/appearance',
        // Appearance 设置所有用户都可以访问
      },
      {
        path: '/settings/notifications',
        // Notifications 设置所有用户都可以访问
      },
    ],
  },
];

// 获取路由配置的辅助函数
export function getRouteConfig(path: string): RouteConfig | undefined {
  const pathname = path.split(/[?#]/)[0].replace(/\/+$/, '') || '/';
  let matchedRoute: RouteConfig | undefined;

  const visitRoute = (route: RouteConfig, scopeLevel?: ScopeLevel) => {
    const resolvedRoute = { ...route, scopeLevel: route.scopeLevel ?? scopeLevel };
    if (pathname === route.path || (route.path !== '/' && pathname.startsWith(`${route.path}/`))) {
      if (!matchedRoute || route.path.length > matchedRoute.path.length) {
        matchedRoute = resolvedRoute;
      }
    }
    route.children?.forEach((child) => visitRoute(child, resolvedRoute.scopeLevel));
  };

  for (const group of routeConfigs) {
    group.routes.forEach((route) => visitRoute(route, group.scopeLevel));
  }
  return matchedRoute;
}

export interface RoutePermissions {
  systemScopes: string[];
  projectScopes: string[];
  isOwner: boolean;
  isProjectOwner: boolean;
}

// Full administrators manage both members and roles. Evaluate system and
// project grants independently so permissions from different levels never mix.
export function canReadRequestDetails(permissions: RoutePermissions, scopeLevel: ScopeLevel = 'any'): boolean {
  const isAdmin = (scopes: string[]) => ['read_requests', 'write_users', 'write_roles'].every((scope) => scopes.includes(scope));
  return (
    permissions.isOwner ||
    isAdmin(permissions.systemScopes) ||
    (scopeLevel !== 'system' && (permissions.isProjectOwner || isAdmin(permissions.projectScopes)))
  );
}

// 检查用户是否有访问路由的权限
export function hasRouteAccess(permissions: RoutePermissions, routeConfig: RouteConfig): boolean {
  const { systemScopes, projectScopes, isOwner, isProjectOwner } = permissions;
  if (routeConfig.requireRequestDetails && !canReadRequestDetails(permissions, routeConfig.scopeLevel)) return false;
  if (routeConfig.requireProjectOwner && !isOwner && !isProjectOwner) {
    return false;
  }

  const scopeLevel = routeConfig.scopeLevel ?? 'any';
  if (isOwner || (isProjectOwner && scopeLevel !== 'system')) {
    return true;
  }

  const scopes = scopeLevel === 'system' ? systemScopes : scopeLevel === 'project' ? projectScopes : [...systemScopes, ...projectScopes];
  const hasScope = (scope: string) => scopes.includes('*') || scopes.includes(scope);
  const { requiredScopes = [], requiredAllScopes = [] } = routeConfig;

  return (requiredScopes.length === 0 || requiredScopes.some(hasScope)) && requiredAllScopes.every(hasScope);
}

// 检查用户是否有访问路由组的权限
export function hasGroupAccess(permissions: RoutePermissions, group: RouteGroup): boolean {
  return group.routes.some((route) => hasRouteAccess(permissions, { ...route, scopeLevel: route.scopeLevel ?? group.scopeLevel }));
}

export function getDefaultRoute(permissions: RoutePermissions, _hasSelectedProject: boolean): string {
  // Personal pages need only a signed-in session, even without project scopes.
  return hasRouteAccess(permissions, { path: '/', requiredScopes: ['read_dashboard'], scopeLevel: 'system' })
    ? '/'
    : '/me/dashboard';
}
