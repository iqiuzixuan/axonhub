import { Check, Copy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard';
import { Button } from '@/components/ui/button';

export function ModelIDCopyCell({ value }: { value: string }) {
  const { t } = useTranslation();
  const { isCopied, handleCopy } = useCopyToClipboard({ text: value });
  return (
    <Button
      variant='ghost'
      size='sm'
      className='group hover:text-primary h-auto justify-start gap-1.5 px-0 py-1 text-sm font-medium hover:bg-transparent'
      aria-label={t('personal.copyModelId', { model: value })}
      title={t(isCopied ? 'personal.copied' : 'personal.clickToCopy')}
      onClick={(event) => {
        event.stopPropagation();
        void handleCopy();
      }}
    >
      <span>{value}</span>
      {isCopied ? (
        <Check className='text-primary size-3.5' />
      ) : (
        <Copy className='text-muted-foreground size-3.5 opacity-50 group-hover:opacity-100 group-focus-visible:opacity-100' />
      )}
    </Button>
  );
}
