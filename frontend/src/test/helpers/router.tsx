import { createElement, type ReactNode } from 'react'

import { AppRouterContext } from '@/hooks/use-app-location'
import {
  createAppRouter,
  memoryHistory,
  type AppRouter,
  type MemoryHistory,
  type RouterContext,
} from '@/lib/app-router'

export const READY_CONTEXT: RouterContext = {
  firstPageId: 'p1',
  knownPageIds: ['p1', 'p2'],
  canAdmin: true,
  shellVisible: true,
}

export interface TestRouter {
  router: AppRouter
  history: MemoryHistory
  wrapper: (props: { children: ReactNode }) => ReactNode
}

/** A real router on an in-memory history, started and given a settled context,
 * plus the provider wrapper for renderHook/render. */
export function testRouter(path = '/'): TestRouter {
  const history = memoryHistory(path)
  const router = createAppRouter(history)
  router.start()
  router.setContext(READY_CONTEXT)
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(AppRouterContext.Provider, { value: router }, children)
  return { router, history, wrapper }
}
