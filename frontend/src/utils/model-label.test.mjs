import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { formatModelLabel } from './model-label.ts';

test('legacy missing model labels are localized, while real model IDs remain intact', () => {
  for (const [locale, expected] of [['zh-CN', '原始模型未记录'], ['en', 'Original model not recorded']]) {
    const messages = JSON.parse(readFileSync(new URL(`../locales/${locale}/billing.json`, import.meta.url), 'utf8'));
    const t = (key) => key.split('.').reduce((value, part) => value?.[part], messages) ?? key;
    assert.equal(formatModelLabel('[original model unavailable]', t), expected);
    assert.equal(formatModelLabel('deepseek-v4-pro', t), 'deepseek-v4-pro');
    assert.equal(formatModelLabel(undefined, t), 'requests.columns.unknown');
  }
});
