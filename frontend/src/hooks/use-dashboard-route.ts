import { useLocalStorageId } from '@/hooks/use-local-storage-id'

const DASHBOARD_ROUTE_KEY = 'routes.dashboardRouteId'

export function useDashboardRouteId(): [string | null, (id: string | null) => void] {
  return useLocalStorageId(DASHBOARD_ROUTE_KEY)
}
