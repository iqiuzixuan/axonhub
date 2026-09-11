import assert from 'node:assert/strict';
import test from 'node:test';
import { buildDimensionChartData } from './dimension-chart-data.ts';

const stat = (id, value, name = 'default') => ({
  id, name, apiKeyUserName: 'Alex', requestCount: value, cost: value,
  totalTokens: value, inputTokens: value, cachedInputTokens: 0, outputTokens: 0,
});

test('identical key and owner names keep separate IDs, values and percentages in every metric', () => {
  const data = [stat('1', 30), stat('2', 70)];
  for (const metric of ['totalTokens', 'requestCount', 'cost']) {
    const items = buildDimensionChartData(data, metric, 'Other');
    assert.deepEqual(items.map(({ id, name, percentage }) => ({ id, name, percentage })), [
      { id: 'item:2', name: 'default', percentage: 70 },
      { id: 'item:1', name: 'default', percentage: 30 },
    ]);
  }
  assert.deepEqual(data.map(item => item.id), ['1', '2']);
});

test('a real item named Other cannot collide with the grouped remainder', () => {
  const items = buildDimensionChartData([stat('other', 99, 'Other'), stat('2', 1)], 'cost', 'Other');
  assert.deepEqual(items.map(item => [item.id, item.name, item.percentage]), [
    ['item:other', 'Other', 99], ['other', 'Other', 1],
  ]);
});

test('empty and zero-valued statistics produce no chart segments', () => {
  assert.deepEqual(buildDimensionChartData([], 'cost', 'Other'), []);
  assert.deepEqual(buildDimensionChartData([stat('1', 0)], 'cost', 'Other'), []);
});
