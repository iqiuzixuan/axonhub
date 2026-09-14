import React from 'react';
import { z } from 'zod';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const zh = JSON.parse(readFileSync(new URL('../../../locales/zh-CN/apikeys.json', import.meta.url), 'utf8'));
const en = JSON.parse(readFileSync(new URL('../../../locales/en/apikeys.json', import.meta.url), 'utf8'));
const t = (key, values = {}) =>
  key === 'currencies.format'
    ? new Intl.NumberFormat(values.locale, {
        style: 'currency',
        currency: values.currency,
        minimumFractionDigits: values.minimumFractionDigits,
        maximumFractionDigits: values.maximumFractionDigits,
      }).format(values.val)
    : (zh[key] ?? key).replace(/\{\{(\w+)\}\}/g, (_, key) => String(values[key] ?? ''));
function load(path, dependencies = {}) {
  const file = new URL(path, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(file, 'utf8'), {
    fileName: file.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.ReactJSX },
  });
  const exports = {};
  new Function('require', 'exports', outputText)((name) => dependencies[name] ?? require(name), exports);
  return exports;
}
const helpers = load('../data/quota-display.ts');
const numbers = load('../../../utils/format-number.ts');
const quota = {
  requests: 1000,
  totalTokens: 2000000,
  cost: 10,
  period: { type: 'past_duration', pastDuration: { value: 1, unit: 'day' } },
};
const snapshot = {
  profileName: 'production',
  quota,
  window: { start: new Date('2026-09-10T07:00:00Z'), end: new Date('2026-09-11T07:00:00Z') },
  usage: { requestCount: 140, totalTokens: 450000, totalCost: 8.2 },
};
const apiKey = {
  id: 'key-1',
  name: 'app-prod',
  type: 'user',
  profiles: { activeProfile: 'production', profiles: [{ name: 'production', quota }] },
};
let query = { data: [snapshot], isLoading: false, isError: false, isFetching: false, dataUpdatedAt: 1, refetch() {} };
let queryOptions;
const usage = load('./api-key-quota-usage.tsx', {
  'react-i18next': { useTranslation: () => ({ t, i18n: { language: 'zh-CN' } }) },
  '@/lib/utils': { cn: (...parts) => parts.filter(Boolean).join(' ') },
  '@/utils/format-number': numbers,
  '@/features/system/data/system': { useGeneralSettings: () => ({ data: { currencyCode: 'CNY', timezone: 'Asia/Shanghai' } }) },
  '../data/quota-display': helpers,
});
const pass = ({ children }) => React.createElement(React.Fragment, null, children);
const cell = load('./api-key-quota-cell.tsx', {
  '@/components/ui/button': { Button: ({ variant: _, ...props }) => React.createElement('button', props) },
  '@/components/ui/dialog': {
    Dialog: pass,
    DialogTrigger: pass,
    DialogContent: () => null,
    DialogHeader: pass,
    DialogTitle: pass,
    DialogDescription: pass,
    DialogFooter: pass,
  },
  '@/components/ui/tooltip': { Tooltip: pass, TooltipTrigger: pass, TooltipContent: () => null },
  '@/lib/utils': { cn: (...parts) => parts.filter(Boolean).join(' ') },
  '../data/quota-display': helpers,
  './api-key-quota-usage': usage,
  '../data/apikeys': {
    useApiKeyQuotaUsages: (_, options) => {
      queryOptions = options;
      return query;
    },
  },
});
const renderCell = (key = apiKey) => renderToStaticMarkup(React.createElement(cell.ApiKeyQuotaCell, { apiKey: key }));

test('three independent limits stay visible with the channel remaining-percent convention', () => {
  const html = renderCell();
  assert.equal((html.match(/role="progressbar"/g) ?? []).length, 3);
  for (const expected of ['86.00%', '77.50%', '18.00%', '请求次数', '总 Token', '费用']) assert.ok(html.includes(expected));
  assert.doesNotMatch(html, /chevron|aria-expanded/);
  assert.match(html, /滚动统计/);
  assert.equal(queryOptions.refetchInterval, 30000);
  assert.equal(queryOptions.silent, true);
});

