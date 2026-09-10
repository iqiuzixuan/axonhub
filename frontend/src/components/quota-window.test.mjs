import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

function transpile(source) {
  return ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.React },
  }).outputText;
}

const helperSource = readFileSync(new URL('../features/system/data/quota-window-display.ts', import.meta.url), 'utf8');
const helpers = await import(`data:text/javascript;base64,${Buffer.from(transpile(helperSource)).toString('base64')}`);
const locale = JSON.parse(readFileSync(new URL('../locales/zh-CN/system.json', import.meta.url), 'utf8'));
const t = (key, values = {}) => key === 'currencies.format'
  ? new Intl.NumberFormat(values.locale, { style: 'currency', currency: values.currency, minimumFractionDigits: 2 }).format(values.val)
  : (locale[key] ?? key).replace(/\{\{(\w+)\}\}/g, (_, name) => String(values[name] ?? ''));
const now = Date.parse('2026-09-10T00:00:00Z');
const limit = (overrides = {}) => ({
  type: 'token', window: '7d', status: 'available', ready: true, usageRatio: 0.17,
  periodStart: '2026-09-09T00:00:00Z', nextResetAt: '2026-09-16T00:00:00Z', ...overrides,
});

test('usage labels distinguish zero, fractional usage, and unavailable values', () => {
  assert.equal(helpers.formatQuotaUsage(helpers.getQuotaWindowUsage(limit({ usageRatio: 0 })), t), '已使用 0%');
  assert.equal(helpers.formatQuotaUsage(helpers.getQuotaWindowUsage(limit({ usageRatio: 0.004 })), t), '已使用 <1%');
  assert.equal(helpers.formatQuotaUsage(helpers.getQuotaWindowUsage(limit()), t), '已使用 17%');
  assert.equal(helpers.getQuotaWindowUsage(limit({ status: 'unknown', usageRatio: 0 })), undefined);
  assert.equal(helpers.getQuotaWindowUsage(limit({ usageRatio: Number.NaN })), undefined);
});

test('elapsed time and reset countdown require real provider timestamps', () => {
  assert.ok(Math.abs(helpers.getQuotaWindowDurationPercent(limit(), now) - 100 / 7) < 1e-10);
  assert.equal(helpers.formatQuotaReset(limit().nextResetAt, t, now), '重置于 6 天 0 小时');
  assert.equal(helpers.formatQuotaReset('2026-09-09T00:00:00Z', t, now), t('quota.label.reset_now'));
  for (const nextResetAt of [undefined, 'bad-date']) {
    assert.equal(helpers.getQuotaWindowDurationPercent(limit({ nextResetAt }), now), undefined);
    assert.equal(helpers.formatQuotaReset(nextResetAt, t, now), '上游未提供重置时间');
  }
  assert.equal(helpers.getQuotaWindowDurationPercent(limit({ periodStart: '2026-09-17T00:00:00Z' }), now), undefined);
});

test('period estimate explains missing reset, zero usage, or missing priced usage without inventing totals', () => {
  assert.equal(helpers.getPeriodQuotaUnavailableReason(limit({ nextResetAt: undefined })), 'quota.label.reset_unavailable');
  assert.equal(helpers.getPeriodQuotaUnavailableReason(limit({ usageRatio: 0 })), 'quota.label.period_quota_no_usage');
  assert.equal(helpers.getPeriodQuotaUnavailableReason(limit()), 'quota.label.period_quota_no_cost');
  assert.equal(helpers.getPeriodQuotaUnavailableReason(limit({ periodCost: 12, periodQuota: 60 })), undefined);
  assert.equal(helpers.hasPeriodQuotaWindow(limit()), true);
  assert.equal(helpers.hasPeriodQuotaWindow(limit({ type: 'credit', window: 'credits' })), false);
});

const passthrough = ({ children }) => React.createElement(React.Fragment, null, children);
globalThis.__quotaWindowTest = {
  React, ...helpers,
  useTranslation: () => ({ t, i18n: { language: 'zh-CN' } }),
  useGeneralSettings: () => ({ data: { currencyCode: 'CNY' } }),
  Tooltip: passthrough, TooltipContent: passthrough, TooltipTrigger: passthrough,
};
const componentSource = readFileSync(new URL('./quota-window.tsx', import.meta.url), 'utf8')
  .replace(/^import[\s\S]*?;\n/gm, '')
  .replace(/^export \{[^\n]+\n/gm, '');
const prelude = `const { ${Object.keys(globalThis.__quotaWindowTest).join(', ')} } = globalThis.__quotaWindowTest;\n`;
const components = await import(`data:text/javascript;base64,${Buffer.from(prelude + transpile(componentSource)).toString('base64')}`);

test('shared popover windows render used progress, weekly label and real reset details', () => {
  const html = renderToStaticMarkup(React.createElement(components.QuotaWindows, { limits: [limit()] }));
  assert.match(html, /每周窗口/);
  assert.match(html, /已使用 17%/);
  assert.match(html, /width:17%/);
  assert.match(html, /重置于|已重置|即将重置/);
  assert.match(html, /aria-hidden/);
});

test('Qianwen zero usage with no reset shows missing reset and estimate reason, not an invented allowance', () => {
  const limits = [limit({ usageRatio: 0, periodStart: undefined, nextResetAt: undefined })];
  const html = renderToStaticMarkup(React.createElement(React.Fragment, null,
    React.createElement(components.QuotaWindows, { limits }),
    React.createElement(components.PeriodQuotaEstimate, { limits, showUnavailable: true })
  ));
  assert.match(html, /已使用 0%/);
  assert.match(html, /上游未提供重置时间/);
  assert.match(html, /预计周期额度/);
  assert.doesNotMatch(html, /aria-hidden|¥|￥|约/);
});

test('other providers retain hidden estimates when no amount is supplied', () => {
  const html = renderToStaticMarkup(React.createElement(components.PeriodQuotaEstimate, { limits: [limit()] }));
  assert.equal(html, '');
});

test('priced windows preserve reported period cost and estimate', () => {
  const html = renderToStaticMarkup(React.createElement(components.PeriodQuotaEstimate, {
    limits: [limit({ periodCost: 12, periodQuota: 48 })], showUnavailable: true,
  }));
  assert.match(html, /12\.00/);
  assert.match(html, /48\.00/);
  assert.doesNotMatch(html, /上游未提供|暂无可计价/);
});
