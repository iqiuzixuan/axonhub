import { useCallback, useMemo } from 'react';
import {
  getDefaultRoute,
  getRouteConfig,
  hasRouteAccess,
  hasGroupAccess,
  type RouteConfig,
  type RouteGroup,
} from '@/config/route-permission';
import { useAuthStore } from '@/stores/authStore';
import { useSelectedProjectId } from '@/stores/projectStore';
import { filterNavItems as filterItems, filterNavGroups as filterGroups } from '@/lib/navigation-permissions';
import { type NavGroup, type NavItem } from '@/components/layout/types';
import { useMe } from '@/features/auth/data/auth';

export function useRoutePermissions() {
  const { user: authUser } = useAuthStore((state) => state.auth);
  const { data: meData } = useMe();
  const selectedProjectId = useSelectedProjectId();

  // Use data from me query if available, otherwise fall back to auth store
  const user = meData || authUser;
  const systemScopes = user?.scopes || [];
  const isOwner = user?.isOwner || false;

  // Get project-level scopes for the selected project
  const projectScopes = useMemo(() => {
    if (!selectedProjectId || !user?.projects) {
      return [];
    }
    const project = user.projects.find((p) => p.projectID === selectedProjectId);
    return project?.effectiveScopes || project?.scopes || [];
  }, [selectedProjectId, user?.projects]);

  const isProjectOwner = useMemo(() => {
    if (isOwner) {
      return true;
    }
    if (!selectedProjectId || !user?.projects) {
      return false;
    }
    const project = user.projects.find((p) => p.projectID === selectedProjectId);
    return project?.isOwner || false;
  }, [isOwner, selectedProjectId, user?.projects]);

  const permissions = useMemo(
    () => ({ systemScopes, projectScopes, isOwner, isProjectOwner }),
    [systemScopes, projectScopes, isOwner, isProjectOwner]
  );

  const checkAccess = useCallback((routeConfig: RouteConfig) => hasRouteAccess(permissions, routeConfig), [permissions]);

  const checkRouteAccess = useCallback(
    (path: string): { hasAccess: boolean; mode?: 'hidden' | 'disabled' } => {
      const routeConfig = getRouteConfig(path);
      return routeConfig ? { hasAccess: checkAccess(routeConfig), mode: routeConfig.mode } : { hasAccess: true };
    },
    [checkAccess]
  );

  const checkGroupAccess = useCallback((group: RouteGroup) => hasGroupAccess(permissions, group), [permissions]);
  const canAccessRoute = useCallback((path: string) => checkRouteAccess(path).hasAccess, [checkRouteAccess]);
  const filterNavItems = useCallback((items: NavItem[]) => filterItems(items, canAccessRoute), [canAccessRoute]);
  const filterNavGroups = useCallback((groups: NavGroup[]) => filterGroups(groups, canAccessRoute), [canAccessRoute]);

  return {
    userScopes: [...systemScopes, ...projectScopes],
    systemScopes,
    projectScopes,
    isOwner,
    isProjectOwner,
    defaultPath: getDefaultRoute(permissions, !!selectedProjectId),
    hasRouteAccess: checkAccess,
    checkRouteAccess,
    checkGroupAccess,
    filterNavItems,
    filterNavGroups,
  };
}
