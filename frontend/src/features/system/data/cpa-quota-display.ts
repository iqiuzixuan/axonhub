import type { ProviderQuotaLimit } from './quotas';

// CPA chooses between accounts, but all windows must fit on the same account.
// The pool badge shows the least pressured usable account, never an average
// or a combination of windows from different accounts.
export function getCPAPoolUsagePercentage(limits: readonly ProviderQuotaLimit[]): number {
  const accounts = new Map<string, ProviderQuotaLimit[]>();
  for (const limit of limits) {
    if (limit.availabilityGroup !== 'cpa_accounts') continue;
    const key = limit.account ?? '';
    accounts.set(key, [...(accounts.get(key) ?? []), limit]);
  }
  const available = [...accounts.values()].filter((windows) =>
    windows.every((limit) => limit.ready && limit.status !== 'exhausted' && limit.usageRatio < 1)
  );
  return available.length > 0
    ? Math.min(...available.map((windows) => Math.max(...windows.map((limit) => limit.usageRatio * 100))))
    : 100;
}
