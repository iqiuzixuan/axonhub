import type { AnalyticsDimensionStat } from '../data/analytics';

export function buildDimensionChartData(
  data: AnalyticsDimensionStat[],
  valueKey: 'totalTokens' | 'requestCount' | 'cost',
  otherLabel: string
) {
  const total = data.reduce((sum, item) => sum + item[valueKey], 0);
  const sorted = [...data].sort((a, b) => b[valueKey] - a[valueKey]);
  const chartData: Array<{ id: string; name: string; apiKeyUserName?: string | null; value: number; percentage: number }> = [];
  let otherValue = 0;

  for (const item of sorted) {
    const value = item[valueKey];
    const percentage = total > 0 ? (value / total) * 100 : 0;
    if (chartData.length < 9 && percentage >= 2) {
      chartData.push({ id: `item:${item.id}`, name: item.name, apiKeyUserName: item.apiKeyUserName, value, percentage });
    } else {
      otherValue += value;
    }
  }

  if (otherValue > 0) {
    chartData.push({ id: 'other', name: otherLabel, value: otherValue, percentage: (otherValue / total) * 100 });
  }

  return chartData.sort((a, b) => b.percentage - a.percentage);
}
