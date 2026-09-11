import { useCallback, useMemo, useState } from 'react';
import type { Updater, VisibilityState } from '@tanstack/react-table';
import {
  loadApiKeyColumnOverrides,
  resolveApiKeyColumnVisibility,
  saveApiKeyColumnOverrides,
  updateApiKeyColumnOverrides,
} from '../data/column-visibility';

export function useApiKeyColumnVisibility(userId: string | undefined, isAdmin: boolean) {
  const [stored, setStored] = useState(() => ({ userId, overrides: loadApiKeyColumnOverrides(userId) }));
  // Resolve the new account immediately, without briefly showing the old account's preferences.
  const overrides = useMemo(() => (stored.userId === userId ? stored.overrides : loadApiKeyColumnOverrides(userId)), [stored, userId]);
  const columnVisibility = useMemo(() => resolveApiKeyColumnVisibility(isAdmin, overrides), [isAdmin, overrides]);
  const onColumnVisibilityChange = useCallback(
    (updater: Updater<VisibilityState>) => {
      const next = typeof updater === 'function' ? updater(columnVisibility) : updater;
      const nextOverrides = updateApiKeyColumnOverrides(columnVisibility, next, overrides);
      setStored({ userId, overrides: nextOverrides });
      saveApiKeyColumnOverrides(userId, nextOverrides);
    },
    [columnVisibility, overrides, userId]
  );

  return { columnVisibility, onColumnVisibilityChange };
}
