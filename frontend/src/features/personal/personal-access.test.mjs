import React from 'react';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import { createStore } from 'zustand/vanilla';

const require = createRequire(import.meta.url);
function load(relativePath, dependencies) {
  const path = new URL(relativePath, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(path, 'utf8'), {
    fileName: path.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2023, esModuleInterop: true },
  });
  const exports = {};
  new Function('require', 'exports', outputText)((name) => (name in dependencies ? dependencies[name] : require(name)), exports);
  return exports;
}
const t = (key, args) => (args?.model ? 'Copy ' + args.model : key);
const badge = { Badge: ({ children }) => React.createElement('span', null, children) };
const icons = new Proxy({}, { get: () => () => null });
const columnDependencies = {
  '@tabler/icons-react': icons,
  '@lobehub/icons': {},
  'react-i18next': { useTranslation: () => ({ t }) },
  '@/hooks/usePermissions': { usePermissions: () => ({ channelPermissions: { canWrite: true } }) },
  '@/components/ui/badge': badge,
  '@/components/ui/button': { Button: 'button' },
  '@/components/ui/checkbox': { Checkbox: 'input' },
  '@/components/ui/switch': { Switch: () => React.createElement('span', { 'data-mutating-switch': true }) },
  '@/components/ui/tooltip': { Tooltip: React.Fragment, TooltipContent: 'span', TooltipTrigger: 'span' },
  '@/components/data-table-column-header': { DataTableColumnHeader: ({ title }) => title },
  '../context/models-context': { useModels: () => ({}) },
  './data-table-row-actions': { DataTableRowActions: () => null },
  './models-status-dialog': { ModelsStatusDialog: () => null },
  './models-table': { useDeveloperLabel: () => (value) => value },
  './model-id-copy-cell': { ModelIDCopyCell: ({ value }) => React.createElement('button', { 'data-copy': value }, value) },
};
const { createColumns } = load('../models/components/models-columns.tsx', columnDependencies);

test('personal columns omit associations and mutations even for an administrator viewing their own data', () => {
  const columns = createColumns(t, false, { showAssociations: false, readOnly: true, copyModelID: true });
  for (const id of ['select', 'actions', 'associationRules', 'associatedChannels'])
    assert.equal(
      columns.some((column) => column.id === id),
      false
    );
  const status = columns.find((column) => column.accessorKey === 'status').cell;
  const html = renderToStaticMarkup(React.createElement(status, { row: { original: { status: 'enabled' } } }));
  assert.match(html, /personal.status.enabled/);
  assert.doesNotMatch(html, /data-mutating-switch/);
  const modelID = columns.find((column) => column.accessorKey === 'modelID').cell;
  assert.match(renderToStaticMarkup(React.createElement(modelID, { row: { original: { modelID: 'my-model' } } })), /data-copy="my-model"/);
});

test('administrator defaults retain existing management columns and status switch', () => {
  const columns = createColumns(t);
  for (const id of ['select', 'actions', 'associationRules', 'associatedChannels'])
    assert.equal(
      columns.some((column) => column.id === id),
      true
    );
  const status = columns.find((column) => column.accessorKey === 'status').cell;
  assert.match(renderToStaticMarkup(React.createElement(status, { row: { original: { status: 'enabled' } } })), /data-mutating-switch/);
});

test('the shared table does not render developer rules or request settings in personal mode', () => {
  let settingsOptions;
  const table = load('../models/components/models-table.tsx', {
    '@tabler/icons-react': icons,
    'framer-motion': { motion: { create: (component) => component }, AnimatePresence: React.Fragment },
    'react-i18next': { useTranslation: () => ({ t, i18n: { exists: () => false } }) },
    '@/components/ui/badge': badge,
    '@/components/ui/button': { Button: ({ children }) => React.createElement('button', null, children) },
    '@/components/ui/input': { Input: ({ value, onChange }) => React.createElement('input', { value, onChange }) },
    '@/components/ui/table': { Table: 'table', TableBody: 'tbody', TableCell: 'td', TableHead: 'th', TableHeader: 'thead', TableRow: 'tr' },
    '@/components/ui/table-skeleton': { TableSkeleton: () => null },
    '@/components/permission-guard': { PermissionGuard: () => null },
    '@/features/system/data/system': {
      useModelSettings: (options) => {
        settingsOptions = options;
        return { data: { developerSettings: [{ developer: 'Example', associations: [{ secret: true }] }] } };
      },
    },
    '../context/models-context': {
      useModels: () => ({ setSelectedModels() {}, setResetRowSelection() {}, setOpen() {}, setCurrentDeveloper() {} }),
    },
  });
  const html = renderToStaticMarkup(
    React.createElement(table.ModelsTable, {
      columns: [{ accessorKey: 'name', header: 'Name' }],
      data: [{ id: 'model', name: 'Example model', developer: 'Example' }],
      nameFilter: '',
      sorting: [],
      onSortingChange() {},
      onNameFilterChange() {},
      canWrite: false,
      showAssociations: false,
    })
  );
  assert.deepEqual(settingsOptions, { enabled: false });
  assert.match(html, /Example model/);
  assert.doesNotMatch(html, /manageDeveloperAssociation|secret/);
});

