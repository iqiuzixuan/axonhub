import { useCallback } from 'react';
import { create } from 'zustand';
import { useAuthStore } from './authStore';

const useWorkspaceSelection = create<{
  account: string;
  projectId: string | null;
  apiKeyId: string | null;
  set: (account: string, projectId: string | null, apiKeyId: string | null) => void;
}>((set) => ({
  account: '',
  projectId: null,
  apiKeyId: null,
  set: (account, projectId, apiKeyId) => set({ account, projectId, apiKeyId }),
}));

// Personal scope is independent of project administration and reset immediately
// when the account changes. A null project means all currently accessible projects.
export function usePersonalScope() {
  const account = useAuthStore((state) => state.auth.accessToken);
  const selection = useWorkspaceSelection();
  const projectId = selection.account === account ? selection.projectId : null;
  const apiKeyId = selection.account === account ? selection.apiKeyId : null;
  const setProjectId = useCallback((value: string | null) => selection.set(account, value, null), [account, selection.set]);
  const setAPIKeyId = useCallback((value: string | null) => selection.set(account, projectId, value), [account, projectId, selection.set]);
  return {
    projectId,
    apiKeyId,
    setProjectId,
    setAPIKeyId,
  };
}
