import assert from 'node:assert/strict';
import test from 'node:test';
import { parseDeepSeekBalances } from './deepseek-balance-display.ts';

const data = (total = '60', baseline = '100', currency = 'CNY') => ({
  balance_infos: [{ currency, total_balance: total, baseline_balance: baseline }],
});

test('remaining bar uses the persisted baseline and retains current total', () => {
  assert.deepEqual(parseDeepSeekBalances(data(), 'available'), [{ currency: 'CNY', totalBalance: 60, remainingPercent: 60 }]);
  assert.equal(parseDeepSeekBalances(data('150', '150'), 'available')[0].remainingPercent, 100);
  assert.equal(parseDeepSeekBalances(data('0', '0'), 'exhausted')[0].remainingPercent, 0);
  assert.equal(parseDeepSeekBalances(data('0', '100'), 'exhausted')[0].totalBalance, 0);
});

test('failed reads hide stale balance and baseline without presenting zero', () => {
  assert.deepEqual(parseDeepSeekBalances(data(), 'unknown'), []);
  assert.deepEqual(parseDeepSeekBalances({ ...data(), error_code: 'check_failed' }, 'available'), []);
  assert.deepEqual(parseDeepSeekBalances(undefined, 'unknown'), []);
});

test('invalid monetary data never renders a fabricated balance', () => {
  for (const [total, baseline, currency] of [
    ['', '100', 'CNY'],
    [null, '100', 'CNY'],
    ['NaN', '100', 'CNY'],
    ['-1', '100', 'CNY'],
    ['1', '0', 'CNY'],
    ['101', '100', 'CNY'],
    ['1', '100', 'EUR'],
    ['1e99999', '100', 'CNY'],
  ]) {
    assert.deepEqual(parseDeepSeekBalances(data(total, baseline, currency), 'available'), []);
  }
});

test('currencies render independently without converting or adding balances', () => {
  const combined = { balance_infos: [...data('60', '100').balance_infos, ...data('5', '10', 'USD').balance_infos] };
  assert.deepEqual(
    parseDeepSeekBalances(combined, 'available').map((b) => b.remainingPercent),
    [60, 50]
  );
  assert.deepEqual(parseDeepSeekBalances({ balance_infos: [...data().balance_infos, ...data().balance_infos] }, 'available'), []);
});
