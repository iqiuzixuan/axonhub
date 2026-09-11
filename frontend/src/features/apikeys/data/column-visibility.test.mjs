import React from 'react';
import { createTable, getCoreRowModel } from '@tanstack/react-table';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test, { beforeEach } from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const require = createRequire(import.meta.url);
function load(path, dependencies = {}) {
  const file = new URL(path, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(file, 'utf8'), {
    fileName: file.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.ReactJSX },
  });
  const exports = {};
  new Function('require', 'exports', outputText)((name) => dependencies[name] ?? require(name), exports);
  return exports;
}
const visibility = load('./column-visibility.ts');
let storage;
beforeEach(() => {
  storage = new Map();
  globalThis.localStorage = { getItem: (key) => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value) };
});

test('admin quota starts hidden, members see it, and explicit choices survive refresh', () => {
  assert.equal(visibility.resolveApiKeyColumnVisibility(true, visibility.loadApiKeyColumnOverrides('admin')).quota, false);
  assert.equal(visibility.resolveApiKeyColumnVisibility(false, visibility.loadApiKeyColumnOverrides('member')).quota, true);
  visibility.saveApiKeyColumnOverrides('admin', { quota: true });
  assert.equal(visibility.resolveApiKeyColumnVisibility(true, visibility.loadApiKeyColumnOverrides('admin')).quota, true);
  visibility.saveApiKeyColumnOverrides('member', { quota: false });
  assert.equal(visibility.resolveApiKeyColumnVisibility(false, visibility.loadApiKeyColumnOverrides('member')).quota, false);
});

test('only user changes are persisted; defaults do not leak across permission changes or accounts', () => {
  const initial = visibility.resolveApiKeyColumnVisibility(true, {});
  const overrides = visibility.updateApiKeyColumnOverrides(initial, { ...initial, key: false }, {});
  assert.deepEqual(overrides, { key: false });
  visibility.saveApiKeyColumnOverrides('admin', overrides);
  assert.equal(visibility.resolveApiKeyColumnVisibility(false, visibility.loadApiKeyColumnOverrides('admin')).quota, true);
  assert.deepEqual(visibility.loadApiKeyColumnOverrides('another-user'), {});
  assert.deepEqual(visibility.loadApiKeyColumnOverrides(), {});
});

test('corrupt, outdated, or unknown stored fields cannot hide required columns', () => {
  const key = 'apikeys-table-column-visibility:user';
  for (const raw of ['invalid', 'null', '[]', '{"v":2,"overrides":{"quota":true}}']) {
    storage.set(key, raw);
    assert.deepEqual(visibility.loadApiKeyColumnOverrides('user'), {});
  }
  storage.set(
    key,
    JSON.stringify({
      v: 1,
      overrides: { quota: true, key: false, name: false, select: false, actions: false, removed: true, status: 'false' },
    })
  );
  assert.deepEqual(visibility.loadApiKeyColumnOverrides('user'), { quota: true, key: false });
  globalThis.localStorage = {
    getItem() {
      throw new Error('blocked');
    },
    setItem() {
      throw new Error('blocked');
    },
  };
  assert.deepEqual(visibility.loadApiKeyColumnOverrides('user'), {});
  assert.doesNotThrow(() => visibility.saveApiKeyColumnOverrides('user', { quota: true }));
});

test('account changes immediately resolve that account and preserve its later checkbox choice', () => {
  visibility.saveApiKeyColumnOverrides('first', { quota: true });
  let state;
  const hooks = load('../hooks/use-column-visibility.ts', {
    react: {
      useState: (initial) => {
        state ??= initial();
        return [
          state,
          (next) => {
            state = next;
          },
        ];
      },
      useMemo: (fn) => fn(),
      useCallback: (fn) => fn,
    },
    '../data/column-visibility': visibility,
  });
  assert.equal(hooks.useApiKeyColumnVisibility('first', true).columnVisibility.quota, true);
  const second = hooks.useApiKeyColumnVisibility('second', true);
  assert.equal(second.columnVisibility.quota, false);
  second.onColumnVisibilityChange((previous) => ({ ...previous, quota: true }));
  assert.equal(hooks.useApiKeyColumnVisibility('second', true).columnVisibility.quota, true);
  assert.deepEqual(visibility.loadApiKeyColumnOverrides('second'), { quota: true });
});

test('column settings include the display-only quota column and toggle real table visibility', () => {
  const translations = JSON.parse(readFileSync(new URL('../../../locales/zh-CN/apikeys.json', import.meta.url), 'utf8'));
  const handlers = new Map();
  const pass = ({ children }) => React.createElement(React.Fragment, null, children);
  const { DataTableViewOptions } = load('../components/data-table-view-options.tsx', {
    '@radix-ui/react-icons': { MixerHorizontalIcon: () => null },
    'react-i18next': { useTranslation: () => ({ t: (key) => translations[key] ?? key }) },
    '@/components/ui/button': { Button: pass },
    '@/components/ui/dropdown-menu': {
      DropdownMenu: pass,
      DropdownMenuTrigger: pass,
      DropdownMenuContent: pass,
      DropdownMenuLabel: pass,
      DropdownMenuSeparator: () => null,
      DropdownMenuCheckboxItem: ({ children, checked, onCheckedChange }) => {
        handlers.set(children, onCheckedChange);
        return React.createElement('button', { role: 'menuitemcheckbox', 'aria-checked': checked }, children);
      },
    },
    '../data/column-visibility': visibility,
  });
  const table = createTable({
    columns: [
      { accessorKey: 'name', enableHiding: false },
      { id: 'quota' },
      { accessorKey: 'key' },
      { id: 'actions', enableHiding: false },
    ],
    data: [],
    state: { columnVisibility: { quota: false }, columnPinning: {} },
    getCoreRowModel: getCoreRowModel(),
    onStateChange() {},
    renderFallbackValue: null,
    onColumnVisibilityChange: (updater) =>
      table.setOptions((previous) => ({
        ...previous,
        state: { ...previous.state, columnVisibility: typeof updater === 'function' ? updater(previous.state.columnVisibility) : updater },
      })),
  });
  const html = renderToStaticMarkup(React.createElement(DataTableViewOptions, { table }));
  assert.match(html, /aria-checked="false">剩余额度/);
  assert.deepEqual([...handlers.keys()], ['剩余额度', 'API Key']);
  assert.equal(table.getVisibleLeafColumns().length, 3);
  handlers.get('剩余额度')(true);
  assert.equal(table.getColumn('quota').getIsVisible(), true);
  assert.equal(table.getVisibleLeafColumns().length, 4);
  handlers.get('剩余额度')(false);
  assert.equal(table.getColumn('quota').getIsVisible(), false);
});
