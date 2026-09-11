import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearch } from '@tanstack/react-router';
import type { ColumnDef, SortingState } from '@tanstack/react-table';
import { Copy, KeyRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { usePersonalScope } from '@/stores/personalWorkspaceStore';
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard';
import { useDebounce } from '@/hooks/use-debounce';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Header } from '@/components/layout/header';
import { Main } from '@/components/layout/main';
import { createColumns } from '@/features/models/components/models-columns';
import { ModelsTable } from '@/features/models/components/models-table';
import ModelsProvider from '@/features/models/context/models-context';
import type { Model } from '@/features/models/data/schema';
import { PersonalKeyFilter, PersonalLoadError } from './components';
import { usePersonalModels, usePersonalWorkspace, type PersonalModel } from './data';
import { personalModelExample } from './utils';

function ModelAccess({ model }: { model: PersonalModel }) {
  const { t } = useTranslation();
  const [language, setLanguage] = useState('curl');
  const embedding = model.type === 'embedding';
  const code = personalModelExample(model.modelId, model.type, window.location.origin + '/v1', language);
  const { handleCopy } = useCopyToClipboard({ text: code });
  return (
    <div className='space-y-4'>
      <h4 className='flex items-center gap-2 text-sm font-semibold'>
        <KeyRound className='size-4' />
        {t('personal.callingKeys')}
      </h4>
      <div className='flex flex-wrap gap-2'>
        {model.apiKeys.map((key) => (
          <Badge variant='outline' key={key.id}>
            {key.name} · {key.projectName}
            {key.activeProfile ? ` · ${key.activeProfile}` : ''}
          </Badge>
        ))}
      </div>
      {(model.type === 'chat' || embedding) && (
        <>
          <div className='flex items-center justify-between'>
            <Tabs value={language} onValueChange={setLanguage}>
              <TabsList>
                <TabsTrigger value='curl'>cURL</TabsTrigger>
                <TabsTrigger value='python'>Python</TabsTrigger>
              </TabsList>
            </Tabs>
            <Button variant='outline' size='sm' onClick={() => void handleCopy()}>
              <Copy className='size-4' />
              {t('personal.copyExample')}
            </Button>
          </div>
          <pre className='bg-muted/30 overflow-auto rounded-lg border p-4 font-mono text-xs leading-6'>
            <code>{code}</code>
          </pre>
          <p className='text-muted-foreground text-xs'>{t('personal.exampleNote')}</p>
        </>
      )}
    </div>
  );
}

export default function PersonalModelsPage() {
  const { t } = useTranslation();
  const search = useSearch({ from: '/_authenticated/me/models' });
  const { projectId, apiKeyId } = usePersonalScope();
  const [nameFilter, setNameFilter] = useState(search.q ?? '');
  useEffect(() => setNameFilter(search.q ?? ''), [search.q]);
  const [sorting, setSorting] = useState<SortingState>([{ id: 'name', desc: false }]);
  const debouncedName = useDebounce(nameFilter, 300);
  const models = usePersonalModels(debouncedName);
  const workspace = usePersonalWorkspace();
  const handleNameFilterChange = useCallback((value: string) => setNameFilter(value), []);
  const byID = useMemo(() => new Map((models.data ?? []).map((model) => [model.id, model])), [models.data]);
  const columns = useMemo<ColumnDef<Model>[]>(
    () =>
      createColumns(t, false, { showAssociations: false, readOnly: true, copyModelID: true }).map((column) =>
        'accessorKey' in column && column.accessorKey === 'type'
          ? {
              ...column,
              cell: ({ row }) =>
                byID.get(row.original.id)?.type ? (
                  <Badge variant='secondary'>{t(`models.types.${row.original.type}`)}</Badge>
                ) : (
                  <span className='text-muted-foreground'>—</span>
                ),
            }
          : column
      ),
    [t, byID]
  );
  // Adapt only display fields to the shared admin table. No settings or channel
  // data are requested from the personal API or used to grant permissions.
  const data = useMemo<Model[]>(
    () =>
      (models.data ?? []).map((model) => ({
        id: model.id,
        name: model.name,
        modelID: model.modelId,
        developer: model.developer,
        icon: model.icon,
        group: model.group,
        type: model.type ?? 'chat',
        status: model.status,
        createdAt: model.createdAt,
        updatedAt: model.updatedAt,
        modelCard: model.modelCard ?? {},
        settings: {
          associations: [],
          disableDeveloperSettingsInheritance: false,
          loadBalancerStrategy: 'default',
          traceStickyMode: 'default',
        },
        associatedChannelCount: 0,
      })),
    [models.data]
  );
  return (
    <ModelsProvider>
      <Header fixed>
        <div className='flex w-full flex-1 flex-col gap-2 md:flex-row md:items-center md:justify-between md:gap-0'>
          <div className='min-w-0'>
            <h2 className='text-xl font-bold tracking-tight'>{t('sidebar.items.models')}</h2>
            <p className='text-muted-foreground text-sm'>{t('personal.modelsDescription')}</p>
          </div>
          <PersonalKeyFilter />
        </div>
      </Header>
      <Main fixed>
        {models.isError || workspace.isError ? (
          <PersonalLoadError
            retry={() => {
              void models.refetch();
              void workspace.refetch();
            }}
          />
        ) : (
          <ModelsTable
            key={`${projectId ?? 'all'}:${apiKeyId ?? 'all'}`}
            columns={columns}
            data={data}
            loading={models.isPending}
            totalCount={data.length}
            nameFilter={nameFilter}
            sorting={sorting}
            onSortingChange={setSorting}
            onNameFilterChange={handleNameFilterChange}
            canWrite={false}
            showAssociations={false}
            renderExpandedFooter={(model) => {
              const personal = byID.get(model.id);
              return personal ? <ModelAccess model={personal} /> : null;
            }}
          />
        )}
      </Main>
    </ModelsProvider>
  );
}