test('only configured metrics get bars; an inactive quota never applies to the key', () => {
  const oneQuota = { ...quota, requests: null, totalTokens: null };
  const html = renderCell({ ...apiKey, profiles: { activeProfile: 'production', profiles: [{ name: 'production', quota: oneQuota }] } });
  assert.equal((html.match(/role="progressbar"/g) ?? []).length, 1);
  const unlimited = renderCell({ ...apiKey, profiles: { ...apiKey.profiles, activeProfile: 'missing' } });
  assert.match(unlimited, /∞/);
  assert.equal(queryOptions.enabled, false);
});

test('missing and failed usage are unknown, not zero use or unlimited capacity', () => {
  query = { ...query, isError: true };
  const html = renderCell();
  assert.match(html, /用量暂不可用/);
  assert.doesNotMatch(html, /86\.00%|100\.00%|∞|aria-valuenow/);
  query = { ...query, isError: false };
  assert.equal(helpers.getQuotaMetrics(quota)[0].used, null);
});

test('usage is clamped to zero remaining when exceeded, without hiding the real used value', () => {
  const [metric] = helpers.getQuotaMetrics(quota, { ...snapshot.usage, requestCount: 1200 });
  assert.equal(metric.used, 1200);
  assert.equal(metric.remaining, 0);
  assert.equal(metric.remainingPercent, 0);
  assert.equal(metric.exhausted, true);
  assert.equal(helpers.getQuotaMetrics(quota, { ...snapshot.usage, requestCount: 0 })[0].remainingPercent, 100);
  const zeroCost = helpers.getQuotaMetrics({ ...quota, cost: 0 }, { ...snapshot.usage, totalCost: 0 })[2];
  assert.equal(zeroCost.remainingPercent, 0);
  assert.equal(zeroCost.exhausted, true);
});

test('a changed rolling or calendar period cannot reuse the old window', () => {
  assert.equal(helpers.isSameQuotaPeriod(quota.period, { type: 'past_duration', pastDuration: { value: 2, unit: 'day' } }), false);
  assert.equal(helpers.isSameQuotaPeriod(quota.period, { type: 'calendar_duration', calendarDuration: { unit: 'day' } }), false);
  assert.equal(
    helpers.isSameQuotaPeriod(
      { type: 'calendar_duration', calendarDuration: { unit: 'day' } },
      { type: 'calendar_duration', calendarDuration: { unit: 'month' } }
    ),
    false
  );
  assert.equal(helpers.isSameQuotaPeriod(quota.period, { ...quota.period, calendarDuration: { unit: 'month' } }), true);
  const html = renderCell({
    ...apiKey,
    profiles: { activeProfile: 'production', profiles: [{ name: 'production', quota: { ...quota, period: { type: 'all_time' } } }] },
  });
  assert.doesNotMatch(html, /86\.00%|77\.50%/);
});

test('details retain units, currency, K/M suffixes and exactly two fractional digits', () => {
  const html = renderToStaticMarkup(React.createElement(usage.ApiKeyQuotaUsage, { quota, snapshot }));
  for (const expected of ['140.00 次', '1.00K 次', '450.00K Token', '2.00M Token', '1.55M Token', '8.20', '10.00', '77.50%'])
    assert.ok(html.includes(expected), expected);
  assert.match(html, /[¥￥]/);
  assert.match(html, /Asia\/Shanghai/);
  assert.match(html, /滚动统计/);
  assert.doesNotMatch(html, /下次重置/);
  assert.equal(numbers.formatNumber(1000), '1K');
  assert.equal(numbers.formatNumber(1000000000, { digits: 2, trimTrailingZeros: false }), '1.00B');
});

test('unlimited dimensions have no bar and natural periods alone show reset times', () => {
  const onlyCost = {
    ...quota,
    requests: null,
    totalTokens: null,
    period: { type: 'calendar_duration', calendarDuration: { unit: 'month' } },
  };
  const html = renderToStaticMarkup(React.createElement(usage.ApiKeyQuotaUsage, { quota: onlyCost, snapshot }));
  assert.equal((html.match(/role="progressbar"/g) ?? []).length, 1);
  assert.match(html, /∞/);
  assert.match(html, /下次重置/);
  const missing = renderToStaticMarkup(React.createElement(usage.ApiKeyQuotaUsage, { quota }));
  assert.doesNotMatch(missing, /aria-valuenow|100\.00%/);
});

