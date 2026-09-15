import { formatModelLabel } from '@/utils/model-label';
import { useState, type ReactNode } from 'react';
import { Link } from '@tanstack/react-router';
import { Activity, BarChart4, Bot, CalendarDays, CheckCircle2, Database, Download, KeyRound, ShieldCheck, Wallet } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Area, AreaChart, CartesianGrid, Cell, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { toast } from 'sonner';
import { usePersonalScope } from '@/stores/personalWorkspaceStore';
import { formatNumber } from '@/utils/format-number';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { getQuotaMetrics } from '@/features/apikeys/data/quota-display';
import { PersonalKeyFilter, PersonalLoadError } from './components';
import { usePersonalDashboard, usePersonalWorkspace, type PersonalQuota } from './data';
import { getPersonalDateRange, isPersonalDateRangeValid, personalUsageCSV, type UsagePeriod } from './utils';

function MetricCard({
  title,
  icon: Icon,
  value,
  children,
}: {
  title: string;
  icon: typeof Activity;
  value?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card className='hover-card min-w-0'>
      <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-2'>
        <div className='flex items-center gap-2'>
          <div className='bg-primary/10 text-primary rounded-lg p-1.5'>
            <Icon className='size-4' />
          </div>
          <CardTitle className='text-sm font-medium'>{title}</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {value !== undefined && <div className='font-mono text-2xl font-bold'>{value}</div>}
        {children}
      </CardContent>
    </Card>
  );
}

function QuotaSummary({ quota, currency }: { quota: PersonalQuota | null; currency: (amount: number) => string }) {
  const { t } = useTranslation();
  if (!quota) return <span className='text-muted-foreground text-xs'>{t('personal.noQuota')}</span>;
  const metrics = getQuotaMetrics(quota.limit, { requestCount: quota.requests, totalTokens: quota.tokens, totalCost: quota.cost }).filter(
    (metric) => metric.limit !== null
  );
  const period = quota.limit.period;
  const periodLabel =
    period.type === 'all_time'
      ? t('apikeys.profiles.quotaPeriodAllTime')
      : period.type === 'calendar_duration'
        ? t(
            period.calendarDuration?.unit === 'week'
              ? 'apikeys.quota.calendarWeek'
              : period.calendarDuration?.unit === 'month'
                ? 'apikeys.quota.calendarMonth'
                : 'apikeys.quota.calendarDay'
          )
        : t('apikeys.quota.rollingPeriod', {
            value: period.pastDuration?.value,
            unit: t(
              `apikeys.profiles.${period.pastDuration?.unit === 'hour' ? 'quotaUnitHour' : period.pastDuration?.unit === 'minute' ? 'quotaUnitMinute' : 'quotaUnitDay'}`
            ),
          });
  return (
    <div className='min-w-44 space-y-2'>
      {metrics.map((metric) => (
        <div key={metric.key}>
          <div className='mb-1 flex justify-between gap-3 text-xs'>
            <span>{t(metric.label)}</span>
            <span className='font-mono'>
              {metric.key === 'cost' ? currency(metric.used ?? 0) : formatNumber(metric.used ?? 0)} /{' '}
              {metric.key === 'cost' ? currency(metric.limit!) : formatNumber(metric.limit!)}
            </span>
          </div>
          <Progress value={100 - (metric.remainingPercent ?? 100)} className='h-1.5' />
        </div>
      ))}
      <p className='text-muted-foreground text-xs'>{periodLabel}</p>
    </div>
  );
}

