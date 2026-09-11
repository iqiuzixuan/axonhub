import { QueryClient } from '@tanstack/react-query';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test, { beforeEach } from 'node:test';
import ts from 'typescript';

const require = createRequire(import.meta.url);
let accessToken;
let projectId;
let canViewUsers;
let requests;

const dependencies = {
  '@tanstack/react-query': { useQuery: (options) => options },
  'react-i18next': { useTranslation: () => ({ t: (key) => key }) },
  '@/stores/authStore': { useAuthStore: (select) => select({ auth: { accessToken } }) },
  '@/stores/projectStore': { useSelectedProjectId: () => projectId },
  '@/hooks/useRequestPermissions': { useRequestPermissions: () => ({ canViewUsers }) },
  '@/hooks/use-error-handler': { useErrorHandler: () => ({ handleError() {} }) },
  '@/hooks/usePermissions': { usePermissions: () => ({ hasSystemScope: () => false }) },
  '@/lib/i18n': { default: { t: (key) => key } },
  sonner: {},
  '@/gql/graphql': {
    graphqlRequest: async (query, variables, headers) => {
      requests.push({ query, variables, headers });
      if (query.includes('brandSettings')) {
        return { brandSettings: { brandName: 'Company', brandLogo: 'data:image/png;base64,dGVzdA==', title: 'Company Gateway' } };
      }
      return { users: { edges: [{ node: { id: 'user-1', name: 'Member', email: 'member@example.com' } }] } };
    },
  },
};

function loadModule(path) {
  const file = new URL(path, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(file, 'utf8'), {
    fileName: file.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS },
  });
  const exports = {};
  new Function('require', 'exports', outputText)((name) => dependencies[name] ?? require(name), exports);
  return exports;
}

const { useApiKeyCreatorOptions } = loadModule('./creator-options.ts');
const { useBrandSettings } = loadModule('../../system/data/system.ts');

beforeEach(() => {
  accessToken = 'member-test-token';
  projectId = 'gid://axonhub/Project/3';
  canViewUsers = true;
  requests = [];
});

test('creator options request only display fields and explicitly filter the current project', async () => {
  const query = useApiKeyCreatorOptions();
  assert.equal(query.enabled, true);
  assert.deepEqual(await query.queryFn(), [{ id: 'user-1', name: 'Member', email: 'member@example.com' }]);
  assert.deepEqual(requests[0].headers, { 'X-Project-ID': projectId });
  assert.deepEqual(requests[0].variables, { where: { hasProjectsWith: [{ id: projectId }] } });
  assert.doesNotMatch(requests[0].query, /\broles\b|\bscopes\b/);
});

test('creator options stay disabled without user read permission, project selection, or login', () => {
  assert.equal(useApiKeyCreatorOptions({ enabled: false }).enabled, false);
  canViewUsers = false;
  assert.equal(useApiKeyCreatorOptions().enabled, false);
  canViewUsers = true;
  projectId = null;
  assert.equal(useApiKeyCreatorOptions().enabled, false);
  projectId = 'project-1';
  accessToken = '';
  assert.equal(useApiKeyCreatorOptions().enabled, false);
  assert.equal(requests.length, 0);
});

test('members without system settings permission still load the shared brand', async () => {
  const query = useBrandSettings();
  assert.equal(query.enabled, true);
  assert.deepEqual(await query.queryFn(), { brandName: 'Company', brandLogo: 'data:image/png;base64,dGVzdA==', title: 'Company Gateway' });
  assert.equal(useBrandSettings({ enabled: false }).enabled, false);
  accessToken = '';
  assert.equal(useBrandSettings().enabled, false);
});

test('new accounts do not inherit cached creator options or skip their own brand query', async () => {
  const cache = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  try {
    await cache.fetchQuery(useApiKeyCreatorOptions());
    await cache.fetchQuery(useBrandSettings());
    accessToken = 'another-member-token';
    await cache.fetchQuery(useApiKeyCreatorOptions());
    await cache.fetchQuery(useBrandSettings());
    assert.equal(requests.length, 4);
    projectId = 'project-2';
    await cache.fetchQuery(useApiKeyCreatorOptions());
    assert.deepEqual(requests[4].headers, { 'X-Project-ID': 'project-2' });
  } finally {
    cache.clear();
  }
});
