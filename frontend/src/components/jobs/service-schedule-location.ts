import { formatAppLocation, type AppLocation } from '@/lib/app-location'

/** Where the Jobs page's Service schedule link goes: the link's address and the click's move are this one location. */
export const SERVICE_SCHEDULE_LOCATION: AppLocation = { panel: 'inventory', route: { view: 'index', section: 'maintenance' } }

export const SERVICE_SCHEDULE_HREF = formatAppLocation(SERVICE_SCHEDULE_LOCATION, { firstPageId: null })
