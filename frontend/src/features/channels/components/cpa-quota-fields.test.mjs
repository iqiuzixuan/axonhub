import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import * as hookForm from 'react-hook-form';
import { Slot } from '@radix-ui/react-slot';
import * as RadixSelect from '@radix-ui/react-select';
import ts from 'typescript';

const locale = JSON.parse(readFileSync(new URL('../../../locales/zh-CN/channels.json', import.meta.url), 'utf8'));
const Label = (props) => React.createElement('label', props);
globalThis.__cpaFormImports = { React, Slot, Label, ...hookForm, cn: (...values) => values.filter(Boolean).join(' ') };
function load(source, prelude) {
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.React } });
  return import(`data:text/javascript;base64,${Buffer.from(prelude + outputText).toString('base64')}`);
}
// Keep the real form contexts, controllers, labels and accessibility bindings.
const formSource = readFileSync(new URL('../../../components/ui/form.tsx', import.meta.url), 'utf8').replace(/^import[\s\S]*?;\n/gm, '');
const form = await load(formSource, 'const { React, Slot, Label, Controller, FormProvider, useFormContext, useFormState, cn } = globalThis.__cpaFormImports;\n');
const Input = (props) => React.createElement('input', props);
globalThis.__cpaFixtureImports = { React, ...form, useForm: hookForm.useForm, Input,
  Select: RadixSelect.Root, SelectTrigger: RadixSelect.Trigger, SelectValue: RadixSelect.Value,
  SelectContent: RadixSelect.Content, SelectItem: RadixSelect.Item, t: (key) => locale[key] ?? key };
const dialog = readFileSync(new URL('./channels-action-dialog.tsx', import.meta.url), 'utf8');
const start = dialog.indexOf("{isCodexType && authMode === 'third-party' && (\n");
const end = dialog.indexOf('{isZenmuxType && (', start);
assert.ok(start >= 0 && end > start);
const region = dialog.slice(start, end);
const { Fixture } = await load(`export function Fixture({ cpa = null, codex = true, thirdParty = true }) {
  const form = useForm({ defaultValues: { settings: { providerQuota: { cpa } }, credentials: { managementApiKey: '' } } });
  const isCodexType = codex;
  const authMode = thirdParty ? 'third-party' : 'official';
  const isCPAType = isCodexType && thirdParty && !!form.watch('settings.providerQuota.cpa');
  const isEdit = true, isDuplicate = false;
  return <Form {...form}><div>${region}</div></Form>;
}`, 'const { React, Form, FormField, FormItem, FormLabel, FormControl, FormDescription, FormMessage, useForm, Input, Select, SelectTrigger, SelectValue, SelectContent, SelectItem, t } = globalThis.__cpaFixtureImports;\n');

test('CPA fields render saved endpoint, compatibility and account with accessible labels and no saved key', () => {
  const markup = renderToStaticMarkup(React.createElement(Fixture, { cpa: { managementURL: 'http://cpa.example/prefix', apiVersion: 'V0', authIndex: 'account-one' } }));
  assert.match(markup, /CPA 管理地址/);
  assert.match(markup, /value="http:\/\/cpa.example\/prefix"/);
  assert.match(markup, /value="account-one"/);
  assert.match(markup, /type="password"/);
  assert.match(markup, /留空保留已保存的管理密钥/);
  assert.doesNotMatch(markup, /undefined-form-item/);
  const labels = [...markup.matchAll(/for="([^"]+)"/g)].map((match) => match[1]);
  assert.equal(labels.length, 5);
  for (const id of labels) assert.ok(markup.includes(`id="${id}"`), `label ${id} has a real field`);
});

test('ordinary third-party stays unconfigured and CPA fields only appear for third-party Codex', () => {
  const ordinary = renderToStaticMarkup(React.createElement(Fixture));
  assert.match(ordinary, /第三方类型/);
  assert.doesNotMatch(ordinary, /CPA 管理地址|type="password"/);
  for (const props of [{ codex: false }, { thirdParty: false }]) {
    const markup = renderToStaticMarkup(React.createElement(Fixture, { ...props, cpa: { managementURL: 'http://cpa.example' } }));
    assert.doesNotMatch(markup, /第三方类型|CPA 管理地址/);
  }
});