test('switching accounts immediately resets the personal project and key selection', () => {
  let account = 'alice-session';
  const { usePersonalScope } = load('../../stores/personalWorkspaceStore.ts', {
    './authStore': { useAuthStore: (select) => select({ auth: { accessToken: account } }) },
    // Read the live store here; React SSR intentionally reads the initial snapshot.
    zustand: {
      create: (initializer) => {
        const store = createStore(initializer);
        return () => store.getState();
      },
    },
  });
  let scope;
  function ReadScope() {
    scope = usePersonalScope();
    return null;
  }
  const read = () => renderToStaticMarkup(React.createElement(ReadScope));
  read();
  scope.setProjectId('project-a');
  read();
  scope.setAPIKeyId('key-a');
  read();
  assert.equal(scope.apiKeyId, 'key-a');
  account = 'bob-session';
  read();
  assert.equal(scope.projectId, null);
  assert.equal(scope.apiKeyId, null);
  scope.setProjectId('project-b');
  read();
  assert.equal(scope.projectId, 'project-b');
  assert.equal(scope.apiKeyId, null);
});

test('model ID click copies the exact ID without toggling the model row', async () => {
  let text,
    click,
    copied = false,
    stopped = false;
  const { ModelIDCopyCell } = load('../models/components/model-id-copy-cell.tsx', {
    'lucide-react': { Check: () => null, Copy: () => null },
    'react-i18next': { useTranslation: () => ({ t }) },
    '@/hooks/use-copy-to-clipboard': {
      useCopyToClipboard: (options) => {
        text = options.text;
        return {
          isCopied: false,
          handleCopy: async () => {
            copied = true;
          },
        };
      },
    },
    '@/components/ui/button': {
      Button: ({ onClick, children }) => {
        click = onClick;
        return React.createElement('button', null, children);
      },
    },
  });
  renderToStaticMarkup(React.createElement(ModelIDCopyCell, { value: 'model/with-alias' }));
  await click({
    stopPropagation() {
      stopped = true;
    },
  });
  assert.equal(text, 'model/with-alias');
  assert.equal(copied, true);
  assert.equal(stopped, true);
});

test('personal queries keep account and scope boundaries and parse weekly quotas and nullable model metadata', async () => {
  let token = 'alice-session';
  let scope = { projectId: 'project-a', apiKeyId: 'key-a' };
  const calls = [];
  const modelSchema = load('../models/data/schema.ts', { '@/gql/pagination': { pageInfoSchema: require('zod').z.any() } });
  const key = {
    id: 'key-a',
    name: 'Personal',
    projectId: 'project-a',
    projectName: 'Project',
    status: 'enabled',
    deleted: false,
    activeProfile: 'weekly',
  };
  const metrics = {
    requests: 1,
    successfulRequests: 1,
    failedRequests: 0,
    canceledRequests: 0,
    pendingRequests: 0,
    inputTokens: 3_000_000_000,
    outputTokens: 0,
    cachedTokens: 0,
    totalTokens: 3_000_000_000,
    cost: 1.25,
    unpricedUsageCount: 0,
    successRate: 100,
  };
  const data = load('./data.ts', {
    '@tanstack/react-query': { useQuery: (options) => options },
    '@/stores/authStore': { useAuthStore: (select) => select({ auth: { accessToken: token } }) },
    '@/stores/personalWorkspaceStore': { usePersonalScope: () => scope },
    '@/features/models/data/schema': modelSchema,
    '@/gql/graphql': {
      graphqlRequest: async (query, variables, headers, options) => {
        calls.push({ variables, headers, options });
        if (query.includes('query MyModels'))
          return {
            myModels: [
              {
                id: 'raw-model',
                modelId: 'raw-model',
                name: 'Raw model',
                developer: '',
                icon: '',
                group: '',
                type: null,
                status: 'enabled',
                createdAt: '2026-09-10T00:00:00Z',
                updatedAt: '2026-09-10T00:00:00Z',
                modelCard: null,
                apiKeys: [key],
              },
            ],
          };
        return {
          myDashboard: {
            timezone: 'Asia/Kathmandu',
            overview: metrics,
            daily: [],
            models: [],
            apiKeys: [
              {
                apiKey: key,
                metrics,
                quota: {
                  profileName: 'weekly',
                  start: '2026-09-06T18:15:00Z',
                  end: '2026-09-13T18:15:00Z',
                  requests: 1,
                  tokens: 3_000_000_000,
                  cost: 1.25,
                  limit: {
                    requests: null,
                    totalTokens: 5_000_000_000,
                    cost: '2.5',
                    period: { type: 'calendar_duration', pastDuration: null, calendarDuration: { unit: 'week' } },
                  },
                },
              },
            ],
          },
        };
      },
    },
  });
  const dashboard = data.usePersonalDashboard('2026-09-10', '2026-09-11', true);
  const signal = new AbortController().signal;
  const result = await dashboard.queryFn({ signal });
  assert.equal(result.overview.totalTokens, 3_000_000_000);
  assert.equal(result.apiKeys[0].quota.limit.period.calendarDuration.unit, 'week');
  assert.equal(result.apiKeys[0].quota.limit.cost, 2.5);
  assert.deepEqual(calls[0].variables, { input: { ...scope, startDate: '2026-09-10', endDate: '2026-09-11' } });
  assert.equal(calls[0].headers, undefined);
  assert.equal(calls[0].options.signal, signal);
  assert.equal(dashboard.placeholderData, undefined);
  assert.equal((await data.usePersonalModels('').queryFn({ signal }))[0].modelCard, null);
  token = 'bob-session';
  scope = { projectId: null, apiKeyId: null };
  const next = data.usePersonalDashboard('2026-09-10', '2026-09-11', true);
  assert.notDeepEqual(next.queryKey, dashboard.queryKey);
  assert.equal(next.placeholderData, undefined);
});