export default function PersonalDashboardPage() {
  const { t, i18n } = useTranslation();
  const { setAPIKeyId } = usePersonalScope();
  const workspace = usePersonalWorkspace();
  const [period, setPeriod] = useState<UsagePeriod>('30');
  const [metric, setMetric] = useState('tokens');
  const [custom, setCustom] = useState({ start: '', end: '' });
  const [draft, setDraft] = useState({ start: '', end: '' });
  const [dateOpen, setDateOpen] = useState(false);
  const [invalidDates, setInvalidDates] = useState(false);
  const today = workspace.data?.today ?? '1970-01-01';
  const range = getPersonalDateRange(today, period, custom);
  const dashboard = usePersonalDashboard(range.start, range.end, !!workspace.data && isPersonalDateRangeValid(range.start, range.end));
  const data = dashboard.data;
  const currency = (amount: number) =>
    t('currencies.format', {
      val: amount,
      currency: workspace.data?.currencyCode ?? 'USD',
      locale: i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    });
  const summary = data?.overview;
  const trend = data?.daily.map((row) => ({ date: row.date.slice(5), ...row.metrics })) ?? [];
  const distribution = data?.models.filter((row) => row.metrics.totalTokens > 0) ?? [];
  const colors = Array.from({ length: 6 }, (_, index) => `var(--chart-${index + 1})`);

  const exportData = () => {
    if (!data) return;
    const labels = [
      'date',
      'requests',
      'successes',
      'failures',
      'canceled',
      'pending',
      'input',
      'output',
      'cached',
      'totalTokens',
      'cost',
      'unpriced',
    ];
    const rows: (string | number)[][] = [labels.map((label) => t(`personal.${label}`))];
    data.daily.forEach(({ date, metrics: m }) =>
      rows.push([
        date,
        m.requests,
        m.successfulRequests,
        m.failedRequests,
        m.canceledRequests,
        m.pendingRequests,
        m.inputTokens,
        m.outputTokens,
        m.cachedTokens,
        m.totalTokens,
        m.cost,
        m.unpricedUsageCount,
      ])
    );
    const url = URL.createObjectURL(new Blob([personalUsageCSV(rows)], { type: 'text/csv;charset=utf-8' }));
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `axonhub-usage-${range.start}-${range.end}.csv`;
    anchor.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    toast.success(t('personal.exported'));
  };

  return (
    <div className='flex-1 space-y-6 p-4 pt-6 md:p-8 md:pt-6'>
      <div className='flex flex-wrap items-start justify-between gap-4'>
        <div>
          <h1 className='text-2xl font-bold tracking-tight'>{t('personal.dashboard')}</h1>
          <p className='text-muted-foreground mt-1 text-sm'>{t('personal.dashboardDescription')}</p>
        </div>
        <div className='flex gap-2'>
          <Button variant='outline' disabled={!data} onClick={exportData}>
            <Download className='size-4' />
            {t('personal.export')}
          </Button>
          <Button asChild>
            <Link to='/me/models'>
              <Bot className='size-4' />
              {t('sidebar.items.models')}
            </Link>
          </Button>
        </div>
      </div>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex items-center gap-2'>
          <PersonalKeyFilter />
          <Badge variant='secondary'>{t('personal.onlyMine')}</Badge>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Tabs value={period} onValueChange={(value) => setPeriod(value as UsagePeriod)}>
            <TabsList>
              <TabsTrigger value='7'>{t('personal.last7Days')}</TabsTrigger>
              <TabsTrigger value='30'>{t('personal.last30Days')}</TabsTrigger>
              <TabsTrigger value='month'>{t('personal.thisMonth')}</TabsTrigger>
            </TabsList>
          </Tabs>
          <Button
            variant='outline'
            disabled={!workspace.data}
            onClick={() => {
              setDraft(range);
              setInvalidDates(false);
              setDateOpen(true);
            }}
          >
            <CalendarDays className='size-4' />
            {workspace.data ? `${range.start} — ${range.end}` : t('personal.dateRange')}
          </Button>
        </div>
      </div>
      {workspace.isError || dashboard.isError ? (
        <PersonalLoadError
          retry={() => {
            if (workspace.isError) void workspace.refetch();
            else void dashboard.refetch();
          }}
        />
      ) : !data ? (
        <div className='grid gap-6 md:grid-cols-2 xl:grid-cols-4'>
          {[0, 1, 2, 3].map((id) => (
            <Skeleton key={id} className='h-40' />
          ))}
          <Skeleton className='col-span-full h-80' />
        </div>
      ) : (
        <>
          <div className='grid gap-6 sm:grid-cols-2 xl:grid-cols-4'>
            <MetricCard title={t('personal.requests')} icon={Database} value={formatNumber(summary!.requests)}>
              <p className='text-muted-foreground mt-2 text-xs'>
                {t('personal.requestBreakdown', {
                  success: formatNumber(summary!.successfulRequests),
                  failed: formatNumber(summary!.failedRequests),
                })}
              </p>
              {(summary!.canceledRequests > 0 || summary!.pendingRequests > 0) && (
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('personal.unfinishedBreakdown', {
                    canceled: formatNumber(summary!.canceledRequests),
                    pending: formatNumber(summary!.pendingRequests),
                  })}
                </p>
              )}
            </MetricCard>
            <MetricCard
              title={t('personal.successRate')}
              icon={CheckCircle2}
              value={summary!.successRate == null ? '—' : `${summary!.successRate.toFixed(1)}%`}
            >
              <p className='text-muted-foreground mt-2 text-xs'>{t('personal.successRateDescription')}</p>
            </MetricCard>
            <MetricCard title={t('personal.tokens')} icon={BarChart4}>
              <div className='flex items-center justify-between gap-3'>
                {(['input', 'output', 'cached'] as const).map((name) => (
                  <div key={name} className='flex-1 border-r pr-2 text-center last:border-0 last:pr-0'>
                    <div className='text-muted-foreground mb-1 text-xs'>{t(`personal.${name}`)}</div>
                    <div className='font-mono text-lg font-bold'>{formatNumber(summary![`${name}Tokens`])}</div>
                  </div>
                ))}
              </div>
            </MetricCard>
            <MetricCard title={t('personal.cost')} icon={Wallet} value={currency(summary!.cost)}>
              <p className='text-muted-foreground mt-2 text-xs'>
                {summary!.unpricedUsageCount
                  ? t('personal.unpricedHint', { count: summary!.unpricedUsageCount })
                  : t('personal.costDescription')}
              </p>
            </MetricCard>
          </div>
          <div className='grid gap-4 lg:grid-cols-7'>
            <Card className='hover-card lg:col-span-4'>
              <CardHeader>
                <CardTitle>{t('personal.trend')}</CardTitle>
                <CardDescription>
                  {range.start} — {range.end} · {data.timezone}
                </CardDescription>
                <CardAction>
                  <Tabs value={metric} onValueChange={setMetric}>
                    <TabsList className='h-8'>
                      <TabsTrigger value='tokens'>Token</TabsTrigger>
                      <TabsTrigger value='requests'>{t('personal.requests')}</TabsTrigger>
                      <TabsTrigger value='cost'>{t('personal.cost')}</TabsTrigger>
                    </TabsList>
                  </Tabs>
                </CardAction>
              </CardHeader>
              <CardContent className='pl-2'>
                <div className='h-[290px]'>
                  <ResponsiveContainer width='100%' height='100%'>
                    <AreaChart data={trend} margin={{ top: 10, right: 20, bottom: 0, left: 5 }}>
                      <CartesianGrid strokeDasharray='3 3' vertical={false} stroke='var(--border)' />
                      <XAxis
                        dataKey='date'
                        tickLine={false}
                        axisLine={false}
                        minTickGap={35}
                        tick={{ fontSize: 12, fill: 'var(--muted-foreground)' }}
                      />
                      <YAxis
                        tickFormatter={(value: number) => (metric === 'cost' ? currency(value) : formatNumber(value))}
                        tickLine={false}
                        axisLine={false}
                        width={70}
                        tick={{ fontSize: 12, fill: 'var(--muted-foreground)' }}
                      />
                      <Tooltip
                        contentStyle={{ background: 'var(--popover)', borderColor: 'var(--border)', borderRadius: 'var(--radius)' }}
                        formatter={(value) => (metric === 'cost' ? currency(Number(value)) : formatNumber(Number(value)))}
                      />
                      <Area
                        isAnimationActive={false}
                        type='monotone'
                        name={t(metric === 'tokens' ? 'personal.input' : `personal.${metric}`)}
                        dataKey={metric === 'tokens' ? 'inputTokens' : metric}
                        stroke='var(--chart-1)'
                        fill='var(--chart-1)'
                        fillOpacity={0.08}
                        strokeWidth={2}
                      />
                      {metric === 'tokens' && (
                        <Area
                          isAnimationActive={false}
                          type='monotone'
                          name={t('personal.output')}
                          dataKey='outputTokens'
                          stroke='var(--chart-2)'
                          fill='var(--chart-2)'
                          fillOpacity={0.04}
                          strokeWidth={2}
                        />
                      )}
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
                <p className='text-muted-foreground mt-3 text-center text-xs'>{t('personal.trendDescription')}</p>
              </CardContent>
            </Card>
            <Card className='hover-card lg:col-span-3'>
              <CardHeader>
                <CardTitle>{t('personal.modelDistribution')}</CardTitle>
                <CardDescription>{t('personal.modelDistributionDescription')}</CardDescription>
              </CardHeader>
              <CardContent>
                {distribution.length ? (
                  <>
                    <div className='relative h-[190px]'>
                      <ResponsiveContainer>
                        <PieChart>
                          <Pie
                            isAnimationActive={false}
                            data={distribution.map((row) => ({ name: formatModelLabel(row.modelId, t), value: row.metrics.totalTokens }))}
                            dataKey='value'
                            innerRadius={58}
                            outerRadius={82}
                            paddingAngle={3}
                            stroke='none'
                          >
                            {distribution.map((row, index) => (
                              <Cell key={row.modelId} fill={colors[index % colors.length]} />
                            ))}
                          </Pie>
                          <Tooltip
                            contentStyle={{ background: 'var(--popover)', borderColor: 'var(--border)' }}
                            formatter={(value) => formatNumber(Number(value))}
                          />
                        </PieChart>
                      </ResponsiveContainer>
                      <div className='pointer-events-none absolute inset-0 flex flex-col items-center justify-center'>
                        <strong className='font-mono text-2xl'>{distribution.length}</strong>
                        <span className='text-muted-foreground text-xs'>{t('personal.modelsUsed')}</span>
                      </div>
                    </div>
                    <div className='mt-2 max-h-36 space-y-2 overflow-auto'>
                      {distribution.map((row, index) => (
                        <Link
                          to='/me/models'
                          search={{ q: row.modelId }}
                          className='hover:text-primary flex min-w-0 items-center gap-2 text-xs'
                          key={row.modelId}
                        >
                          <i className='size-2 shrink-0 rounded-full' style={{ background: colors[index % colors.length] }} />
                          <span className='truncate'>{formatModelLabel(row.modelId, t)}</span>
                          <span className='text-muted-foreground ml-auto'>
                            {((row.metrics.totalTokens / summary!.totalTokens) * 100).toFixed(1)}%
                          </span>
                        </Link>
                      ))}
                    </div>
                  </>
                ) : (
                  <p className='text-muted-foreground flex h-[290px] items-center justify-center text-sm'>{t('personal.noUsage')}</p>
                )}
              </CardContent>
            </Card>
          </div>
          <div className='bg-card flex items-center gap-3 rounded-lg border p-4'>
            <div className='bg-primary/10 text-primary flex size-8 items-center justify-center rounded-md'>
              <KeyRound className='size-5' />
            </div>
            <span className='text-lg font-semibold'>{t('personal.personalKeys')}</span>
          </div>
          <Card>
            <CardHeader>
              <CardTitle>{t('personal.keyUsage')}</CardTitle>
              <CardDescription>{t('personal.quotaDescription')}</CardDescription>
            </CardHeader>
            <CardContent className='overflow-x-auto'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {['key', 'project', 'requests', 'tokens', 'cost', 'quota'].map((name) => (
                      <TableHead key={name}>{t(`personal.${name}`)}</TableHead>
                    ))}
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.apiKeys.map(({ apiKey, metrics, quota }) => (
                    <TableRow key={apiKey.id}>
                      <TableCell className='py-4'>
                        <div className='font-medium'>{apiKey.name}</div>
                        {(apiKey.deleted || apiKey.status !== 'enabled') && (
                          <Badge variant='outline' className='mt-1 text-xs'>
                            {t(apiKey.deleted ? 'personal.deleted' : 'personal.disabled')}
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>{apiKey.projectName}</TableCell>
                      <TableCell className='font-mono'>{formatNumber(metrics.requests)}</TableCell>
                      <TableCell className='font-mono'>{formatNumber(metrics.totalTokens)}</TableCell>
                      <TableCell className='font-mono'>
                        {currency(metrics.cost)}
                        {metrics.unpricedUsageCount > 0 && (
                          <p className='text-muted-foreground mt-1 text-xs'>
                            {t('personal.unpricedHint', { count: metrics.unpricedUsageCount })}
                          </p>
                        )}
                      </TableCell>
                      <TableCell>
                        <QuotaSummary quota={quota} currency={currency} />
                      </TableCell>
                      <TableCell>
                        {!apiKey.deleted && apiKey.status === 'enabled' && (
                          <Button asChild variant='ghost' size='sm'>
                            <Link to='/me/models' onClick={() => setAPIKeyId(apiKey.id)}>
                              {t('sidebar.items.models')}
                            </Link>
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                  {!data.apiKeys.length && (
                    <TableRow>
                      <TableCell colSpan={7} className='text-muted-foreground h-32 text-center'>
                        {t('personal.noKeys')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
          <p className='text-muted-foreground flex items-start gap-2 text-xs'>
            <ShieldCheck className='size-4 shrink-0' />
            {t('personal.usageScope')}
          </p>
        </>
      )}
      <Dialog open={dateOpen} onOpenChange={setDateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('personal.dateRange')}</DialogTitle>
            <DialogDescription>{t('personal.dateRangeDescription')}</DialogDescription>
          </DialogHeader>
          <form
            className='space-y-4'
            onSubmit={(event) => {
              event.preventDefault();
              if (!isPersonalDateRangeValid(draft.start, draft.end)) {
                setInvalidDates(true);
                return;
              }
              setCustom(draft);
              setPeriod('custom');
              setDateOpen(false);
            }}
          >
            <div className='space-y-2'>
              <Label htmlFor='personal-start'>{t('personal.startDate')}</Label>
              <Input
                id='personal-start'
                type='date'
                value={draft.start}
                onChange={(event) => setDraft({ ...draft, start: event.target.value })}
                required
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='personal-end'>{t('personal.endDate')}</Label>
              <Input
                id='personal-end'
                type='date'
                value={draft.end}
                onChange={(event) => setDraft({ ...draft, end: event.target.value })}
                required
              />
            </div>
            {invalidDates && (
              <p role='alert' className='text-destructive text-sm'>
                {t('personal.invalidDates')}
              </p>
            )}
            <Button type='submit' className='w-full'>
              {t('personal.apply')}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
