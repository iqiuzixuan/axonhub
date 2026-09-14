import { useMemo } from 'react';
import { canReadRequestDetails } from '@/config/route-permission';
import { useAuthStore } from '@/stores/authStore';
import { useSelectedProjectId } from '@/stores/projectStore';
import { useMe } from '@/features/auth/data/auth';

export interface RequestPermissions {
  canViewDetails: boolean;
  canViewUsers: boolean;
  canViewApiKeys: boolean;
  canViewChannels: boolean;
  canViewRoles: boolean;
  canViewCallerUser: boolean;
}

/** Returns request-view permissions for the selected project's effective scopes. */
export function useRequestPermissions(projectId?: string | null): RequestPermissions {
  const { user: authUser } = useAuthStore((state) => state.auth);
  const { data: meData } = useMe();
  const selectedProjectId = useSelectedProjectId();
  const effectiveProjectId = projectId === undefined ? selectedProjectId : projectId;

  // Use data from me query if available, otherwise fall back to auth store
  const user = meData || authUser;
  const systemScopes = user?.scopes || [];
  const isOwner = user?.isOwner || false;

  // Get project-level scopes for the selected project
  const projectScopes = useMemo(() => {
    if (!effectiveProjectId || !user?.projects) {
      return [];
    }
    const project = user.projects.find((p) => p.projectID === effectiveProjectId);
    return project?.effectiveScopes || project?.scopes || [];
  }, [effectiveProjectId, user?.projects]);

  const isProjectOwner = useMemo(() => {
    if (isOwner) return true;
    return user?.projects?.some((project) => project.projectID === effectiveProjectId && project.isOwner) ?? false;
  }, [isOwner, effectiveProjectId, user?.projects]);

  const permissions = useMemo(() => {
    const canViewDetails = canReadRequestDetails({ systemScopes, projectScopes, isOwner, isProjectOwner });
    // 合并系统级和项目级权限
    const userScopes = [...systemScopes, ...projectScopes];

    // Owner用户拥有所有权限
    if (isProjectOwner || userScopes.includes('*')) {
      return {
        canViewDetails,
        canViewUsers: true,
        canViewApiKeys: true,
        canViewChannels: true,
        canViewRoles: true,
        canViewCallerUser: isProjectOwner,
      };
    }

    return {
      canViewDetails,
      canViewUsers: userScopes.includes('read_users'),
      canViewApiKeys: userScopes.includes('read_api_keys'),
      canViewChannels: userScopes.includes('read_channels'),
      canViewRoles: userScopes.includes('read_roles'),
      canViewCallerUser: false,
    };
  }, [systemScopes, projectScopes, isOwner, isProjectOwner]);

  return permissions;
}
