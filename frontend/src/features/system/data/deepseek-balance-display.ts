export type DeepSeekBalanceDisplay = {
  currency: 'CNY' | 'USD';
  totalBalance: number;
  remainingPercent: number;
};

// The backend persists the visual reference so reloads and different clients
// show the same bar. Failed reads must not expose the previous balance.
export function parseDeepSeekBalances(quotaData: unknown, status: string): DeepSeekBalanceDisplay[] {
  if (status === 'unknown' || typeof quotaData !== 'object' || quotaData === null) return [];
  const data = quotaData as Record<string, unknown>;
  if (data.error_code || !Array.isArray(data.balance_infos)) return [];

  const result: DeepSeekBalanceDisplay[] = [];
  const seen = new Set<string>();
  for (const entry of data.balance_infos) {
    if (typeof entry !== 'object' || entry === null) return [];
    const balance = entry as Record<string, unknown>;
    if (balance.currency !== 'CNY' && balance.currency !== 'USD') return [];
    if (seen.has(balance.currency)) return [];
    seen.add(balance.currency);
    const amounts = [balance.total_balance, balance.baseline_balance];
    if (amounts.some((value) => typeof value !== 'string' || value.length > 64 || !/^\d+(\.\d+)?$/.test(value))) return [];
    const total = Number(balance.total_balance);
    const baseline = Number(balance.baseline_balance);
    if (!Number.isFinite(total) || !Number.isFinite(baseline) || total < 0 || baseline < total) return [];
    result.push({
      currency: balance.currency,
      totalBalance: total,
      remainingPercent: baseline > 0 ? Math.max(0, Math.min(100, (total / baseline) * 100)) : 0,
    });
  }
  return result;
}
