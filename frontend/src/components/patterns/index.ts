// ADR 0142: the CRUD pattern library's barrel. Every Equipment-style index
// or details page composes these instead of reaching for ui/table or
// ui/card directly - see docs/adr/0142-crud-pattern-library.md.
//
// The dev-only gallery (gallery.tsx) is deliberately NOT re-exported here:
// it exists to demonstrate these patterns with fixture data, not to be
// composed by one, and barrelling it in would pull its fixtures into every
// production bundle that imports anything from this module. App.tsx lazy-
// loads it by its own path instead.

export { ConfirmDelete, type ConfirmDeleteProps } from './confirm-delete'
export { DetailsLayout, type DetailsLayoutProps } from './details-layout'
export { EmptyState, type EmptyStateProps } from './empty-state'
export { FormRow, type FormRowProps } from './form-row'
export { FormSection, type FormSectionProps } from './form-section'
export {
  IndexTable,
  RowActions,
  type IndexTableProps,
  type RowAction,
  type RowActionsProps,
} from './index-table'
export { IndexFilters, type IndexFilter, type IndexFilterOption, type IndexFiltersProps } from './index-filters'
export { ResourceItem, type ResourceItemProps } from './resource-item'
export { ResourceList, type ResourceListProps } from './resource-list'
export { Page, type PageAction, type PageProps } from './page'
export { SaveBar, type SaveBarProps } from './save-bar'
export { SaveBarSlot, SAVE_BAR_SLOT_ID } from './save-bar-slot'
