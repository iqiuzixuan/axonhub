import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { useState, type ComponentProps } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog';
import { Form } from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import { Label } from '@/components/ui/label';
import { ModelPriceEditor } from '@/components/model-price-editor';
import { modelPriceSchema } from '@/features/channels/data/schema';
import { useGeneralSettings } from '@/features/system/data/system';
import { useModels } from '../context/models-context';
import { useUpdateModel } from '../data/models';

const schema = z.object({ prices: z.array(z.object({ modelId: z.string(), price: modelPriceSchema })) });
type Values = z.infer<typeof schema>;

export function ModelBillingPriceDialog() {
  const { t } = useTranslation();
  const { currentRow, setOpen } = useModels();
  const update = useUpdateModel();
  const { data: general } = useGeneralSettings();
  const [configured, setConfigured] = useState(Boolean(currentRow?.settings.billingPrice));
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { prices: [{ modelId: currentRow!.modelID, price: currentRow!.settings.billingPrice ?? { items: [] } }] },
  });
  const itemsPath = 'prices.0.price.items' as const;
  const save = form.handleSubmit(async (data) => {
    await update.mutateAsync({
      id: currentRow!.id,
      input: { settings: { ...currentRow!.settings, billingPrice: configured ? data.prices[0].price : null } },
    });
    setOpen(null);
  });
  return (
    <Dialog open onOpenChange={() => setOpen(null)}>
      <DialogContent className='flex max-h-[90vh] flex-col sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {t('billing.price.title')} · {currentRow!.modelID}
          </DialogTitle>
          <DialogDescription>{t('billing.price.description')}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={save} className='flex min-h-0 flex-col gap-4'>
            <div className='flex items-center gap-2'>
              <Switch id='billing-price-configured' checked={configured} onCheckedChange={setConfigured} />
              <Label htmlFor='billing-price-configured'>{t('billing.price.configured')}</Label>
            </div>
            {configured && (
              <div className='min-h-0 overflow-y-auto'>
                <ModelPriceEditor
                  control={form.control as unknown as ComponentProps<typeof ModelPriceEditor>['control']}
                  priceIndex={0}
                  currencyCode={general?.currencyCode}
                  onAddItem={() =>
                    form.setValue(itemsPath, [
                      ...form.getValues(itemsPath),
                      { itemCode: 'prompt_tokens', pricing: { mode: 'usage_per_unit', usagePerUnit: '0' } },
                    ])
                  }
                  onRemoveItem={(_, index) =>
                    form.setValue(
                      itemsPath,
                      form.getValues(itemsPath).filter((_, i) => i !== index)
                    )
                  }
                  onAddVariant={(_, index) => {
                    const path = `prices.0.price.items.${index}.promptWriteCacheVariants` as const;
                    form.setValue(path, [
                      ...(form.getValues(path) ?? []),
                      { variantCode: 'five_min', pricing: { mode: 'usage_per_unit', usagePerUnit: '0' } },
                    ]);
                  }}
                  onRemoveVariant={(_, index, variant) => {
                    const path = `prices.0.price.items.${index}.promptWriteCacheVariants` as const;
                    form.setValue(
                      path,
                      (form.getValues(path) ?? []).filter((_, i) => i !== variant)
                    );
                  }}
                />
                {form.watch(itemsPath).length === 0 && <p className='text-sm text-muted-foreground'>{t('billing.price.free')}</p>}
              </div>
            )}
            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setOpen(null)}>
                {t('common.buttons.cancel')}
              </Button>
              <Button type='submit' disabled={update.isPending}>
                {t('common.buttons.save')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
