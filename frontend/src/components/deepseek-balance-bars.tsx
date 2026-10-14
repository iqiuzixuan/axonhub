import { useTranslation } from 'react-i18next';
import { parseDeepSeekBalances } from '@/features/system/data/deepseek-balance-display';

export function DeepSeekBalanceBars({ quotaData, status }: { quotaData: unknown; status: string }) {
  const { t, i18n } = useTranslation();
  const balances = parseDeepSeekBalances(quotaData, status);
  if (balances.length === 0) {
    return <div className='text-muted-foreground text-center text-xs'>{t('quota.label.unavailable')}</div>;
  }

  return (
    <div className='flex min-w-0 flex-col items-stretch gap-1.5 text-[11px]'>
      {balances.map((balance) => {
        const remaining = balance.remainingPercent;
        const amount = t('currencies.format', {
          val: balance.totalBalance,
          currency: balance.currency,
          locale: i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
          minimumFractionDigits: 2,
          maximumFractionDigits: 6,
        });
        const color = remaining <= 20 ? 'text-red-500' : remaining <= 50 ? 'text-yellow-500' : 'text-green-600 dark:text-green-500';
        return (
          <div key={balance.currency} className='flex min-w-0 items-center gap-1'>
            <span className='text-muted-foreground w-20 shrink-0 truncate text-left'>{t('quota.label.account_balance')}</span>
            <div
              className='bg-muted h-1.5 min-w-0 flex-1 overflow-hidden rounded-full'
              role='progressbar'
              aria-label={t('quota.label.account_balance')}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={remaining}
              aria-valuetext={amount}
            >
              <div
                className={`h-full ${remaining <= 20 ? 'bg-red-500' : remaining <= 50 ? 'bg-yellow-500' : 'bg-green-500'}`}
                style={{ width: `${remaining}%` }}
              />
            </div>
            <span className={`w-20 shrink-0 text-right font-medium tabular-nums ${color}`}>{amount}</span>
          </div>
        );
      })}
    </div>
  );
}
