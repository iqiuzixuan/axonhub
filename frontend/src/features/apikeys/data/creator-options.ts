import { useQuery } from '@tanstack/react-query';
import { graphqlRequest } from '@/gql/graphql';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/stores/authStore';
import { useSelectedProjectId } from '@/stores/projectStore';
import { useErrorHandler } from '@/hooks/use-error-handler';
import { useRequestPermissions } from '@/hooks/useRequestPermissions';

export const API_KEY_CREATOR_OPTIONS_QUERY = `
  query APIKeyCreatorOptions($where: UserWhereInput!) {
    users(first: 100, where: $where) {
      edges {
        node {
          id
          name
          email
        }
      }
    }
  }
`;

interface CreatorOption {
  id: string;
  name: string;
  email: string;
}

export function useApiKeyCreatorOptions(options?: { enabled?: boolean }) {
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();
  const selectedProjectId = useSelectedProjectId();
  const accessToken = useAuthStore((state) => state.auth.accessToken);
  const { canViewUsers } = useRequestPermissions();

  return useQuery({
    queryKey: ['apiKeyCreatorOptions', selectedProjectId, accessToken],
    enabled: options?.enabled !== false && canViewUsers && !!selectedProjectId && !!accessToken,
    queryFn: async () => {
      try {
        const data = await graphqlRequest<{ users: { edges: { node: CreatorOption }[] } }>(
          API_KEY_CREATOR_OPTIONS_QUERY,
          { where: { hasProjectsWith: [{ id: selectedProjectId }] } },
          { 'X-Project-ID': selectedProjectId! }
        );
        return data.users.edges.map((edge) => edge.node);
      } catch (error) {
        handleError(error, t('common.errors.loadFailed'));
        throw error;
      }
    },
  });
}
