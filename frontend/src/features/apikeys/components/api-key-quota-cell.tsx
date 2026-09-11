import { useState } from 'react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useApiKeyQuotaUsages } from '../data/apikeys';
import { getActiveQuotaProfile, getQuotaMetrics, isSameQuotaPeriod, quotaRemainingColor } from '../data/quota-display';
import type { ApiKey } from '../data/schema';
import { ApiKeyQuotaUsage, QuotaRemainingBar, useQuotaDisplay } from './api-key-quota-usage';

export function ApiKeyQuotaCell({ apiKey }: { apiKey: ApiKey }) {
  const [open, setOpen] = useState(false);
  const { t, percent, period } = useQuotaDisplay();
  const profile = getActiveQuotaProfile(apiKey);
  const quota = profile?.quota;
  const query = useApiKeyQuotaUsages(apiKey.id, { enabled: quota != null, refetchInterval: open ? 10000 : 30000, silent: true });
  const saved = query.data?.find((usage) => usage.profileName === profile?.name);
  const snapshot = !query.isError && quota && saved?.quota && isSameQuotaPeriod(quota.period, saved.quota.period) ? saved : undefined;

  if (!quota) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className='text-muted-foreground inline-block px-2' aria-label={t('apikeys.quota.unlimited')}>
            ∞
          </span>
        </TooltipTrigger>
        <TooltipContent>{t('apikeys.quota.unlimited')}</TooltipContent>
      </Tooltip>
    );
  }

  const metrics = getQuotaMetrics(quota, snapshot?.usage).filter((metric) => metric.limit != null);
  const unavailable = !query.isLoading && !snapshot;
  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) void query.refetch();
      }}
    >
      <DialogTrigger asChild>
        <Button
          variant='ghost'
          className='h-auto w-64 flex-col items-stretch gap-1.5 px-2 py-1.5'
          aria-label={t('apikeys.quota.view', { name: apiKey.name })}
        >
          {metrics.map((metric) => (
            <span key={metric.key} className='flex min-w-0 items-center gap-1 text-[11px]'>
              <span className='text-muted-foreground w-16 shrink-0 truncate text-left'>{t(metric.label)}</span>
              <QuotaRemainingBar
                remaining={metric.remainingPercent}
                label={t('apikeys.quota.metricRemaining', { metric: t(metric.label) })}
              />
              <span className={cn('w-14 shrink-0 text-right font-medium tabular-nums', quotaRemainingColor(metric.remainingPercent))}>
                {metric.remainingPercent == null ? '—' : percent(metric.remainingPercent)}
              </span>
            </span>
          ))}
          <span className='text-muted-foreground text-left text-[11px] font-normal whitespace-normal'>
            {query.isLoading ? t('apikeys.quota.loading') : unavailable ? t('apikeys.quota.unavailable') : period(quota)}
            {snapshot && metrics.some((metric) => metric.exhausted) && ` · ${t('apikeys.quota.exhausted')}`}
          </span>
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('apikeys.quota.title')}</DialogTitle>
          <DialogDescription>{t('apikeys.quota.description', { name: apiKey.name, profile: profile?.name })}</DialogDescription>
        </DialogHeader>
        {unavailable && (
          <p className='text-destructive text-sm' role='status'>
            {t('apikeys.quota.unavailableHint')}
          </p>
        )}
        {query.isLoading && (
          <p className='text-muted-foreground text-sm' role='status'>
            {t('apikeys.quota.loading')}
          </p>
        )}
        <ApiKeyQuotaUsage quota={quota} snapshot={snapshot} updatedAt={snapshot ? query.dataUpdatedAt : undefined} />
        <DialogFooter>
          <Button variant='outline' onClick={() => void query.refetch()} disabled={query.isFetching}>
            {t('apikeys.quota.refresh')}
          </Button>
          <Button onClick={() => setOpen(false)}>{t('common.buttons.close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
