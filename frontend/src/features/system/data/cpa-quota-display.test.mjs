import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const { outputText } = ts.transpileModule(readFileSync(new URL('./cpa-quota-display.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
});
const { getCPAPoolUsagePercentage } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const window = (account, window, usageRatio) => ({ account, window, usageRatio, type: 'token', ready: usageRatio < 1,
  status: usageRatio === 1 ? 'exhausted' : 'available', availabilityGroup: 'cpa_accounts' });

test('a single weekly CPA account displays its real consumption', () => {
  assert.equal(getCPAPoolUsagePercentage([window('', '7d', .11)]), 11);
});

test('pool summary never combines spare windows from two unusable accounts', () => {
  const a = [window('a', '5h', 1), window('a', '7d', .1)];
  const b = [window('b', '5h', .1), window('b', '7d', 1)];
  assert.equal(getCPAPoolUsagePercentage([...a, ...b]), 100);
});

test('pool summary uses a healthy account instead of adding, averaging or showing the exhausted account', () => {
  const a = [window('a', '5h', 1), window('a', '7d', .1)];
  const b = [window('b', '5h', .2), window('b', '7d', .3)];
  const c = [window('c', '5h', .5), window('c', '7d', .6)];
  assert.equal(getCPAPoolUsagePercentage([...a, ...b, ...c]), 30);
});
