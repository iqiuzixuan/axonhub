import React from 'react';
import { createInstance } from 'i18next';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { I18nextProvider, useTranslation } from 'react-i18next';
import ts from 'typescript';
import { parseDeepSeekBalances } from '../features/system/data/deepseek-balance-display.ts';

const i18n = createInstance();
await i18n.init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'quota.label.account_balance': 'Account Balance',
        'quota.label.unavailable': 'N/A',
        'currencies.format': '{{val, currency}}',
      },
    },
  },
});
const source = readFileSync(new URL('./deepseek-balance-bars.tsx', import.meta.url), 'utf8').replace(/^import[^\n]*\n/gm, '');
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.React },
});
globalThis.__deepSeekRenderBindings = { React, useTranslation, parseDeepSeekBalances };
const { DeepSeekBalanceBars } = await import(
  `data:text/javascript;base64,${Buffer.from(
    'const { React, useTranslation, parseDeepSeekBalances } = globalThis.__deepSeekRenderBindings;\n' + outputText
  ).toString('base64')}`
);
delete globalThis.__deepSeekRenderBindings;

const render = (total, baseline, status = 'available', extra = {}) =>
  renderToStaticMarkup(
    React.createElement(
      I18nextProvider,
      { i18n },
      React.createElement(DeepSeekBalanceBars, {
        status,
        quotaData: { balance_infos: [{ currency: 'CNY', total_balance: total, baseline_balance: baseline }], ...extra },
      })
    )
  );

test('actual balance component shows a remaining bar and current currency amount only', () => {
  const html = render('60', '100');
  assert.match(html, /style="width:60%"/);
  assert.match(html, /CN¥60\.00/);
  const visibleText = html.replace(/<[^>]*>/g, '');
  assert.equal(visibleText, 'Account BalanceCN¥60.00');
  assert.doesNotMatch(visibleText, /%|100|Granted|Paid|Reset|Available/);
  assert.match(html, /bg-green-500/);
});

test('recharge, yellow and red consumption, and zero render consistently', () => {
  assert.match(render('150', '150'), /style="width:100%"/);
  assert.match(render('50', '100'), /bg-yellow-500/);
  assert.match(render('20', '100'), /bg-red-500/);
  const empty = render('0', '0', 'exhausted');
  assert.match(empty, /style="width:0%"/);
  assert.match(empty, /CN¥0\.00/);
});

test('failed query renders N/A instead of stale balance or zero', () => {
  for (const html of [render('60', '100', 'unknown'), render('60', '100', 'available', { error_code: 'check_failed' })]) {
    assert.match(html, />N\/A</);
    assert.doesNotMatch(html, /progressbar|¥|60|0\.00/);
  }
});
