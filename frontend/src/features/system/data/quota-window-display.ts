import type { TFunction } from 'i18next';
import type { ProviderQuotaLimit } from './quotas';

export const QUOTA_WINDOW_LABEL_KEYS: Record<string, string> = {
  '5h': 'quota.window.5h',
  '7d': 'quota.window.7d',
  '30d': 'quota.window.30d',
  daily: 'quota.window.daily',
  weekly: 'quota.window.weekly',
  monthly: 'quota.window.monthly',
  payg: 'quota.label.token_usage',
  pay_as_you_go: 'quota.label.token_usage',
  credits: 'quota.label.credits_remaining',
  overage: 'quota.label.overage_window',
  cycle: 'quota.label.subscription',
};

export function quotaWindowLabel(window: string | undefined, t: TFunction): string {
  if (!window) return '';
  const translationKey = QUOTA_WINDOW_LABEL_KEYS[window];
  if (translationKey) return t(translationKey);
  return window === 'primary' || window === 'secondary' ? t('quota.label.token_usage') : window;
}

export function formatQuotaUsage(usagePercent: number | undefined, t: TFunction): string {
  if (usagePercent === undefined) return t('quota.label.unavailable');
  const percent = usagePercent > 0 && usagePercent < 1 ? '<1' : Math.round(usagePercent);
  return t('quota.label.percent_used', { percent });
}

export function getPeriodQuotaUnavailableReason(limit: ProviderQuotaLimit): string | undefined {
  if (limit.periodQuota != null && limit.periodCost != null) return undefined;
  if (getQuotaWindowUsage(limit) === undefined) return 'quota.label.unavailable';
  if (!limit.nextResetAt || !Number.isFinite(Date.parse(limit.nextResetAt))) return 'quota.label.reset_unavailable';
  if (limit.usageRatio <= 0) return 'quota.label.period_quota_no_usage';
  return 'quota.label.period_quota_no_cost';
}

export function hasPeriodQuotaWindow(limit: ProviderQuotaLimit): boolean {
  if (limit.periodQuota != null) return true;
  return limit.type === 'token' && Boolean(limit.window && /^(\d+[smhd]|daily|weekly|monthly|cycle)$/.test(limit.window));
}

export function getQuotaWindowUsage(limit: ProviderQuotaLimit): number | undefined {
  if (limit.status === 'unknown' || !Number.isFinite(limit.usageRatio)) return undefined;
  return Math.max(0, Math.min(100, limit.usageRatio * 100));
}

export function getQuotaWindowDurationPercent(limit: ProviderQuotaLimit, now = Date.now()): number | undefined {
  if (!limit.periodStart || !limit.nextResetAt) return undefined;
  const start = Date.parse(limit.periodStart);
  const end = Date.parse(limit.nextResetAt);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return undefined;
  return Math.max(0, Math.min(100, ((now - start) / (end - start)) * 100));
}

export function formatQuotaReset(resetAt: string | undefined, t: TFunction, now = Date.now()): string {
  if (!resetAt || !Number.isFinite(Date.parse(resetAt))) return t('quota.label.reset_unavailable');
  const diffMs = Date.parse(resetAt) - now;
  if (diffMs <= 0) return t('quota.label.reset_now');
  const minutes = Math.floor(diffMs / 60000);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const time = days > 0
    ? `${days}${t('quota.label.d')} ${hours % 24}${t('quota.label.h')}`
    : hours > 0
      ? `${hours}${t('quota.label.h')} ${minutes % 60}${t('quota.label.m')}`
      : `${minutes}${t('quota.label.m')}`;
  return t('quota.label.resets_in_time', { time });
}
