import React from 'react';
import { QueryClient } from '@tanstack/react-query';
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
let locationPathname;
let navigations;
let effects;
let goBack;
let myProjects;
let projectsLoading;
let dashboardMounts;
let accessToken;

const router = { navigate: (options) => navigations.push(options) };

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
  react: { ...React, useEffect: (effect) => effects.push(effect) },
  '@tanstack/react-router': {
    useRouter: () => router,
    useLocation: ({ select }) => select({ pathname: locationPathname }),
    useMatch: ({ select }) => select({ pathname }),
  },
  '@tabler/icons-react': { IconShieldX: () => null, IconArrowLeft: () => null },
  'react-i18next': { useTranslation: () => ({ t: (key) => key }) },
  '@/hooks/useRoutePermissions': { useRoutePermissions },
  '@/components/ui/alert': { Alert: 'div', AlertDescription: 'p', AlertTitle: 'h2' },
  '@/components/ui/button': {
    Button: ({ onClick, children }) => {
      goBack = onClick;
      return React.createElement('button', null, children);
    },
  },
});

const { Route: homeRoute } = loadComponent('../routes/_authenticated/index.tsx', {
  '@tanstack/react-router': { createFileRoute: () => (options) => options },
  '@/components/route-guard': { RouteGuard },
  '@/stores/projectStore': { useSelectedProjectId: () => selectedProjectId },
  '@/hooks/useRoutePermissions': { useRoutePermissions },
  '@/features/projects/data/projects': { useMyProjects: () => ({ data: myProjects, isLoading: projectsLoading }) },
  '@/features/dashboard': {
    default: () => {
      dashboardMounts += 1;
      return React.createElement('div', null, 'dashboard-loaded');
    },
  },
});

const authStore = {
  useAuthStore: (select) =>
    select({
      auth: {
        user: authUser,
        accessToken,
        setUser: (user) => {
          authUser = user;
        },
        setAccessToken: (token) => {
          accessToken = token;
        },
      },
    }),
  setTokenToStorage() {},
  removeTokenFromStorage() {},
};
const { useSignIn, useOIDCExchange } = loadComponent('../features/auth/data/auth.ts', {
  '@tanstack/react-query': { useMutation: (options) => options },
  '@tanstack/react-router': { useRouter: () => router },
  '@/gql/graphql': {},
  '@/gql/users': {},
  sonner: { toast: { success() {} } },
  '@/stores/authStore': authStore,
  '@/stores/projectStore': {},
  '@/lib/project-membership': {},
  '@/lib/api-client': {},
  '@/lib/i18n': { default: { language: 'en', t: (key) => key } },
});

const { useMyProjects } = loadComponent('../features/projects/data/projects.ts', {
  '@tanstack/react-query': { useQuery: (options) => options },
  'react-i18next': { useTranslation: () => ({ t: (key) => key }) },
  '@/gql/graphql': { graphqlRequest: async () => ({ myProjects }) },
  sonner: {},
  '@/lib/i18n': {},
  '@/hooks/use-error-handler': { useErrorHandler: () => ({ handleError() {} }) },
  '@/stores/projectStore': {},
  '@/stores/authStore': authStore,
  './schema': { projectSchema: { parse: (project) => project } },
});

beforeEach(() => {
  selectedProjectId = 'project-a';
  pathname = '/project/users/';
  locationPathname = pathname;
  navigations = [];
  effects = [];
  goBack = undefined;
  myProjects = [{ id: 'project-a' }, { id: 'project-b' }];
  projectsLoading = false;
  dashboardMounts = 0;
  accessToken = 'login-test-token';
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

test('switching accounts cannot reuse the previous account active project list', async () => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  try {
    myProjects = [{ id: 'first-account-project' }];
    assert.deepEqual(await queryClient.fetchQuery(useMyProjects()), myProjects);
    accessToken = 'second-account-token';
    myProjects = [{ id: 'second-account-project' }];
    assert.deepEqual(await queryClient.fetchQuery(useMyProjects()), myProjects);
  } finally {
    queryClient.clear();
  }
});

function renderHome() {
  pathname = '/';
  const html = renderToStaticMarkup(React.createElement(homeRoute.component));
  effects.splice(0).forEach((effect) => effect());
  return html;
}

test('password and SSO login reach an accessible page through the shared home route', () => {
  for (const method of ['password', 'sso']) {
    authUser.projects[0].effectiveScopes = ['read_api_keys', 'read_requests'];
    const response = { token: 'login-test-token', user: authUser };
    const mutation = method === 'password' ? useSignIn() : useOIDCExchange();
    mutation.onSuccess(method === 'password' ? response : { data: response });
    assert.deepEqual(navigations.pop(), { to: '/', replace: true });
    assert.equal(renderHome(), '');
    assert.deepEqual(navigations.pop(), { to: '/project/api-keys', replace: true });
    assert.equal(dashboardMounts, 0);
  }
});

test('home waits for the active project selection instead of redirecting with empty scopes', () => {
  authUser.projects[0].effectiveScopes = ['read_requests'];
  selectedProjectId = null;
  projectsLoading = true;
  assert.equal(renderHome(), '');
  assert.deepEqual(navigations, []);
  projectsLoading = false;
  assert.equal(renderHome(), '');
  assert.deepEqual(navigations, []);
  selectedProjectId = 'project-a';
  renderHome();
  assert.deepEqual(navigations, [{ to: '/project/requests', replace: true }]);
});

test('a pending navigation cannot make the old home guard redirect again to the profile', () => {
  authUser.projects[0].effectiveScopes = ['read_api_keys', 'read_requests'];
  locationPathname = '/';
  renderHome();
  assert.deepEqual(navigations.pop(), { to: '/project/api-keys', replace: true });
  // The router updates its URL while the old home match is still mounted.
  locationPathname = '/project/api-keys';
  renderHome();
  assert.deepEqual(navigations, [{ to: '/project/api-keys', replace: true }]);
  assert.equal(dashboardMounts, 0);
});

test('home waits for a stale project to be cleared when there are no active projects', () => {
  myProjects = [];
  authUser.projects[0].effectiveScopes = ['read_requests'];
  renderHome();
  assert.deepEqual(navigations, []);
  selectedProjectId = null;
  renderHome();
  assert.deepEqual(navigations, [{ to: '/settings/profile', replace: true }]);
});

test('members without business scopes land on their profile and owners retain the dashboard', () => {
  authUser.projects = [];
  myProjects = [];
  selectedProjectId = null;
  assert.equal(renderHome(), '');
  assert.deepEqual(navigations.pop(), { to: '/settings/profile', replace: true });
  authUser.isOwner = true;
  assert.match(renderHome(), /dashboard-loaded/);
  assert.equal(dashboardMounts, 1);
  assert.deepEqual(navigations, []);
});

test('the denied playground returns to a permitted page even if its explicit fallback is also denied', () => {
  pathname = '/project/playground';
  authUser.projects[0].effectiveScopes = ['read_api_keys', 'read_requests'];
  const html = renderToStaticMarkup(
    React.createElement(
      RouteGuard,
      {
        requiredScopes: ['write_requests', 'read_channels'],
        scopeLevel: 'any',
        fallbackPath: '/',
      },
      'playground-loaded'
    )
  );
  assert.match(html, /common.routeGuard.accessDenied/);
  assert.doesNotMatch(html, /playground-loaded/);
  goBack();
  assert.deepEqual(navigations, [{ to: '/project/api-keys', replace: true }]);
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
