import assert from 'node:assert/strict';
import test from 'node:test';
import { getPersonalDateRange, isPersonalDateRangeValid, personalUsageCSV, personalModelExample } from './utils.ts';

test('presets use the server calendar date across months and leap years', () => {
  assert.deepEqual(getPersonalDateRange('2024-03-01', '7', { start: '', end: '' }), { start: '2024-02-24', end: '2024-03-01' });
  assert.deepEqual(getPersonalDateRange('2026-09-11', 'month', { start: '', end: '' }), { start: '2026-09-01', end: '2026-09-11' });
});

test('custom dates reject normalization, invalid order and unbounded ranges', () => {
  for (const dates of [
    ['2026-02-30', '2026-03-01'],
    ['', '2026-03-01'],
    ['2026-03-02', '2026-03-01'],
    ['2026-01-01', '2026-12-31'],
  ]) {
    assert.equal(isPersonalDateRangeValid(...dates), false, dates.join(','));
  }
  assert.equal(isPersonalDateRangeValid('2026-01-01', '2026-04-03'), true);
  assert.equal(isPersonalDateRangeValid('2026-01-01', '2026-04-04'), false);
});

test('CSV preserves numbers, quotes and line breaks while neutralizing formulas', () => {
  assert.equal(
    personalUsageCSV([['a,"b"\nc', '=SUM(A1)', ' @cmd', -2, 3_000_000_000]]),
    '\uFEFF"a,""b""\nc","\'=SUM(A1)","\' @cmd","-2","3000000000"'
  );
});

test('copyable examples select the correct protocol and escape model identifiers', () => {
  const curl = personalModelExample("model';echo unsafe;'", 'embedding', 'https://example.invalid/v1', 'curl');
  assert.match(curl, /\/embeddings/);
  assert.ok(curl.includes("model'\\'';echo unsafe;'\\''"));
  assert.match(curl, /AXONHUB_API_KEY/);
  const python = personalModelExample('model"\nname', 'chat', 'https://example.invalid/v1', 'python');
  assert.match(python, /chat.completions.create/);
  assert.ok(python.includes('model=' + JSON.stringify('model"\nname')));
});
