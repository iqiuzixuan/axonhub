import { useMemo } from 'react';
import { NAV_GROUP_DEFS } from '@/config/nav-items';
import { Command } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/stores/authStore';
import { useSidebarPrefsStore } from '@/stores/sidebarPrefsStore';
import { formatUserName, userNameInitials } from '@/lib/utils';
import { useRoutePermissions } from '@/hooks/useRoutePermissions';
import { useMe } from '@/features/auth/data/auth';
import { type SidebarData, type NavGroup, type NavLink } from './components/layout/types';

// Translates the structural navigation definition into renderable groups.
// Shared with the "customize menu" dialog so it can list the same items.
export function useRawNavGroups(): NavGroup[] {
  const { t } = useTranslation();

  return useMemo(
    () =>
      NAV_GROUP_DEFS.map((group) => ({
        title: t(group.titleKey),
        items: group.items.map(
          (item) =>
            ({
              title: t(item.titleKey),
              url: item.url,
              icon: item.icon,
              mobileOnly: item.mobileOnly,
            }) as NavLink
        ),
      })),
    [t]
  );
}

// Removes hidden items and drops groups that become empty. Pure so it can be
// unit tested without React.
export function applyHiddenNavItems(groups: NavGroup[], hiddenItems: string[]): NavGroup[] {
  if (hiddenItems.length === 0) {
    return groups;
  }

  return groups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !('url' in item) || !hiddenItems.includes(item.url as string)),
    }))
    .filter((group) => group.items.length > 0);
}

export function useSidebarData(): SidebarData {
  const { t } = useTranslation();
  const { user: authUser } = useAuthStore((state) => state.auth);
  const { data: meData } = useMe();
  const { filterNavGroups } = useRoutePermissions();
  const hiddenItems = useSidebarPrefsStore((state) => state.hiddenItems);

  // Use data from me query if available, otherwise fall back to auth store
  const user = meData || authUser;

  const rawNavGroups = useRawNavGroups();

  // Filter by permission first, then by the user's own visibility preferences.
  const filteredNavGroups = useMemo(
    () => applyHiddenNavItems(filterNavGroups(rawNavGroups), hiddenItems),
    [filterNavGroups, rawNavGroups, hiddenItems]
  );

  return {
    user: {
      name: formatUserName(user?.name, user?.email) || t('users.badges.user'),
      email: user?.email || 'user@example.com',
      avatar: user?.avatar || userNameInitials(user?.name, user?.email),
    },
    teams: [
      {
        name: t('sidebar.team.name'),
        logo: Command,
        description: '',
        // DO NOT USE THIS
        // plan: t('sidebar.team.plan'),
      },
    ],
    navGroups: filteredNavGroups,
  };
}
