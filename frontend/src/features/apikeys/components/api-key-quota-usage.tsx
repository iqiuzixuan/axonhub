import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import { formatNumber } from '@/utils/format-number';
import { useGeneralSettings } from '@/features/system/data/system';
import { getQuotaMetrics, quotaRemainingBarColor, quotaRemainingColor, type ApiKeyQuota, type QuotaMetric } from '../data/quota-display';
import type { ApiKeyProfileQuotaUsage } from '../data/schema';

export function useQuotaDisplay() {
  const { t, i18n } = useTranslation();
  const { data: settings } = useGeneralSettings();
  const locale = i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US';
  const percent = (value: number) =>
    new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value) + '%';
  const value = (key: QuotaMetric['key'], amount: number | null) => {
    if (amount == null) return '—';
    if (key === 'cost') {
      return t('currencies.format', {
        val: amount,
        currency: settings?.currencyCode || 'USD',
        locale,
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      });
    }
    return t(key === 'requests' ? 'apikeys.quota.requestsValue' : 'apikeys.quota.tokensValue', {
      value: formatNumber(amount, { digits: 2, trimTrailingZeros: false }),
    });
  };
  const period = (quota: ApiKeyQuota) => {
    const window = quota.period;
    if (window.type === 'all_time') return t('apikeys.profiles.quotaPeriodAllTime');
    if (window.type === 'calendar_duration') {
      return t(window.calendarDuration?.unit === 'month' ? 'apikeys.quota.calendarMonth' : 'apikeys.quota.calendarDay');
    }
    const unit = window.pastDuration?.unit;
    const unitKey = unit === 'minute' ? 'quotaUnitMinute' : unit === 'hour' ? 'quotaUnitHour' : 'quotaUnitDay';
    return t('apikeys.quota.rollingPeriod', { value: window.pastDuration?.value, unit: t(`apikeys.profiles.${unitKey}`) });
  };
  const date = (time: Date | number) =>
    new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone: settings?.timezone || undefined,
    }).format(time);
  return { t, value, percent, period, date, timezone: settings?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone };
}

export function QuotaRemainingBar({ remaining, label }: { remaining: number | null; label: string }) {
  return (
    <span
      className='bg-muted block h-1.5 min-w-0 flex-1 overflow-hidden rounded-full'
      role='progressbar'
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={remaining ?? undefined}
    >
      {remaining != null && <span className={cn('block h-full', quotaRemainingBarColor(remaining))} style={{ width: `${remaining}%` }} />}
    </span>
  );
}

export function ApiKeyQuotaUsage({
  quota,
  snapshot,
  updatedAt,
}: {
  quota: ApiKeyQuota;
  snapshot?: ApiKeyProfileQuotaUsage;
  updatedAt?: number;
}) {
  const { t, value, percent, period, date, timezone } = useQuotaDisplay();
  const metrics = getQuotaMetrics(quota, snapshot?.usage);

  return (
    <div className='space-y-4' data-testid='api-key-quota-usage'>
      <div className='space-y-4'>
        {metrics.map((metric) => (
          <div key={metric.key} className='space-y-1.5'>
            <div className='flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-sm'>
              <span className='font-medium'>{t(metric.label)}</span>
              <span className='text-muted-foreground tabular-nums'>
                {t('apikeys.quota.usedOfLimit', {
                  used: value(metric.key, metric.used),
                  limit: metric.limit == null ? '∞' : value(metric.key, metric.limit),
                })}
              </span>
            </div>
            {metric.limit != null && (
              <QuotaRemainingBar
                remaining={metric.remainingPercent}
                label={t('apikeys.quota.metricRemaining', { metric: t(metric.label) })}
              />
            )}
            <div className='flex flex-wrap items-center justify-between gap-2 text-xs tabular-nums'>
              <span className='text-muted-foreground'>
                {t('apikeys.quota.remainingValue', { value: metric.limit == null ? '∞' : value(metric.key, metric.remaining) })}
              </span>
              <span className={quotaRemainingColor(metric.remainingPercent)}>
                {metric.remainingPercent != null && percent(metric.remainingPercent)}
                {metric.exhausted && ` · ${t('apikeys.quota.exhausted')}`}
              </span>
            </div>
          </div>
        ))}
      </div>
      <div className='text-muted-foreground space-y-1 border-t pt-3 text-xs'>
        <div className='text-foreground font-medium'>{period(quota)}</div>
        {snapshot?.window && (
          <div>
            {snapshot.window.start ? date(snapshot.window.start) : t('apikeys.profiles.quotaPeriodAllTime')} →{' '}
            {snapshot.window.end ? date(snapshot.window.end) : '—'} ({timezone})
          </div>
        )}
        <div>
          {t(
            quota.period.type === 'past_duration'
              ? 'apikeys.quota.rollingHint'
              : quota.period.type === 'all_time'
                ? 'apikeys.quota.allTimeHint'
                : 'apikeys.quota.calendarHint'
          )}
        </div>
        {quota.period.type === 'calendar_duration' && snapshot?.window.end && (
          <div>{t('apikeys.quota.resetsAt', { time: date(snapshot.window.end) })}</div>
        )}
        {updatedAt != null && updatedAt > 0 && <div>{t('apikeys.quota.updatedAt', { time: date(updatedAt) })}</div>}
      </div>
    </div>
  );
}
