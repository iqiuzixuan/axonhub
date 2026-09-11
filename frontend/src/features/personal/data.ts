import { z } from 'zod';
import { useQuery } from '@tanstack/react-query';
import { graphqlRequest } from '@/gql/graphql';
import { useAuthStore } from '@/stores/authStore';
import { usePersonalScope } from '@/stores/personalWorkspaceStore';
import { modelCardSchema, modelTypeSchema } from '@/features/models/data/schema';

export const personalKeySchema = z.object({
  id: z.string(),
  name: z.string(),
  projectId: z.string(),
  projectName: z.string(),
  status: z.enum(['enabled', 'disabled', 'archived']),
  deleted: z.boolean(),
  activeProfile: z.string(),
});
export type PersonalKey = z.infer<typeof personalKeySchema>;
const workspaceSchema = z.object({
  timezone: z.string(),
  currencyCode: z.string(),
  today: z.string(),
  apiKeys: z.array(personalKeySchema),
});
export const metricsSchema = z.object({
  requests: z.number(),
  successfulRequests: z.number(),
  failedRequests: z.number(),
  canceledRequests: z.number(),
  pendingRequests: z.number(),
  inputTokens: z.number(),
  outputTokens: z.number(),
  cachedTokens: z.number(),
  totalTokens: z.number(),
  cost: z.number(),
  unpricedUsageCount: z.number(),
  successRate: z.number().nullable(),
});
export type UsageMetrics = z.infer<typeof metricsSchema>;
const quotaSchema = z.object({
  profileName: z.string(),
  start: z.string().nullable(),
  end: z.string().nullable(),
  requests: z.number(),
  tokens: z.number(),
  cost: z.number(),
  limit: z.object({
    requests: z.number().nullable(),
    totalTokens: z.number().nullable(),
    cost: z.coerce.number().nullable(),
    period: z.object({
      type: z.enum(['all_time', 'past_duration', 'calendar_duration']),
      pastDuration: z.object({ value: z.number(), unit: z.enum(['minute', 'hour', 'day']) }).nullable(),
      calendarDuration: z.object({ unit: z.enum(['day', 'week', 'month']) }).nullable(),
    }),
  }),
});
export const dashboardSchema = z.object({
  timezone: z.string(),
  overview: metricsSchema,
  daily: z.array(z.object({ date: z.string(), metrics: metricsSchema })),
  models: z.array(z.object({ modelId: z.string(), metrics: metricsSchema })),
  apiKeys: z.array(z.object({ apiKey: personalKeySchema, metrics: metricsSchema, quota: quotaSchema.nullable() })),
});
export type PersonalDashboard = z.infer<typeof dashboardSchema>;
export type PersonalQuota = z.infer<typeof quotaSchema>;
const personalModelSchema = z.object({
  id: z.string(),
  modelId: z.string(),
  name: z.string(),
  developer: z.string(),
  icon: z.string(),
  group: z.string(),
  type: modelTypeSchema.nullable(),
  status: z.enum(['enabled', 'disabled', 'archived']),
  createdAt: z.coerce.date(),
  updatedAt: z.coerce.date(),
  modelCard: modelCardSchema.nullable(),
  apiKeys: z.array(personalKeySchema),
});
export type PersonalModel = z.infer<typeof personalModelSchema>;

const keyFields = 'id name projectId projectName status deleted activeProfile';
const metricFields = `
  requests successfulRequests failedRequests canceledRequests pendingRequests
  inputTokens outputTokens cachedTokens totalTokens cost unpricedUsageCount successRate
`;
const workspaceQuery = `query MyWorkspace($projectId: ID) {
  myWorkspace(projectId: $projectId) { timezone currencyCode today apiKeys { ${keyFields} } }
}`;
const dashboardQuery = `query MyDashboard($input: PersonalUsageInput!) {
  myDashboard(input: $input) {
    timezone overview { ${metricFields} }
    daily { date metrics { ${metricFields} } }
    models { modelId metrics { ${metricFields} } }
    apiKeys {
      apiKey { ${keyFields} } metrics { ${metricFields} }
      quota { profileName start end requests tokens cost
        limit { requests totalTokens cost period {
          type pastDuration { value unit } calendarDuration { unit }
        } }
      }
    }
  }
}`;
const modelsQuery = `query MyModels($input: PersonalModelsInput) {
  myModels(input: $input) {
    id modelId name developer icon group type status createdAt updatedAt
    modelCard {
      reasoning { supported default } toolCall temperature vision
      modalities { input output } cost { input output cacheRead cacheWrite }
      limit { context output } knowledge releaseDate lastUpdated
    }
    apiKeys { ${keyFields} }
  }
}`;

export function usePersonalWorkspace() {
  const token = useAuthStore((state) => state.auth.accessToken);
  const { projectId } = usePersonalScope();
  return useQuery({
    queryKey: ['personalWorkspace', token, projectId],
    queryFn: async ({ signal }) => {
      const response = await graphqlRequest<{ myWorkspace: unknown }>(workspaceQuery, { projectId }, undefined, { signal });
      return workspaceSchema.parse(response.myWorkspace);
    },
    enabled: !!token,
    staleTime: 15_000,
    refetchInterval: 60_000,
  });
}
export function usePersonalDashboard(startDate: string, endDate: string, enabled: boolean) {
  const token = useAuthStore((state) => state.auth.accessToken);
  const { projectId, apiKeyId } = usePersonalScope();
  return useQuery({
    queryKey: ['personalDashboard', token, projectId, apiKeyId, startDate, endDate],
    queryFn: async ({ signal }) => {
      const response = await graphqlRequest<{ myDashboard: unknown }>(
        dashboardQuery,
        { input: { projectId, apiKeyId, startDate, endDate } },
        undefined,
        { signal }
      );
      return dashboardSchema.parse(response.myDashboard);
    },
    enabled: !!token && enabled,
    staleTime: 15_000,
    refetchInterval: 60_000,
  });
}
export function usePersonalModels(search: string) {
  const token = useAuthStore((state) => state.auth.accessToken);
  const { projectId, apiKeyId } = usePersonalScope();
  return useQuery({
    queryKey: ['personalModels', token, projectId, apiKeyId, search],
    queryFn: async ({ signal }) => {
      const response = await graphqlRequest<{ myModels: unknown }>(modelsQuery, { input: { projectId, apiKeyId, search } }, undefined, {
        signal,
      });
      return z.array(personalModelSchema).parse(response.myModels);
    },
    enabled: !!token,
    staleTime: 15_000,
    refetchInterval: 60_000,
  });
}
