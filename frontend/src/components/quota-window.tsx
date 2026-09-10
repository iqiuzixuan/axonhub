import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import type { ProviderQuotaLimit } from '@/features/system/data/quotas';
import { useGeneralSettings } from '@/features/system/data/system';
import {
  getQuotaWindowUsage,
  getQuotaWindowDurationPercent,
  formatQuotaReset,
  formatQuotaUsage,
  quotaWindowLabel,
  getPeriodQuotaUnavailableReason,
  hasPeriodQuotaWindow,
} from '@/features/system/data/quota-window-display';

export { QUOTA_WINDOW_LABEL_KEYS } from '@/features/system/data/quota-window-display';

// UsageTimeBar shows usage on a single progress bar with a small triangle below
// it marking how far the reset window has elapsed (time progress). Hovering
// reveals the detailed figures via tooltip, keeping the row compact.
export function UsageTimeBar({ usagePercent, durationPercent, tooltip }: { usagePercent: number; durationPercent?: number; tooltip: ReactNode }) {
  const clamped = Math.min(Math.max(usagePercent || 0, 0), 100);
  const markerLeft = durationPercent === undefined ? undefined : Math.min(Math.max(durationPercent, 0), 100);
  const u = clamped / 100;
  let severity = u;
  if (durationPercent !== undefined && durationPercent > 0) {
    const d = Math.max(durationPercent / 100, 0.01);
    severity = u * (u / d);
  }
  severity = Math.min(1, Math.max(0, severity));

  // Tailwind 500 colors approximation for a modern, theme-friendly gradient:
  // Green (142, 71%, 45%), Yellow (45, 93%, 47%), Red (0, 84%, 60%)
  let h: number;
  let s: number;
  let l: number;
  if (severity < 0.5) {
    const n = severity * 2; // 0 to 1
    h = 142 - n * (142 - 45);
    s = 71 + n * (93 - 71);
    l = 45 + n * (47 - 45);
  } else {
    const n = (severity - 0.5) * 2; // 0 to 1
    h = 45 - n * 45;
    s = 93 - n * (93 - 84);
    l = 47 + n * (60 - 47);
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className='relative cursor-default pb-1.5' tabIndex={0}>
          <div className='bg-muted/60 h-1.5 w-full overflow-hidden rounded-full'>
            <div
              className='h-full transition-all duration-500'
              style={{ width: `${clamped}%`, backgroundColor: `hsl(${Math.round(h)}, ${Math.round(s)}%, ${Math.round(l)}%)` }}
            />
          </div>
          {markerLeft !== undefined && (
            <div className='absolute top-2 -translate-x-1/2' style={{ left: `${markerLeft}%` }} aria-hidden>
              {/* upward triangle pointing at the bar, marking elapsed time */}
              <div className='border-b-muted-foreground h-0 w-0 border-x-[3px] border-b-[4px] border-x-transparent' />
            </div>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent side='top'>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

// PeriodQuotaEstimate prices each limit window: the backend sums what the
// channel cost during the window from AxonHub usage logs and divides by the
// usage ratio the provider reported, which yields what the whole window is
// worth. Missing estimates explain which input is unavailable.
export function PeriodQuotaEstimate({ limits, showUnavailable = false }: { limits: ProviderQuotaLimit[]; showUnavailable?: boolean }) {
  const { t, i18n } = useTranslation();
  const { data: generalSettings } = useGeneralSettings();

  const windows = limits.filter((limit) => limit.periodQuota != null || (showUnavailable && hasPeriodQuotaWindow(limit)));
  if (windows.length === 0) return null;

  const formatCurrency = (val: number) =>
    t('currencies.format', {
      val,
      currency: generalSettings?.currencyCode || 'USD',
      locale: i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    });

  return (
    <div className='border-border/60 mt-3 space-y-2 border-t border-dashed pt-3'>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className='text-muted-foreground cursor-default text-xs font-medium'>{t('quota.label.period_quota')}</span>
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-[260px]'>
          {t('quota.label.period_quota_hint')}
        </TooltipContent>
      </Tooltip>

      {windows.map((limit, index) => {
        const label = quotaWindowLabel(limit.window, t) || t('quota.label.token_usage');
        const unavailableReason = getPeriodQuotaUnavailableReason(limit);

        return (
          <div key={`${limit.window ?? limit.type}-${index}`} className='flex items-center justify-between gap-3 text-xs'>
            <span className='text-muted-foreground'>{label}</span>
            <span className='text-foreground text-right font-medium'>
              {unavailableReason
                ? t(unavailableReason)
                : t('quota.label.period_quota_value', {
                    used: formatCurrency(limit.periodCost as number),
                    total: formatCurrency(limit.periodQuota as number),
                  })}
            </span>
          </div>
        );
      })}
    </div>
  );
}

export function QuotaWindow({ limit }: { limit: ProviderQuotaLimit }) {
  const { t } = useTranslation();
  const label = quotaWindowLabel(limit.window, t) || t('quota.label.quota');
  const usagePercent = getQuotaWindowUsage(limit);
  const usageText = formatQuotaUsage(usagePercent, t);
  const durationPercent = getQuotaWindowDurationPercent(limit);
  const resetText = formatQuotaReset(limit.nextResetAt, t);
  const tooltip = (
    <div className='space-y-0.5'>
      <div className='font-medium'>{label}</div>
      <div>{usageText}</div>
      {durationPercent !== undefined && <div>{t('quota.label.time_elapsed')}: {Math.round(durationPercent)}%</div>}
      <div>{resetText}</div>
    </div>
  );

  const bar =
    usagePercent === undefined ? (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className='bg-muted/60 h-1.5 w-full rounded-full' tabIndex={0} />
        </TooltipTrigger>
        <TooltipContent side='top'>{tooltip}</TooltipContent>
      </Tooltip>
    ) : (
      <UsageTimeBar usagePercent={usagePercent} durationPercent={durationPercent} tooltip={tooltip} />
    );

  return (
    <div className='space-y-1.5'>
      <div className='flex items-center justify-between text-xs'>
        <span className='text-muted-foreground font-medium'>{label}</span>
        <span className='text-foreground font-medium'>{usageText}</span>
      </div>
      {bar}
    </div>
  );
}

export function QuotaWindows({ limits }: { limits: ProviderQuotaLimit[] }) {
  const { t } = useTranslation();
  return (
    <div className='mt-3 space-y-3'>
      {limits.length === 0 && <div className='bg-muted/40 text-muted-foreground rounded p-2 text-[11px]'>{t('quota.label.unavailable')}</div>}
      {limits.map((limit, index) => (
        <div key={`${limit.window}-${index}`} className={index > 0 ? 'border-border/60 border-t border-dashed pt-3' : undefined}>
          <QuotaWindow limit={limit} />
        </div>
      ))}
    </div>
  );
}
