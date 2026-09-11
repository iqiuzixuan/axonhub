import assert from 'node:assert/strict';
import test from 'node:test';

async function loadStore(t, cachedUser) {
  const entries = new Map([
    ['axonhub_access_token', 'retained-test-token'],
    ['axonhub_user_info', cachedUser],
  ]);
  const previousStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key) => entries.get(key) ?? null,
      setItem: (key, value) => entries.set(key, value),
      removeItem: (key) => entries.delete(key),
    },
  });
  t.after(() => {
    if (previousStorage) {
      Object.defineProperty(globalThis, 'localStorage', previousStorage);
    } else {
      delete globalThis.localStorage;
    }
  });
  const { useAuthStore } = await import(`./authStore.ts?test=${encodeURIComponent(t.name)}`);
  return { auth: useAuthStore.getState().auth, entries };
}

test('legacy first/last-name browser cache is discarded while its login token survives', async (t) => {
  const { auth, entries } = await loadStore(t, JSON.stringify({ id: 'user-1', firstName: '三', lastName: '张' }));
  assert.equal(auth.user, null);
  assert.equal(entries.has('axonhub_user_info'), false);
  assert.equal(auth.accessToken, 'retained-test-token');
  assert.equal(entries.get('axonhub_access_token'), 'retained-test-token');
});

test('new browser cache preserves a complete name exactly as saved', async (t) => {
  const user = { id: 'user-1', name: '张 三 🌟', email: 'user@example.com' };
  const { auth, entries } = await loadStore(t, JSON.stringify(user));
  assert.deepEqual(auth.user, user);
  auth.setUser({ ...user, name: '新的昵称' });
  assert.equal(JSON.parse(entries.get('axonhub_user_info')).name, '新的昵称');
});

test('malformed browser cache does not prevent refreshing user data with a retained token', async (t) => {
  const { auth } = await loadStore(t, '{invalid json');
  assert.equal(auth.user, null);
  assert.equal(auth.accessToken, 'retained-test-token');
});
