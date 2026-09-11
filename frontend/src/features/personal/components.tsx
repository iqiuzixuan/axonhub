import { useTranslation } from 'react-i18next';
import { usePersonalScope } from '@/stores/personalWorkspaceStore';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { usePersonalWorkspace } from './data';

export function PersonalKeyFilter() {
  const { t } = useTranslation();
  const { apiKeyId, setAPIKeyId } = usePersonalScope();
  const { data } = usePersonalWorkspace();
  return (
    <Select value={apiKeyId ?? 'all'} onValueChange={(value) => setAPIKeyId(value === 'all' ? null : value)}>
      <SelectTrigger className='w-[200px]' aria-label={t('personal.keyFilter')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value='all'>{t('personal.allKeys')}</SelectItem>
        {data?.apiKeys.map((key) => (
          <SelectItem key={key.id} value={key.id}>
            {key.name}
            {key.deleted ? ` · ${t('personal.deleted')}` : key.status !== 'enabled' ? ` · ${t('personal.disabled')}` : ''}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function PersonalLoadError({ retry }: { retry: () => void }) {
  const { t } = useTranslation();
  return (
    <Alert variant='destructive'>
      <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
        {t('personal.loadError')}
        <Button size='sm' variant='outline' onClick={retry}>
          {t('personal.retry')}
        </Button>
      </AlertDescription>
    </Alert>
  );
}
