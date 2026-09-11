import assert from 'node:assert/strict';
import test from 'node:test';
import { formatApiKeyLabel } from './utils.ts';

test('same key names identify their owners without changing the stored name', () => {
  assert.equal(formatApiKeyLabel('default', '张 三'), 'default · 张 三');
  assert.equal(formatApiKeyLabel('default', 'Alex'), 'default · Alex');
  assert.equal(formatApiKeyLabel('Paid', '  张 三  '), 'Paid · 张 三');
});

test('missing, deleted or unavailable owners keep the original name without a separator', () => {
  for (const owner of [null, undefined, '', '  ']) {
    assert.equal(formatApiKeyLabel('service-key', owner), 'service-key');
  }
  assert.equal(formatApiKeyLabel(null, 'Alex'), '');
});
