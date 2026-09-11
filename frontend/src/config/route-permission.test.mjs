import assert from 'node:assert/strict';
import test from 'node:test';
import { filterNavGroups } from '../lib/navigation-permissions.ts';
import { getRouteConfig, hasGroupAccess, hasRouteAccess, routeConfigs } from './route-permission.ts';

const noPermissions = {
  systemScopes: [],
  projectScopes: [],
  isOwner: false,
  isProjectOwner: false,
};

function canAccess(permissions, path) {
  const route = getRouteConfig(path);
  assert.ok(route, `Missing route configuration for ${path}`);
  return hasRouteAccess({ ...noPermissions, ...permissions }, route);
}

test('project users requires both project and member read access', () => {
  for (const projectScopes of [[], ['read_users'], ['read_projects']]) {
    assert.equal(canAccess({ projectScopes }, '/project/users'), false);
  }
  assert.equal(canAccess({ projectScopes: ['read_projects', 'read_users'] }, '/project/users'), true);
  assert.equal(canAccess({ systemScopes: ['read_projects'], projectScopes: ['read_users'] }, '/project/users'), true);
  assert.equal(canAccess({ systemScopes: ['read_users'], projectScopes: ['read_projects'] }, '/project/users'), true);
});

test('a user with only member read access no longer sees the failing project users entry', () => {
  const groups = [{ title: '项目', items: [{ title: '用户', url: '/project/users' }] }];
  assert.deepEqual(
    filterNavGroups(groups, (path) => canAccess({ projectScopes: ['read_users'] }, path)),
    []
  );
  assert.deepEqual(
    filterNavGroups(groups, (path) => canAccess({ projectScopes: ['read_projects', 'read_users'] }, path)),
    groups
  );
});

test('system and project scopes stay separate for admin navigation', () => {
  assert.equal(canAccess({ projectScopes: ['read_users', 'read_channels'] }, '/users'), false);
  assert.equal(canAccess({ projectScopes: ['read_channels'] }, '/channels'), false);
  assert.equal(canAccess({ systemScopes: ['read_users'] }, '/users'), true);
  assert.equal(canAccess({ systemScopes: ['read_channels'] }, '/channels'), true);
});

test('project owners can open project pages without receiving admin access', () => {
  const owner = { isProjectOwner: true };
  for (const route of routeConfigs.find((group) => group.title === 'Project').routes) {
    assert.equal(canAccess(owner, route.path), true, route.path);
  }
  for (const path of ['/', '/users', '/roles', '/channels', '/projects', '/system']) {
    assert.equal(canAccess(owner, path), false, path);
  }
});

test('system owners retain access to all navigation routes', () => {
  for (const group of routeConfigs) {
    for (const route of group.routes) {
      assert.equal(canAccess({ isOwner: true }, route.path), true, route.path);
    }
  }
});

test('scope wildcards respect scope level and do not grant ownership', () => {
  assert.equal(canAccess({ systemScopes: ['*'] }, '/project/users'), true);
  assert.equal(canAccess({ projectScopes: ['*'] }, '/project/users'), true);
  assert.equal(canAccess({ projectScopes: ['*'] }, '/users'), false);
  assert.equal(canAccess({ systemScopes: ['*'] }, '/project/usage-stats'), false);
});

test('usage statistics remain limited to owners', () => {
  assert.equal(canAccess({ projectScopes: ['read_requests'] }, '/project/usage-stats'), false);
  assert.equal(canAccess({ isProjectOwner: true }, '/project/usage-stats'), true);
});

test('playground navigation follows the same scopes as its page guard', () => {
  assert.equal(canAccess({}, '/project/playground'), false);
  assert.equal(canAccess({ projectScopes: ['read_requests'] }, '/project/playground'), false);
  assert.equal(canAccess({ projectScopes: ['write_requests'] }, '/project/playground'), true);
  assert.equal(canAccess({ systemScopes: ['read_channels'] }, '/project/playground'), true);
});

test('route lookup preserves group scope and covers trailing slashes and detail pages', () => {
  assert.equal(getRouteConfig('/users/').scopeLevel, 'system');
  assert.equal(getRouteConfig('/project/users/?page=1#table').path, '/project/users');
  assert.equal(canAccess({ projectScopes: ['read_users'] }, '/project/users/'), false);
  assert.equal(getRouteConfig('/project/requests/request-123').path, '/project/requests');
  assert.equal(canAccess({}, '/project/requests/request-123'), false);
  assert.equal(canAccess({ projectScopes: ['read_requests'] }, '/project/requests/request-123'), true);
  assert.equal(getRouteConfig('/settings/profile').path, '/settings/profile');
  assert.equal(getRouteConfig('/users-other'), undefined);
});

test('group access evaluates the group scope level', () => {
  const admin = routeConfigs.find((group) => group.title === 'Admin');
  const restrictedAdmin = { ...admin, routes: admin.routes.filter((route) => route.requiredScopes?.length) };
  assert.equal(hasGroupAccess({ ...noPermissions, isProjectOwner: true }, restrictedAdmin), false);
  assert.equal(hasGroupAccess({ ...noPermissions, systemScopes: ['read_users'] }, restrictedAdmin), true);
});

test('navigation removes inaccessible children and empty groups regardless of translated titles', () => {
  const groups = [
    { title: '管理', items: [{ title: '用户', url: '/users' }] },
    {
      title: '项目',
      items: [
        {
          title: '成员管理',
          items: [
            { title: '用户', url: '/project/users' },
            { title: '角色', url: '/project/roles' },
          ],
        },
        { title: '请求', url: '/project/requests' },
        { title: '测试场', url: '/project/playground' },
      ],
    },
  ];
  const original = structuredClone(groups);
  const filtered = filterNavGroups(groups, (path) => canAccess({ projectScopes: ['read_roles', 'read_requests'] }, path));
  assert.deepEqual(filtered, [
    {
      title: '项目',
      items: [
        { title: '成员管理', items: [{ title: '角色', url: '/project/roles' }] },
        { title: '请求', url: '/project/requests' },
      ],
    },
  ]);
  assert.deepEqual(
    filterNavGroups(groups, (path) => canAccess({}, path)),
    []
  );
  assert.deepEqual(groups, original, 'Filtering must not modify the shared navigation configuration');
});

test('navigation follows the selected project permissions instead of another project', () => {
  const groups = [
    {
      title: '项目',
      items: [
        { title: '用户', url: '/project/users' },
        { title: '请求', url: '/project/requests' },
      ],
    },
  ];
  const firstProject = { projectScopes: ['read_projects', 'read_users', 'read_requests'] };
  const secondProject = { projectScopes: ['read_requests'] };
  assert.deepEqual(
    filterNavGroups(groups, (path) => canAccess(firstProject, path)),
    groups
  );
  assert.deepEqual(
    filterNavGroups(groups, (path) => canAccess(secondProject, path)),
    [
      {
        title: '项目',
        items: [{ title: '请求', url: '/project/requests' }],
      },
    ]
  );
});
