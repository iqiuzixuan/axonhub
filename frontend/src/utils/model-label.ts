import type { TFunction } from 'i18next';

// Keep the API value stable for filters and grouping; translate only its label.
export function formatModelLabel(model: string | null | undefined, t: TFunction): string {
  if (model === '[original model unavailable]') {
    return t('billing.originalModelNotRecorded');
  }
  return model || t('requests.columns.unknown');
}
