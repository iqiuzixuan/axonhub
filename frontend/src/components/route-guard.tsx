import { useEffect } from 'react';
import { useMatch, useRouter } from '@tanstack/react-router';
import { IconShieldX, IconArrowLeft } from '@tabler/icons-react';
import { type ScopeLevel } from '@/config/route-permission';
import { useTranslation } from 'react-i18next';
import { useRoutePermissions } from '@/hooks/useRoutePermissions';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

interface RouteGuardProps {
  children: React.ReactNode;
  requiredScopes?: string[];
  scopeLevel?: ScopeLevel;
  fallbackPath?: string;
  showForbidden?: boolean;
  requireProjectOwner?: boolean; // 是否需要项目所有者权限
}

export function RouteGuard({
  children,
  requiredScopes = [],
  scopeLevel,
  fallbackPath,
  showForbidden = true,
  requireProjectOwner = false,
}: RouteGuardProps) {
  const router = useRouter();
  // During navigation the URL can change before this route unmounts.
  // Always check the page being rendered, not the pending destination.
  const pathname = useMatch({ strict: false, select: (match) => match.pathname });
  const { checkRouteAccess, hasRouteAccess, defaultPath } = useRoutePermissions();
  const accessibleFallback = fallbackPath && checkRouteAccess(fallbackPath).hasAccess ? fallbackPath : defaultPath;
  const returnPath = accessibleFallback === pathname ? '/settings/profile' : accessibleFallback;
  const hasAccess =
    checkRouteAccess(pathname).hasAccess && hasRouteAccess({ path: pathname, requiredScopes, scopeLevel, requireProjectOwner });

  useEffect(() => {
    if (!hasAccess && !showForbidden) {
      // 如果没有权限且不显示禁止页面，则重定向
      router.navigate({ to: returnPath, replace: true });
    }
  }, [hasAccess, showForbidden, returnPath, router]);

  if (!hasAccess) {
    if (showForbidden) {
      return <ForbiddenPage onGoBack={() => router.navigate({ to: returnPath, replace: true })} />;
    }
    return null; // 重定向中，不显示任何内容
  }

  return <>{children}</>;
}

function ForbiddenPage({ onGoBack }: { onGoBack: () => void }) {
  const { t } = useTranslation();

  return (
    <div className='flex h-screen items-center justify-center'>
      <div className='max-w-md text-center'>
        <div className='mb-6'>
          <IconShieldX className='mx-auto h-16 w-16 text-red-500' />
        </div>

        <Alert className='mb-6'>
          <IconShieldX className='h-4 w-4' />
          <AlertTitle>{t('common.routeGuard.accessDenied')}</AlertTitle>
          <AlertDescription>{t('common.routeGuard.noPermission')}</AlertDescription>
        </Alert>

        <Button onClick={onGoBack} variant='outline' className='gap-2'>
          <IconArrowLeft className='h-4 w-4' />
          {t('common.routeGuard.goBack')}
        </Button>
      </div>
    </div>
  );
}
