import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../../components/quota-badges.tsx', import.meta.url), 'utf8');
for (const window of ['gemini_5h', 'gemini_7d', 'claude_gpt_5h', 'claude_gpt_7d']) {
  test(`Antigravity ${window} retains its translated quota label`, () => {
    assert.ok(source.includes(`${window}: \'quota.window.${window}\'`));
    for (const language of ['en', 'zh-CN']) {
      const messages = JSON.parse(readFileSync(new URL(`../../../locales/${language}/system.json`, import.meta.url)));
      assert.match(messages[`quota.window.${window}`], window.startsWith('gemini') ? /Gemini/ : /Claude\/GPT/);
    }
  });
}
