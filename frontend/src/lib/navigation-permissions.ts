import type { NavGroup, NavItem } from '@/components/layout/types';

type CanAccessRoute = (path: string) => boolean;

export function filterNavItems(items: NavItem[], canAccessRoute: CanAccessRoute): NavItem[] {
  return items.flatMap((item): NavItem[] => {
    if (item.items) {
      const children = item.items.filter((child) => child.url && canAccessRoute(child.url));
      return children.length > 0 ? [{ ...item, items: children }] : [];
    }

    return item.url && canAccessRoute(item.url) ? [item] : [];
  });
}

export function filterNavGroups(groups: NavGroup[], canAccessRoute: CanAccessRoute): NavGroup[] {
  return groups
    .map((group) => ({ ...group, items: filterNavItems(group.items, canAccessRoute) }))
    .filter((group) => group.items.length > 0);
}
