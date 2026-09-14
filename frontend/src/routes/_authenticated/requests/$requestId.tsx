import { createFileRoute } from '@tanstack/react-router';
import { RouteGuard } from '@/components/route-guard';
import RequestDetailGlobalPage from '@/features/requests/components/request-detail-global-page';

export const Route = createFileRoute('/_authenticated/requests/$requestId')({
  component: () => (
    <RouteGuard requireRequestDetails scopeLevel="system">
      <RequestDetailGlobalPage />
    </RouteGuard>
  ),
});
