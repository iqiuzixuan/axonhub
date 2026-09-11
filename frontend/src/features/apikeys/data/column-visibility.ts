import type { VisibilityState } from '@tanstack/react-table';

export const API_KEY_COLUMN_LABELS: Record<string, string> = {
  id: 'common.columns.id',
  key: 'apikeys.columns.key',
  creator: 'apikeys.columns.creator',
  type: 'apikeys.columns.type',
  status: 'common.columns.status',
  activeProfile: 'apikeys.columns.activeProfile',
  quota: 'apikeys.quota.column',
  createdAt: 'common.columns.createdAt',
  updatedAt: 'common.columns.updatedAt',
};

const storageKey = (userId: string) => `apikeys-table-column-visibility:${userId}`;

export function loadApiKeyColumnOverrides(userId?: string): VisibilityState {
  if (!userId) return {};
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(storageKey(userId)) ?? 'null');
    if (!parsed || typeof parsed !== 'object' || !('v' in parsed) || parsed.v !== 1 || !('overrides' in parsed)) return {};
    const overrides = parsed.overrides;
    if (!overrides || typeof overrides !== 'object' || Array.isArray(overrides)) return {};
    return Object.fromEntries(
      Object.entries(overrides).filter(([id, visible]) => Object.hasOwn(API_KEY_COLUMN_LABELS, id) && typeof visible === 'boolean')
    );
  } catch {
    return {};
  }
}

export function saveApiKeyColumnOverrides(userId: string | undefined, overrides: VisibilityState) {
  if (!userId) return;
  try {
    localStorage.setItem(storageKey(userId), JSON.stringify({ v: 1, overrides }));
  } catch {
    // Column toggles remain usable when browser storage is unavailable.
  }
}

export function resolveApiKeyColumnVisibility(isAdmin: boolean, overrides: VisibilityState): VisibilityState {
  return { quota: !isAdmin, ...overrides };
}

export function updateApiKeyColumnOverrides(previous: VisibilityState, next: VisibilityState, overrides: VisibilityState): VisibilityState {
  const updated = { ...overrides };
  for (const id of Object.keys(API_KEY_COLUMN_LABELS)) {
    if ((previous[id] ?? true) !== (next[id] ?? true)) updated[id] = next[id] ?? true;
  }
  return updated;
}
