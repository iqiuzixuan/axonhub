import { createFileRoute } from '@tanstack/react-router';
import PersonalDashboardPage from '@/features/personal/dashboard';

export const Route = createFileRoute('/_authenticated/me/dashboard')({
  component: PersonalDashboardPage,
});
