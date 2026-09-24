import React from 'react';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import { formatModelLabel } from '../utils/model-label.ts';
import * as routePermissions from '../config/route-permission.ts';

const require = createRequire(import.meta.url);
let user;
let selectedProjectId = 'project-a';
let meData;
function load(relativePath, dependencies) {
  const file = new URL(relativePath, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(file, 'utf8'), {
    fileName: file.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX },
  });
  const exports = {};
  new Function('require', 'exports', outputText)((name) => dependencies[name] ?? require(name), exports);
  return exports;
}
const { useRequestPermissions } = load('./useRequestPermissions.ts', {
  '@/config/route-permission': routePermissions,
  '@/stores/authStore': { useAuthStore: (select) => select({ auth: { user } }) },
  '@/stores/projectStore': { useSelectedProjectId: () => selectedProjectId },
  '@/features/auth/data/auth': { useMe: () => ({ data: meData }) },
});
const ui = (tag) => ({ children }) => React.createElement(tag, null, children);
const { useRequestsColumns } = load('../features/requests/components/requests-columns.tsx', {
  'react-i18next': { useTranslation: () => ({ t: (key) => key, i18n: { language: 'en' } }) },
  '@/lib/utils': { extractNumberID: (id) => id.split('/').at(-1) },
  '@/utils/format-duration': {},
  '@/utils/model-label': { formatModelLabel },
  '@/hooks/use-pagination-search': { usePaginationSearch: () => ({ navigateWithSearch() {} }) },
  '@/hooks/usePermissions': { usePermissions: () => ({ hasSystemScope: () => false }) },
  '@/components/ui/badge': { Badge: ui('span') },
  '@/components/ui/button': { Button: ui('button') },
  '@/components/ui/tooltip': { Tooltip: ui('div'), TooltipTrigger: ui('div'), TooltipContent: ui('span') },
  '@/components/data-table-column-header': { DataTableColumnHeader: ui('div') },
  '@/features/system/data/system': { useGeneralSettings: () => ({}), useSecuritySettings: () => ({}), useUpdateSecuritySettings: () => ({}) },
  '../../../hooks/useRequestPermissions': { useRequestPermissions },
  '../utils/tokens-per-second': {},
  '../utils/upstream-model-audit': { getUpstreamModelAudit: () => ({ status: 'matched' }), getRequestModelAuditTooltip: () => '' },
  './help': { getStatusColor: () => '' },
});
function permissions(projectId) {
  let result;
  function Probe() { result = useRequestPermissions(projectId); return null; }
  renderToStaticMarkup(React.createElement(Probe));
  return result;
}
function renderColumns() {
  let columns;
  function Probe() {
    columns = useRequestsColumns();
    const IdCell = columns.find((column) => column.accessorKey === 'id').cell;
    return React.createElement(IdCell, { row: { original: { id: 'gid://axonhub/Request/42', status: 'completed', stream: false }, index: 0 } });
  }
  return { html: renderToStaticMarkup(React.createElement(Probe)), get columns() { return columns; } };
}

test('ordinary readers and wildcard scopes cannot expose details or ID preview buttons', () => {
  for (const scopes of [['read_requests', 'write_requests'], ['*']]) {
    user = { scopes, projects: [{ projectID: 'project-a', scopes, isOwner: false }] };
    meData = undefined;
    assert.equal(permissions().canViewDetails, false);
    const result = renderColumns();
    assert.ok(!result.columns.some((column) => column.id === 'details'));
    assert.ok(!result.html.includes('<button'));
    assert.ok(result.html.includes('#42'));
  }
});

test('system owners retain detail buttons globally and in every project', () => {
  user = { isOwner: true, projects: [] };
  meData = undefined;
  assert.equal(permissions(null).canViewDetails, true);
  assert.equal(permissions('project-b').canViewDetails, true);
  const result = renderColumns();
  assert.ok(result.columns.some((column) => column.id === 'details'));
  assert.ok(result.html.includes('<button'));
});

test('project owners cannot borrow the selected project on global or other project pages', () => {
  user = { scopes: ['read_requests'], projects: [{ projectID: 'project-a', isOwner: true }] };
  meData = undefined;
  assert.equal(permissions().canViewDetails, true);
  assert.equal(permissions('project-b').canViewDetails, false);
  assert.equal(permissions(null).canViewDetails, false);
  selectedProjectId = 'project-b';
  assert.equal(permissions().canViewDetails, false);
  selectedProjectId = 'project-a';
});

test('refreshed membership revocation hides existing detail entry points', () => {
  user = { projects: [{ projectID: 'project-a', isOwner: true }] };
  meData = { projects: [{ projectID: 'project-a', isOwner: false }] };
  assert.equal(permissions().canViewDetails, false);
  assert.ok(!renderColumns().columns.some((column) => column.id === 'details'));
  meData = undefined;
});


test('project Admin scopes enable only that project; system administration enables every project', () => {
  const adminScopes = ['read_requests', 'write_users', 'write_roles'];
  user = { scopes: [], projects: [{ projectID: 'project-a', effectiveScopes: adminScopes }] };
  meData = undefined;
  assert.equal(permissions().canViewDetails, true);
  assert.equal(permissions('project-b').canViewDetails, false);
  assert.equal(permissions(null).canViewDetails, false);
  assert.ok(renderColumns().columns.some((column) => column.id === 'details'));
  user = { scopes: adminScopes, projects: [] };
  assert.equal(permissions(null).canViewDetails, true);
  assert.equal(permissions('project-b').canViewDetails, true);
  user = { scopes: ['write_users'], projects: [{ projectID: 'project-a', effectiveScopes: ['write_roles', 'read_requests'] }] };
  assert.equal(permissions().canViewDetails, false);
});

function renderModelCell(modelID, requestedModelID, actualModel) {
  function Probe() {
    const columns = useRequestsColumns();
    const ModelCell = columns.find((column) => column.id === 'modelID').cell;
    return React.createElement(ModelCell, { row: { original: {
      modelID, requestedModelID, format: 'openai/chat_completions',
      executions: { edges: [{ node: { modelID: actualModel, format: 'openai/chat_completions' } }] },
    } } });
  }
  return renderToStaticMarkup(React.createElement(Probe));
}

test('ordinary users never render the execution-model tooltip, even with stale privileged data', () => {
  user = { scopes: ['read_requests'], projects: [{ projectID: 'project-a', scopes: ['read_requests'], isOwner: false }] };
  meData = undefined;
  const html = renderModelCell('public-A', 'public-A', 'secret-C');
  assert.ok(html.includes('public-A'));
  assert.ok(!html.includes('secret-C'));
  assert.ok(!html.includes('billing.routing'));
});

test('administrators can inspect mapping while the main model follows either policy', () => {
  user = { isOwner: true, projects: [] };
  meData = undefined;
  for (const displayed of ['public-A', 'secret-C']) {
    const html = renderModelCell(displayed, 'public-A', 'secret-C');
    assert.ok(html.includes('public-A'));
    assert.ok(html.includes('secret-C'));
    assert.ok(html.includes('billing.routing'));
  }
});

test('missing original models show the localized label without revealing execution models', () => {
  user = { scopes: ['read_requests'], projects: [{ projectID: 'project-a', scopes: ['read_requests'], isOwner: false }] };
  meData = undefined;
  const html = renderModelCell('[original model unavailable]', undefined, 'secret-C');
  assert.ok(html.includes('billing.originalModelNotRecorded'));
  assert.ok(!html.includes('[original model unavailable]'));
  assert.ok(!html.includes('secret-C'));
  assert.ok(!html.includes('billing.routing'));
});
