import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';

const compile = (source) => ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023, jsx: ts.JsxEmit.React },
}).outputText;
const load = (source) => import(`data:text/javascript;base64,${Buffer.from(compile(source)).toString('base64')}`);
const pi = await load(readFileSync(new URL('../data/pi-models-config.ts', import.meta.url), 'utf8'));
const passthrough = ({ children }) => React.createElement(React.Fragment, null, children);
let detail;
let blocks;
const models = ['allowed', 'blocked'].map((modelID) => ({ node: {
  modelID, name: modelID, type: 'chat', developer: 'test',
  modelCard: { reasoning: { supported: true }, reasoningEfforts: ['low', 'high'] },
} }));
const mocks = {
  React, useState: React.useState, useEffect: React.useEffect, useMemo: React.useMemo,
  formatApiKeyLabel: (name) => name,
  useTranslation: () => ({ t: (key) => key }),
  useCopyToClipboard: () => ({ isCopied: false, handleCopy() {} }),
  useApiKeysContext: () => ({
    isDialogOpen: { view: true }, closeDialog() {},
    selectedApiKey: { id: 'key-1', name: 'test key', type: 'personal', key: 'ah-test-not-a-real-key' },
  }),
  useApiKey: (id) => { assert.equal(id, 'key-1'); return { data: detail }; },
  useQueryAllModels: () => ({ data: { edges: models } }),
  buildPiModelsConfig: pi.buildPiModelsConfig,
  serializePiModelsConfig: pi.serializePiModelsConfig,
  highlightMaskedCode: async () => ['', ''],
  MaskedCodeBlock: ({ language, realCode, displayCode, children }) => {
    blocks.push({ language, realCode, displayCode });
    return React.createElement('pre', null, displayCode, children);
  },
  TabsTrigger: ({ value, children }) => React.createElement('button', { 'data-tab': value }, children),
};
for (const name of ['Copy', 'Eye', 'EyeOff', 'AlertTriangle', 'Link', 'CheckIcon', 'Alert', 'AlertDescription',
  'Button', 'Dialog', 'DialogContent', 'DialogDescription', 'DialogHeader', 'DialogTitle', 'Tabs',
  'TabsList', 'TabsContent', 'MaskedCodeBlockCopyButton', 'Tooltip', 'TooltipContent', 'TooltipTrigger']) {
  mocks[name] = passthrough;
}
globalThis.__piDialogIntegration = mocks;
const source = readFileSync(new URL('./apikeys-view-dialog.tsx', import.meta.url), 'utf8').replace(/^import[\s\S]*?;\n/gm, '');
const { ApiKeysViewDialog } = await load(`const { ${Object.keys(mocks).join(', ')} } = globalThis.__piDialogIntegration;\n${source}`);

test('Pi tab reaches the model export and respects the loaded key profile', () => {
  detail = { profiles: { activeProfile: 'restricted', profiles: [{ name: 'restricted', modelIDs: ['allowed'] }] } };
  blocks = [];
  const html = renderToStaticMarkup(React.createElement(ApiKeysViewDialog));
  assert.match(html, /data-tab="pi"/);
  const block = blocks.find((item) => item.language === 'json');
  const provider = JSON.parse(block.realCode).providers.axonhub;
  assert.deepEqual(provider.models.map((model) => model.id), ['allowed']);
  assert.equal(provider.models[0].thinkingLevelMap.high, 'high');
  assert.equal(provider.apiKey, 'ah-test-not-a-real-key');
  assert.equal(JSON.parse(block.displayCode).providers.axonhub.apiKey, 'ah-...-key');
});

test('Pi export stays empty until key details arrive instead of using the unrestricted list row', () => {
  detail = undefined;
  blocks = [];
  renderToStaticMarkup(React.createElement(ApiKeysViewDialog));
  assert.equal(blocks.find((item) => item.language === 'json').realCode, '');
});
