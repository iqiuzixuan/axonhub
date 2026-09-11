import React from 'react';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test, { beforeEach } from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import * as routePermissions from '../config/route-permission.ts';
import * as navigationPermissions from '../lib/navigation-permissions.ts';

const require = createRequire(import.meta.url);
let selectedProjectId;
let authUser;
let meData;
let pathname;

// Render the real hook and guard with only their external data/UI dependencies replaced.
function loadComponent(relativePath, dependencies) {
  const fileName = new URL(relativePath, import.meta.url);
  const { outputText } = ts.transpileModule(readFileSync(fileName, 'utf8'), {
    fileName: fileName.pathname,
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX },
  });
  const exports = {};
  const resolve = (name) => (Object.hasOwn(dependencies, name) ? dependencies[name] : require(name));
  new Function('require', 'exports', outputText)(resolve, exports);
  return exports;
}

const { useRoutePermissions } = loadComponent('./useRoutePermissions.ts', {
  '@/config/route-permission': routePermissions,
  '@/lib/navigation-permissions': navigationPermissions,
  '@/stores/authStore': { useAuthStore: (select) => select({ auth: { user: authUser } }) },
  '@/stores/projectStore': { useSelectedProjectId: () => selectedProjectId },
  '@/features/auth/data/auth': { useMe: () => ({ data: meData }) },
});

const { RouteGuard } = loadComponent('../components/route-guard.tsx', {
  '@tanstack/react-router': {
    useRouter: () => ({ navigate() {} }),
    useLocation: ({ select }) => select({ pathname }),
  },
  '@tabler/icons-react': { IconShieldX: () => null, IconArrowLeft: () => null },
  'react-i18next': { useTranslation: () => ({ t: (key) => key }) },
  '@/hooks/useRoutePermissions': { useRoutePermissions },
  '@/components/ui/alert': { Alert: 'div', AlertDescription: 'p', AlertTitle: 'h2' },
  '@/components/ui/button': { Button: 'button' },
});

beforeEach(() => {
  selectedProjectId = 'project-a';
  pathname = '/project/users/';
  meData = undefined;
  authUser = {
    scopes: [],
    isOwner: false,
    projects: [
      { projectID: 'project-a', scopes: ['read_users'], effectiveScopes: ['read_users'], isOwner: false },
      { projectID: 'project-b', scopes: [], effectiveScopes: ['read_users', 'read_projects'], isOwner: false },
    ],
  };
});

function readMenu() {
  let groups;
  function Menu() {
    groups = useRoutePermissions().filterNavGroups([{ title: '项目', items: [{ title: '用户', url: '/project/users' }] }]);
    return null;
  }
  renderToStaticMarkup(React.createElement(Menu));
  return groups;
}

test('the hook uses only the selected project, including permissions granted by its roles', () => {
  assert.deepEqual(readMenu(), []);
  selectedProjectId = 'project-b';
  assert.equal(readMenu()[0].items[0].url, '/project/users');
  selectedProjectId = 'project-a';
  assert.deepEqual(readMenu(), []);
  selectedProjectId = null;
  assert.deepEqual(readMenu(), []);
});

test('fresh user data hides navigation after permissions are revoked', () => {
  selectedProjectId = 'project-b';
  assert.equal(readMenu().length, 1);
  meData = { ...authUser, projects: [{ projectID: 'project-b', scopes: ['read_users'], effectiveScopes: [] }] };
  assert.deepEqual(readMenu(), []);
});

test('direct navigation cannot mount a project users page whose query would be denied', () => {
  let pageMounts = 0;
  function ProjectUsersPage() {
    pageMounts += 1;
    return React.createElement('div', null, 'project-users-loaded');
  }
  const page = () =>
    renderToStaticMarkup(
      React.createElement(RouteGuard, { requiredScopes: ['read_users'], scopeLevel: 'any' }, React.createElement(ProjectUsersPage))
    );

  assert.match(page(), /common.routeGuard.accessDenied/);
  assert.equal(pageMounts, 0, 'A blocked page must not mount or trigger its data queries');
  selectedProjectId = 'project-b';
  assert.match(page(), /project-users-loaded/);
  assert.equal(pageMounts, 1);
});

test('project owners see the menu and can open its page even without explicit scopes', () => {
  authUser.projects[0] = { projectID: 'project-a', scopes: [], effectiveScopes: [], isOwner: true };
  assert.equal(readMenu().length, 1);
  assert.match(
    renderToStaticMarkup(React.createElement(RouteGuard, { requiredScopes: ['read_users'], scopeLevel: 'any' }, 'project-users-loaded')),
    /project-users-loaded/
  );
});
