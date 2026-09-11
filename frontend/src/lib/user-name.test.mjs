import assert from 'node:assert/strict';
import test from 'node:test';
import { formatUserName, userNameInitials, userNameUpdate } from './utils.ts';
import { userNameSchema } from './validation.ts';

const nameSchema = userNameSchema((key) => key);

test('names preserve the entered order and internal spaces in validation and display', () => {
  for (const name of ['张三', '欧阳小明', '小秋', 'John Smith', 'Smith John', '张 三', '王  小明', '🌟 昵称']) {
    const savedName = nameSchema.parse(`  ${name}  `);
    assert.equal(savedName, name);
    assert.equal(formatUserName(savedName, 'user@example.com'), name);
  }
});

test('names cannot be empty or whitespace-only', () => {
  for (const name of ['', '   ', '\n\t', '\u3000']) {
    const parsed = nameSchema.safeParse(name);
    assert.equal(parsed.success, false);
    assert.equal(parsed.error.issues[0].message, 'users.validation.nameRequired');
  }
});

test('names accept 100 Unicode code points and reject 101, including supplementary characters', () => {
  for (const character of ['张', 'a', '🌟', '𠮷']) {
    const maximumName = character.repeat(100);
    assert.equal(nameSchema.parse(` ${maximumName} `), maximumName);
    const tooLong = nameSchema.safeParse(character.repeat(101));
    assert.equal(tooLong.success, false);
    assert.equal(tooLong.error.issues[0].message, 'users.validation.nameTooLong');
  }
});

test('empty legacy names fall back to the email without interpreting its order', () => {
  for (const name of ['', ' ', null, undefined]) {
    assert.equal(formatUserName(name, 'user@example.com'), 'user@example.com');
  }
  assert.equal(formatUserName(null, null), '');
});

test('editing permits an unchanged 101-character migrated name but never a new over-limit name', () => {
  const migratedName = `${'a'.repeat(50)} ${'b'.repeat(50)}`;
  const editSchema = userNameSchema((key) => key, migratedName);
  assert.equal(editSchema.parse(migratedName), migratedName);
  assert.equal(editSchema.safeParse(`${'c'.repeat(50)} ${'b'.repeat(50)}`).success, false);
  assert.equal(editSchema.safeParse(`${migratedName}c`).success, false);
  assert.equal(nameSchema.safeParse(migratedName).success, false);
  assert.equal(editSchema.parse('新的名称'), '新的名称');
  assert.equal(editSchema.safeParse('   ').success, false);
});

test('unchanged names are omitted from profile and user update inputs, including migrated long names', () => {
  for (const originalName of ['张三', `${'a'.repeat(50)} ${'b'.repeat(50)}`]) {
    const profileInput = { ...userNameUpdate(originalName, originalName), preferLanguage: 'zh' };
    const userInput = { ...userNameUpdate(originalName, originalName), scopes: ['read_users'] };
    assert.equal(Object.hasOwn(profileInput, 'name'), false);
    assert.equal(Object.hasOwn(userInput, 'name'), false);
    assert.deepEqual(userNameUpdate('新的名称', originalName), { name: '新的名称' });
  }
  assert.deepEqual(userNameUpdate('新用户'), { name: '新用户' });
});

test('avatar initials keep the name order without splitting supplementary characters', () => {
  assert.equal(userNameInitials('张三'), '张三');
  assert.equal(userNameInitials('John Smith'), 'JO');
  assert.equal(userNameInitials('🌟昵称'), '🌟昵');
  assert.equal(userNameInitials('𠮷野'), '𠮷野');
  assert.equal(userNameInitials('', 'test@example.com'), 'TE');
  assert.equal(userNameInitials(), 'U');
});
