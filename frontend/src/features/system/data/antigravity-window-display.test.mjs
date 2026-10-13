import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { quotaWindowLabel, hasPeriodQuotaWindow, getQuotaWindowDurationPercent } from './quota-window-display.ts';

for (const window of ['gemini_5h', 'gemini_7d', 'claude_gpt_5h', 'claude_gpt_7d']) {
  test(`Antigravity ${window} uses translated labels and supports period estimates`, () => {
    const limit = { type: 'token', window, periodStart: '2026-10-13T00:00:00Z', nextResetAt: '2026-10-13T10:00:00Z' };
    assert.equal(hasPeriodQuotaWindow(limit), true);
    assert.equal(getQuotaWindowDurationPercent(limit, Date.parse('2026-10-13T05:00:00Z')), 50);
    for (const language of ['en', 'zh-CN']) {
      const messages = JSON.parse(readFileSync(new URL(`../../../locales/${language}/system.json`, import.meta.url)));
      const label = quotaWindowLabel(window, (key) => messages[key]);
      assert.equal(typeof label, 'string');
      assert.notEqual(label, window);
      assert.match(label, window.startsWith('gemini') ? /Gemini/ : /Claude\/GPT/);
    }
  });
}
