import { createFileRoute } from '@tanstack/react-router';
import PersonalModelsPage from '@/features/personal/models';

export const Route = createFileRoute('/_authenticated/me/models')({
  validateSearch: (search: Record<string, unknown>): { q?: string } => ({
    q: typeof search.q === 'string' ? search.q.slice(0, 200) : undefined,
  }),
  component: PersonalModelsPage,
});
