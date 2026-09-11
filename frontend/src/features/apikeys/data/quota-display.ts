import type { ApiKey, ApiKeyProfile, ApiKeyQuotaUsage } from './schema';

export type ApiKeyQuota = NonNullable<ApiKeyProfile['quota']>;
export type QuotaMetric = {
  key: 'requests' | 'totalTokens' | 'cost';
  label: string;
  limit: number | null;
  used: number | null;
  remaining: number | null;
  remainingPercent: number | null;
  exhausted: boolean;
};

export function getActiveQuotaProfile(apiKey: ApiKey) {
  const name = apiKey.profiles?.activeProfile;
  return name ? apiKey.profiles?.profiles?.find((profile) => profile.name === name) : undefined;
}

export function getQuotaMetrics(quota: ApiKeyQuota, usage?: ApiKeyQuotaUsage): QuotaMetric[] {
  const fields = [
    ['requests', 'requestCount', 'apikeys.profiles.quotaRequests'],
    ['totalTokens', 'totalTokens', 'apikeys.profiles.quotaTotalTokens'],
    ['cost', 'totalCost', 'apikeys.profiles.quotaCost'],
  ] as const;

  return fields.map(([key, usageKey, label]) => {
    const limit = quota[key] ?? null;
    const rawUsed = usage?.[usageKey];
    const used = rawUsed != null && Number.isFinite(rawUsed) ? Math.max(0, rawUsed) : null;
    const remaining = limit != null && used != null ? Math.max(0, limit - used) : null;
    const remainingPercent = remaining != null && limit != null ? (limit <= 0 ? 0 : Math.min(100, (remaining / limit) * 100)) : null;
    return { key, label, limit, used, remaining, remainingPercent, exhausted: limit != null && used != null && used >= limit };
  });
}

// Compare only the effective window, ignoring inactive form fields and object key order.
export function isSameQuotaPeriod(a: ApiKeyQuota['period'], b: ApiKeyQuota['period']) {
  if (a.type !== b.type) return false;
  if (a.type === 'past_duration') return a.pastDuration?.value === b.pastDuration?.value && a.pastDuration?.unit === b.pastDuration?.unit;
  if (a.type === 'calendar_duration') return a.calendarDuration?.unit === b.calendarDuration?.unit;
  return true;
}

export function quotaRemainingColor(remaining: number | null) {
  if (remaining == null) return 'text-muted-foreground';
  if (remaining <= 20) return 'text-red-500';
  if (remaining <= 50) return 'text-yellow-600 dark:text-yellow-500';
  return 'text-green-600 dark:text-green-500';
}

export function quotaRemainingBarColor(remaining: number) {
  if (remaining <= 20) return 'bg-red-500';
  if (remaining <= 50) return 'bg-yellow-500';
  return 'bg-green-500';
}
