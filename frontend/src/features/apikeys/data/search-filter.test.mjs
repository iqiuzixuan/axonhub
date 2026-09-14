import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import ts from 'typescript';
import { z } from 'zod';
import { buildAPIKeySearchFilter } from './search-filter.ts';

test('list searches preserve key matching and add the owner name as an alternative', () => {
  assert.deepEqual(buildAPIKeySearchFilter('  张 三  ', { canViewUsers: true, includeKey: true }), {
    or: [
      { nameContainsFold: '张 三' },
      { keyContainsFold: '张 三' },
      { hasUserWith: [{ nameContainsFold: '张 三' }] },
    ],
  });
});

test('unavailable user details cannot be searched and blank input does not add a filter', () => {
  assert.deepEqual(buildAPIKeySearchFilter('Alex', { canViewUsers: false }), { or: [{ nameContainsFold: 'Alex' }] });
  assert.deepEqual(buildAPIKeySearchFilter('test-key', { canViewUsers: false, includeKey: true }), {
    or: [{ nameContainsFold: 'test-key' }, { keyContainsFold: 'test-key' }],
  });
  for (const search of [undefined, '', ' \t\u3000 ']) {
    assert.deepEqual(buildAPIKeySearchFilter(search, { canViewUsers: true, includeKey: true }), {});
  }
});

function optionsHarness(canViewUsers) {
  const calls = [];
  const dependencies = {
    '@tanstack/react-query': { useInfiniteQuery: (options) => options, useQuery: (options) => options },
    '@/gql/graphql': {
      graphqlRequest: async (query, variables, headers) => {
        calls.push({ query, variables, headers });
        return { apiKeys: { edges: [], pageInfo: { hasNextPage: false, endCursor: null } } };
      },
    },
    'react-i18next': { useTranslation: () => ({ t: (key) => key }) },
    '@/stores/authStore': {},
    '@/stores/projectStore': { useSelectedProjectId: () => 'gid://axonhub/Project/3' },
    '@/hooks/use-error-handler': { useErrorHandler: () => ({ handleError() {} }) },
    '../../../hooks/useRequestPermissions': { useRequestPermissions: () => ({ canViewUsers }) },
    './search-filter': { buildAPIKeySearchFilter },
    './schema': { apiKeyStatusSchema: z.enum(['enabled', 'disabled', 'archived']) },
    sonner: {},
  };
  const file = new URL('./apikeys.ts', import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(file, 'utf8'), {
    fileName: file.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS },
  });
  const exports = {};
  const require = createRequire(import.meta.url);
  new Function('require', 'exports', outputText)((name) => dependencies[name] ?? require(name), exports);
  return { ...exports, calls };
}

test('request, analytics and prompt selectors send owner search to the server before pagination', async () => {
  const { useApiKeyOptions, calls } = optionsHarness(true);
  const query = useApiKeyOptions({ search: '  aLeX ', includeArchived: true });
  await query.queryFn({ pageParam: 'next-page-cursor' });
  assert.equal(query.enabled, true);
  assert.deepEqual(calls[0].headers, { 'X-Project-ID': 'gid://axonhub/Project/3' });
  assert.equal(calls[0].variables.after, 'next-page-cursor');
  assert.equal(calls[0].variables.first, 100);
  assert.deepEqual(calls[0].variables.where, {
    typeNotIn: ['noauth'],
    statusIn: ['enabled', 'disabled', 'archived'],
    or: [{ nameContainsFold: 'aLeX' }, { hasUserWith: [{ nameContainsFold: 'aLeX' }] }],
  });
  assert.match(calls[0].query, /user\s*\{\s*name\s*\}/);
});

test('selector permission fallback preserves status filtering and omits user fields', async () => {
  const { useApiKeyOptions, calls } = optionsHarness(false);
  await useApiKeyOptions({ search: 'default' }).queryFn({ pageParam: undefined });
  assert.deepEqual(calls[0].variables.where, {
    typeNotIn: ['noauth'], statusIn: ['enabled', 'disabled'], or: [{ nameContainsFold: 'default' }],
  });
  assert.doesNotMatch(calls[0].query, /\buser\s*\{/);
});

test('selected key backfill stays ID-based when the search changes', async () => {
  const { useApiKeyOptionsByIDs, calls } = optionsHarness(true);
  const ids = ['gid://axonhub/APIKey/7'];
  await useApiKeyOptionsByIDs(ids).queryFn();
  assert.deepEqual(calls[0].variables.where, {
    typeNotIn: ['noauth'], statusIn: ['enabled', 'disabled', 'archived'], idIn: ids,
  });
});
