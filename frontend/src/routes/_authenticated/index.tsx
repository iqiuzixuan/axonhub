import { createFileRoute } from '@tanstack/react-router';
import { useSelectedProjectId } from '@/stores/projectStore';
import { useRoutePermissions } from '@/hooks/useRoutePermissions';
import { RouteGuard } from '@/components/route-guard';
import Dashboard from '@/features/dashboard';
import { useMyProjects } from '@/features/projects/data/projects';

function ProtectedDashboard() {
  const { checkRouteAccess } = useRoutePermissions();
  const selectedProjectId = useSelectedProjectId();
  const { data: myProjects, isLoading } = useMyProjects();
  const projectReady = !isLoading && (myProjects?.length ? myProjects.some((p) => p.id === selectedProjectId) : !selectedProjectId);

  // Let ProjectSwitcher select an active project before choosing a project landing page.
  if (!checkRouteAccess('/').hasAccess && !projectReady) {
    return null;
  }

  return (
    <RouteGuard requiredScopes={['read_dashboard']} scopeLevel='system' showForbidden={false}>
      <Dashboard />
    </RouteGuard>
  );
}

export const Route = createFileRoute('/_authenticated/')({
  component: ProtectedDashboard,
});