test('quota translations exist in both languages', () => {
  for (const file of [
    './api-key-quota-cell.tsx',
    './api-key-quota-usage.tsx',
    '../data/quota-display.ts',
    './apikeys-profiles-dialog.tsx',
    './apikeys-create-template-dialog.tsx',
    './apikeys-edit-template-dialog.tsx',
  ]) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8');
    for (const [, key] of source.matchAll(/['"](apikeys\.(?:quota|profiles\.quota)[\w.]*)['"]/g)) {
      assert.ok(zh[key], `${key} missing in Chinese`);
      assert.ok(en[key], `${key} missing in English`);
    }
  }
});

test('natural weeks round-trip through key, profile, template and quota usage schemas', () => {
  const schema = load('../data/schema.ts', {
    '@/gql/pagination': { pageInfoSchema: z.any() },
    '@/features/users/data/schema': { userSchema: z.object({}) },
  });
  const { formSchemaFactory } = load('../data/template-form-schema.ts');
  const weeklyQuota = { ...quota, period: { type: 'calendar_duration', calendarDuration: { unit: 'week' } } };
  const profile = { name: 'production', modelMappings: [], channelTagsMatchMode: 'any', quota: weeklyQuota };
  const profiles = { activeProfile: profile.name, profiles: [profile] };
  for (const inputSchema of [schema.updateApiKeyProfilesInputSchema, schema.updateApiKeyProfilesInputSchemaFactory(t)]) {
    assert.deepEqual(inputSchema.parse(profiles).profiles[0].quota, weeklyQuota);
  }
  const key = schema.apiKeySchema.parse({
    ...apiKey,
    key: 'test-only-key',
    status: 'enabled',
    createdAt: new Date(),
    updatedAt: new Date(),
    profiles,
  });
  assert.deepEqual(key.profiles.profiles[0].quota, weeklyQuota);
  const template = { name: 'weekly-template', projectID: 'project-1', profile };
  for (const inputSchema of [
    formSchemaFactory(t),
    schema.createApiKeyProfileTemplateInputSchema,
    schema.updateApiKeyProfileTemplateInputSchema,
  ]) {
    assert.deepEqual(inputSchema.parse(template).profile.quota, weeklyQuota);
  }
  const saved = schema.apiKeyProfileTemplateSchema.parse({
    ...template,
    id: 'template-1',
    createdAt: '2026-09-11T07:00:00Z',
    updatedAt: '2026-09-11T07:00:00Z',
  });
  assert.deepEqual(saved.profile.quota, weeklyQuota);
  assert.deepEqual(schema.apiKeyProfileQuotaUsageSchema.parse({ ...snapshot, quota: weeklyQuota }).quota, weeklyQuota);
});

test('natural week shows its own caption and Monday reset with all three metrics', () => {
  const weeklyQuota = { ...quota, period: { type: 'calendar_duration', calendarDuration: { unit: 'week' } } };
  const weeklySnapshot = {
    ...snapshot,
    quota: weeklyQuota,
    window: { start: new Date('2026-09-06T16:00:00Z'), end: new Date('2026-09-13T16:00:00Z') },
  };
  const previousQuery = query;
  try {
    query = { ...query, data: [weeklySnapshot] };
    const html = renderCell({
      ...apiKey,
      profiles: { activeProfile: 'production', profiles: [{ name: 'production', quota: weeklyQuota }] },
    });
    assert.match(html, /自然周/);
    assert.match(html, /text-\[10px\]/);
    assert.equal((html.match(/role="progressbar"/g) ?? []).length, 3);
    assert.match(html, /18\.00%/);
    assert.doesNotMatch(html, /自然日|自然月/);
    const details = renderToStaticMarkup(React.createElement(usage.ApiKeyQuotaUsage, { quota: weeklyQuota, snapshot: weeklySnapshot }));
    assert.match(details, /自然周/);
    assert.match(details, /下次重置：2026年9月14日 00:00/);
    assert.match(details, /Asia\/Shanghai/);
    assert.match(details, /450\.00K Token/);
  } finally {
    query = previousQuery;
  }
});

test('quota queries are scoped, cancellable, silent on row errors, and invalidated on save', async () => {
  const schema = load('../data/schema.ts', {
    '@/gql/pagination': { pageInfoSchema: z.any() },
    '@/features/users/data/schema': { userSchema: z.object({}) },
  });
  let token = 'first-user';
  let project = 'first-project';
  let response = { apiKeyQuotaUsages: [snapshot] };
  let error;
  let toastCount = 0;
  const calls = [];
  const invalidated = [];
  const data = load('../data/apikeys.ts', {
    '@tanstack/react-query': {
      useQuery: (options) => options,
      useMutation: (options) => options,
      useQueryClient: () => ({ invalidateQueries: ({ queryKey }) => invalidated.push(queryKey) }),
    },
    '@/gql/graphql': {
      graphqlRequest: async (...args) => {
        calls.push(args);
        if (error) throw error;
        return response;
      },
    },
    'react-i18next': { useTranslation: () => ({ t }) },
    '@/stores/authStore': { useAuthStore: (select) => select({ auth: { accessToken: token } }) },
    '@/stores/projectStore': { useSelectedProjectId: () => project },
    '@/hooks/use-error-handler': {
      useErrorHandler: () => ({
        handleError() {
          toastCount++;
        },
      }),
    },
    '../../../hooks/useRequestPermissions': { useRequestPermissions: () => ({ canViewUsers: false }) },
    './search-filter': load('../data/search-filter.ts'),
    './schema': schema,
    sonner: { toast: { success() {} } },
  });
  const first = data.useApiKeyQuotaUsages('key-1', { silent: true });
  const signal = new AbortController().signal;
  assert.deepEqual(await first.queryFn({ signal }), [snapshot]);
  assert.deepEqual(calls[0][1], { apiKeyId: 'key-1' });
  assert.deepEqual(calls[0][2], { 'X-Project-ID': project });
  assert.equal(calls[0][3].signal, signal);
  token = 'second-user';
  assert.notDeepEqual(data.useApiKeyQuotaUsages('key-1').queryKey, first.queryKey);
  project = 'second-project';
  assert.notDeepEqual(data.useApiKeyQuotaUsages('key-1').queryKey, first.queryKey);
  assert.equal(data.useApiKeyQuotaUsages('key-1', { enabled: false }).enabled, false);
  token = '';
  assert.equal(data.useApiKeyQuotaUsages('key-1').enabled, false);
  error = new Error('read failed');
  await assert.rejects(first.queryFn({ signal }));
  assert.equal(toastCount, 0);
  error = undefined;
  response = {
    apiKeys: {
      edges: [{ node: { ...apiKey, key: 'test-only-key', status: 'enabled', createdAt: new Date(), updatedAt: new Date() }, cursor: 'c1' }],
      pageInfo: {},
      totalCount: 1,
    },
  };
  const list = await data.useApiKeys().queryFn();
  assert.deepEqual(list.edges[0].node.profiles.profiles[0].quota, quota);
  assert.match(calls.at(-1)[0], /quota\s*\{\s*requests\s+totalTokens\s+cost\s+period\s*\{/);
  data.useUpdateApiKeyProfiles().onSuccess({}, { id: 'key-1' });
  assert.ok(invalidated.some((key) => key[0] === 'apiKeyQuotaUsages' && key[1] === 'key-1'));
  data.useLoadApiKeyProfileTemplate().onSuccess({}, { apiKeyID: 'key-2' });
  assert.ok(invalidated.some((key) => key[0] === 'apiKeyQuotaUsages' && key[1] === 'key-2'));
  data.useUpdateApiKeyProfileTemplate().onSuccess();
  assert.ok(invalidated.some((key) => key[0] === 'apiKeyQuotaUsages' && key.length === 1));
});
