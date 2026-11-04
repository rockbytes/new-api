export { DataTablePagination } from './core/pagination'
export { DataTableColumnHeader } from './core/column-header'
export { BadgeCell } from './core/badge-cell'
export { BadgeListCell } from './core/badge-list-cell'
export { TruncatedCell } from './core/truncated-cell'
export { DataTableViewOptions } from './toolbar/view-options'
export { DataTableToolbar } from './toolbar/toolbar'
export { DataTableMobileFilterPanel } from './toolbar/mobile-filter-panel'
export { DataTableBulkActions } from './toolbar/bulk-actions'
export {
  StaticDataTable,
  type StaticDataTableColumn,
} from './static/static-data-table'

export { staticDataTableClassNames } from './static/static-data-table-classnames'
export {
  DataTableRow,
  DataTableRowActionMenu,
  DataTableView,
} from './core/data-table-view'
export { MobileCardList } from './layout/mobile-card-list'

export { DataTablePage } from './layout/data-table-page'
export { DataTableViewModeToggle } from './toolbar/view-mode-toggle'
export { useDataTable } from './hooks/use-data-table'
export {
  useDataTableViewMode,
  type DataTableViewMode,
} from './hooks/use-data-table-view-mode'
export { useDebouncedColumnFilter } from './hooks/use-debounced-column-filter'

export const DISABLED_ROW_DESKTOP =
  '[--data-table-card-bg:var(--table-disabled)] hover:[--data-table-card-bg:var(--table-disabled-hover)] data-[state=selected]:![--data-table-card-bg:var(--table-disabled)] data-[state=selected]:hover:![--data-table-card-bg:var(--table-disabled-hover)] [background-color:var(--table-disabled)] hover:[background-color:var(--table-disabled-hover)] [&>td:first-child]:[border-left-color:var(--table-disabled-border)] [&>td:first-child]:border-l-4 [&>td:first-child]:pl-1'

export const DISABLED_ROW_MOBILE =
  '[--data-table-card-bg:var(--table-disabled)] data-[state=selected]:![--data-table-card-bg:var(--table-disabled)] [background-color:var(--table-disabled)]'
