import { useEffect, useMemo, useState } from 'react'
import CloseRoundedIcon from '@mui/icons-material/CloseRounded'
import { Button } from '@mui/material'
import AppModal from '@/components/AppModal'
import JavEntityInventoryBadges from '@/components/JavEntityInventoryBadges'
import {
  JAV_INVENTORY_ALL,
  JAV_INVENTORY_IMPORTED,
  JAV_INVENTORY_PENDING,
  normalizeJavInventory,
} from '@/utils/javInventory'
import { isChineseLocale, zh } from '@/utils/i18n'

const isModifiedClick = (event) =>
  event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0

function censorLabel(value) {
  if (value === 'mixed') return zh('混合', 'Mixed')
  if (value === true) return zh('无码', 'Uncensored')
  if (value === false) return zh('有码', 'Censored')
  return zh('未知', 'Unknown')
}

const unknownStudioLabel = () => zh('未知片商', 'Unknown studio')
const studioListSeparator = () => (isChineseLocale() ? '、' : ', ')

function normalizeCount(value) {
  if (value === null || value === undefined || value === '') return null
  const count = Number(value)
  if (!Number.isFinite(count)) return null
  return Math.max(0, Math.floor(count))
}

/**
 * Read the inventory counters without making old API responses invalid.
 * Prefixes were originally returned with only work_count; once the API
 * exposes pending_count/imported_count those counters become authoritative.
 */
function readInventoryCounts(item) {
  const workCount = normalizeCount(item?.work_count)
  let pendingCount = normalizeCount(item?.pending_count)
  let importedCount = normalizeCount(item?.imported_count)

  // Be tolerant of a partially upgraded server while keeping the invariant
  // total = pending + imported whenever the API provides enough information.
  if (workCount !== null && pendingCount === null && importedCount !== null) {
    pendingCount = Math.max(0, workCount - importedCount)
  }
  if (workCount !== null && importedCount === null && pendingCount !== null) {
    importedCount = Math.max(0, workCount - pendingCount)
  }
  const total = workCount ?? (pendingCount ?? 0) + (importedCount ?? 0)
  const hasBreakdown = pendingCount !== null && importedCount !== null
  return {
    total,
    pending: pendingCount ?? 0,
    imported: importedCount ?? 0,
    hasBreakdown,
  }
}

function inventoryCountForMode(counts, inventoryMode) {
  switch (inventoryMode) {
    case JAV_INVENTORY_PENDING:
      return counts.pending
    case JAV_INVENTORY_IMPORTED:
      return counts.imported
    default:
      return counts.total
  }
}

function inventoryLabel(inventoryMode) {
  switch (inventoryMode) {
    case JAV_INVENTORY_PENDING:
      return zh('待入库', 'Pending')
    case JAV_INVENTORY_IMPORTED:
      return zh('已入库', 'Imported')
    default:
      return zh('全部', 'All')
  }
}

export default function JavPrefixModal({
  open,
  items = [],
  loading = false,
  error = '',
  activePrefix = '',
  buildPrefixUrl,
  onClose,
  onSelectPrefix,
}) {
  const [search, setSearch] = useState('')
  const [sortMode, setSortMode] = useState('count')
  const [censorMode, setCensorMode] = useState('all')
  const [inventoryMode, setInventoryMode] = useState(JAV_INVENTORY_ALL)
  const normalizedSearch = search.trim().toLowerCase()
  const filteredItems = useMemo(() => {
    const merged = new Map()
    ;(items || []).forEach((item) => {
      if (censorMode === 'censored' && item?.is_uncensored !== false) return
      if (censorMode === 'uncensored' && item?.is_uncensored !== true) return
      if (normalizedSearch) {
        const prefix = String(item?.prefix || '').toLowerCase()
        const studio = String(item?.studio_name || unknownStudioLabel()).toLowerCase()
        if (!prefix.includes(normalizedSearch) && !studio.includes(normalizedSearch)) return
      }

      const prefix = String(item?.prefix || '').trim()
      if (!prefix) return
      const counts = readInventoryCounts(item)
      // Inventory-specific views are only meaningful when the API supplied
      // both counters. This prevents a legacy response (work_count only)
      // from being presented as an empty, authoritative pending/imported set.
      if (inventoryMode !== JAV_INVENTORY_ALL && !counts.hasBreakdown) return
      const visibleCount = inventoryCountForMode(counts, inventoryMode)
      if (visibleCount <= 0) return
      const visiblePendingCount =
        inventoryMode === JAV_INVENTORY_IMPORTED
          ? 0
          : inventoryMode === JAV_INVENTORY_PENDING
            ? visibleCount
            : counts.pending
      const visibleImportedCount =
        inventoryMode === JAV_INVENTORY_PENDING
          ? 0
          : inventoryMode === JAV_INVENTORY_IMPORTED
            ? visibleCount
            : counts.imported
      const key = prefix.toUpperCase()
      const existing = merged.get(key) || {
        ...item,
        prefix,
        studio_name: '',
        work_count: 0,
        pending_count: 0,
        imported_count: 0,
        inventory_breakdown_available: counts.hasBreakdown,
        inventory_mode: inventoryMode,
        is_uncensored: item?.is_uncensored,
        studios: new Map(),
        censorValues: new Set(),
      }
      existing.inventory_breakdown_available =
        existing.inventory_breakdown_available && counts.hasBreakdown
      const studioName = String(item?.studio_name || '').trim() || unknownStudioLabel()
      const studioId = Number(item?.studio_id)
      const hasStudioId = Number.isFinite(studioId) && studioId > 0
      const studioKey = hasStudioId ? `id:${studioId}` : `name:${studioName}`
      const studioItem = existing.studios.get(studioKey) || {
        id: hasStudioId ? studioId : null,
        name: studioName,
        work_count: 0,
        pending_count: 0,
        imported_count: 0,
      }
      studioItem.work_count += visibleCount
      studioItem.pending_count += visiblePendingCount
      studioItem.imported_count += visibleImportedCount
      existing.studios.set(studioKey, studioItem)
      if (item?.is_uncensored === true || item?.is_uncensored === false) {
        existing.censorValues.add(item.is_uncensored)
      }
      existing.work_count += visibleCount
      existing.pending_count += visiblePendingCount
      existing.imported_count += visibleImportedCount
      merged.set(key, existing)
    })

    const list = Array.from(merged.values()).map((item) => {
      const censorValues = Array.from(item.censorValues)
      const studioItems = Array.from(item.studios.values()).sort(
        (a, b) =>
          Number(b?.work_count || 0) - Number(a?.work_count || 0) ||
          String(a?.name || '').localeCompare(String(b?.name || ''))
      )
      return {
        ...item,
        studioItems,
        studio_name: studioItems.map((studio) => studio.name).join(studioListSeparator()),
        is_uncensored:
          censorValues.length === 1 ? censorValues[0] : censorValues.length > 1 ? 'mixed' : null,
      }
    })
    return [...list].sort((a, b) => {
      const aPrefix = String(a?.prefix || '')
      const bPrefix = String(b?.prefix || '')
      if (sortMode === 'az') {
        return (
          aPrefix.localeCompare(bPrefix, undefined, { numeric: true }) ||
          String(a?.studio_name || '').localeCompare(String(b?.studio_name || ''))
        )
      }
      const countDiff = Number(b?.work_count || 0) - Number(a?.work_count || 0)
      return (
        countDiff ||
        aPrefix.localeCompare(bPrefix, undefined, { numeric: true }) ||
        String(a?.studio_name || '').localeCompare(String(b?.studio_name || ''))
      )
    })
  }, [censorMode, inventoryMode, items, normalizedSearch, sortMode])

  const inventoryBreakdownAvailable = useMemo(() => {
    const sourceItems = items || []
    return (
      sourceItems.length > 0 && sourceItems.every((item) => readInventoryCounts(item).hasBreakdown)
    )
  }, [items])

  const inventorySummary = useMemo(() => {
    let total = 0
    let pending = 0
    let imported = 0
    let hasBreakdown = (items || []).length > 0
    ;(items || []).forEach((item) => {
      const counts = readInventoryCounts(item)
      total += counts.total
      pending += counts.pending
      imported += counts.imported
      hasBreakdown = hasBreakdown && counts.hasBreakdown
    })
    return { total, pending, imported, hasBreakdown }
  }, [items])

  useEffect(() => {
    if (!open) return
    setSearch('')
    setSortMode('count')
    setCensorMode('all')
    setInventoryMode(JAV_INVENTORY_ALL)
  }, [open])

  if (!open) return null

  return (
    <AppModal
      ariaLabelledby="jav-prefix-modal-title"
      className="p-4"
      contentClassName="flex max-h-[86vh] w-full max-w-4xl flex-col rounded-lg bg-white shadow-xl"
      onClose={onClose}
    >
      <div className="flex items-center justify-between border-b px-5 py-4">
        <div>
          <h2 id="jav-prefix-modal-title" className="text-lg font-semibold text-gray-900">
            {zh('番号', 'JAV codes')}
          </h2>
          <p className="mt-1 text-xs text-gray-500">
            {zh(
              '点击番号查询对应影片；作品状态与有码/无码独立统计',
              'Select a code to filter matching works; work status is tracked separately from censor status'
            )}
            {inventorySummary.hasBreakdown ? (
              <span className="ml-2 text-gray-400">
                {zh(
                  `索引总计 ${inventorySummary.total} 部 · 待入库 ${inventorySummary.pending} · 已入库 ${inventorySummary.imported}`,
                  `Index total ${inventorySummary.total} · ${inventorySummary.pending} pending · ${inventorySummary.imported} imported`
                )}
              </span>
            ) : null}
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="inline-flex h-8 w-8 items-center justify-center rounded-full text-gray-500 hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
          aria-label={zh('关闭', 'Close')}
        >
          <CloseRoundedIcon fontSize="small" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3 border-b px-5 py-3">
        <input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          className="h-9 min-w-[14rem] flex-1 rounded border border-gray-200 px-3 text-sm outline-none focus:border-blue-400 focus:ring-2 focus:ring-blue-100"
          placeholder={zh('搜索番号或片商', 'Search code or studio')}
          aria-label={zh('搜索番号', 'Search JAV codes')}
        />
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-xs font-medium text-gray-500">
            {zh('作品状态', 'Work status')}
          </span>
          <div
            className="inline-flex shrink-0 overflow-hidden rounded border border-gray-200 bg-white text-xs"
            role="group"
            aria-label={zh('作品状态筛选', 'Work status filter')}
          >
            {[JAV_INVENTORY_ALL, JAV_INVENTORY_PENDING, JAV_INVENTORY_IMPORTED].map(
              (mode, index) => (
                <button
                  key={mode}
                  type="button"
                  disabled={mode !== JAV_INVENTORY_ALL && !inventoryBreakdownAvailable}
                  className={`px-3 py-2 font-medium ${index > 0 ? 'border-l border-gray-200' : ''} ${
                    inventoryMode === mode
                      ? 'bg-blue-50 text-blue-700'
                      : mode !== JAV_INVENTORY_ALL && !inventoryBreakdownAvailable
                        ? 'cursor-not-allowed text-gray-300'
                        : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
                  }`}
                  onClick={() => setInventoryMode(normalizeJavInventory(mode))}
                  aria-pressed={inventoryMode === mode}
                  title={
                    mode !== JAV_INVENTORY_ALL && !inventoryBreakdownAvailable
                      ? zh('服务端尚未返回作品状态统计', 'Work status counters are unavailable')
                      : undefined
                  }
                >
                  {inventoryLabel(mode)}
                </button>
              )
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-xs font-medium text-gray-500">{zh('类型', 'Type')}</span>
          <div
            className="inline-flex shrink-0 overflow-hidden rounded border border-gray-200 bg-white text-xs"
            role="group"
            aria-label={zh('有码无码筛选', 'Censor status filter')}
          >
            <button
              type="button"
              className={`px-3 py-2 font-medium ${
                censorMode === 'all'
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
              onClick={() => setCensorMode('all')}
            >
              {zh('全部', 'All')}
            </button>
            <button
              type="button"
              className={`border-l border-gray-200 px-3 py-2 font-medium ${
                censorMode === 'censored'
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
              onClick={() => setCensorMode('censored')}
            >
              {zh('有码', 'Censored')}
            </button>
            <button
              type="button"
              className={`border-l border-gray-200 px-3 py-2 font-medium ${
                censorMode === 'uncensored'
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
              onClick={() => setCensorMode('uncensored')}
            >
              {zh('无码', 'Uncensored')}
            </button>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-xs font-medium text-gray-500">{zh('排序', 'Sort')}</span>
          <div
            className="inline-flex shrink-0 overflow-hidden rounded border border-gray-200 bg-white text-xs"
            role="group"
            aria-label={zh('番号排序', 'Code sort order')}
          >
            <button
              type="button"
              className={`px-3 py-2 font-medium ${
                sortMode === 'count'
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
              onClick={() => setSortMode('count')}
            >
              {zh('作品数', 'Works')}
            </button>
            <button
              type="button"
              className={`border-l border-gray-200 px-3 py-2 font-medium ${
                sortMode === 'az'
                  ? 'bg-blue-50 text-blue-700'
                  : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
              }`}
              onClick={() => setSortMode('az')}
            >
              A-Z
            </button>
          </div>
        </div>
      </div>

      <div className="min-h-[260px] flex-1 overflow-auto">
        {loading ? (
          <div className="flex min-h-[260px] items-center justify-center text-sm text-gray-500">
            {zh('加载中…', 'Loading...')}
          </div>
        ) : error ? (
          <div className="m-5 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
            {error}
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="flex min-h-[260px] items-center justify-center text-sm text-gray-500">
            {normalizedSearch
              ? zh('没有匹配番号', 'No matching codes')
              : inventoryMode === JAV_INVENTORY_ALL
                ? zh('暂无番号', 'No codes')
                : zh(
                    `暂无${inventoryLabel(inventoryMode)}番号`,
                    `No ${inventoryLabel(inventoryMode).toLowerCase()} codes`
                  )}
          </div>
        ) : (
          <table className="w-full border-collapse text-left text-sm">
            <thead className="sticky top-0 bg-gray-50 text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-5 py-3 font-semibold">{zh('番号', 'Code')}</th>
                <th className="px-5 py-3 font-semibold">{zh('片商', 'Studio')}</th>
                <th className="px-5 py-3 font-semibold">{zh('类型', 'Type')}</th>
                <th className="px-5 py-3 font-semibold">{zh('作品状态', 'Work status')}</th>
                <th className="px-5 py-3 text-right font-semibold">{zh('作品数', 'Works')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filteredItems.map((item) => {
                const prefix = String(item?.prefix || '').trim()
                const active = prefix && prefix === activePrefix
                const href = buildPrefixUrl?.(item) || '#'
                return (
                  <tr
                    key={`${prefix}-${item?.studio_id || 'none'}-${String(item?.is_uncensored)}`}
                    className={active ? 'bg-blue-50' : 'hover:bg-gray-50'}
                  >
                    <td className="px-5 py-3">
                      <a
                        href={href}
                        className="font-semibold text-blue-700 hover:text-blue-800 hover:underline"
                        onClick={(event) => {
                          if (isModifiedClick(event)) return
                          event.preventDefault()
                          onSelectPrefix?.(item)
                        }}
                      >
                        {prefix}
                      </a>
                    </td>
                    <td className="px-5 py-3 text-gray-700">
                      <div className="flex flex-wrap gap-y-1">
                        {(item?.studioItems || []).length > 0 ? (
                          item.studioItems.map((studio, index) => {
                            const studios = item.studioItems || []
                            const studioName = String(studio?.name || '').trim()
                            const studioId = Number(studio?.id)
                            const hasStudio = Number.isFinite(studioId) && studioId > 0
                            const studioFilterItem = {
                              ...item,
                              studio_id: hasStudio ? studioId : null,
                              studio_name: studioName || unknownStudioLabel(),
                              include_studio_filter: true,
                            }
                            return (
                              <span
                                key={`${hasStudio ? studioId : 'unknown'}-${studioName || index}`}
                                className="inline-flex"
                              >
                                <a
                                  href={buildPrefixUrl?.(studioFilterItem) || '#'}
                                  className="text-gray-700 hover:text-gray-900 hover:underline"
                                  title={zh(
                                    `搜索 ${prefix} + ${studioFilterItem.studio_name}`,
                                    `Search ${prefix} + ${studioFilterItem.studio_name}`
                                  )}
                                  onClick={(event) => {
                                    if (isModifiedClick(event)) return
                                    event.preventDefault()
                                    onSelectPrefix?.(studioFilterItem)
                                  }}
                                >
                                  {studioFilterItem.studio_name}
                                </a>
                                {index < studios.length - 1 ? (
                                  <span className="text-gray-500">{studioListSeparator()}</span>
                                ) : null}
                              </span>
                            )
                          })
                        ) : (
                          <a
                            href={
                              buildPrefixUrl?.({
                                ...item,
                                studio_id: null,
                                studio_name: unknownStudioLabel(),
                                include_studio_filter: true,
                              }) || '#'
                            }
                            className="text-gray-700 hover:text-gray-900 hover:underline"
                            title={zh(
                              `搜索 ${prefix} + ${unknownStudioLabel()}`,
                              `Search ${prefix} + ${unknownStudioLabel()}`
                            )}
                            onClick={(event) => {
                              if (isModifiedClick(event)) return
                              event.preventDefault()
                              onSelectPrefix?.({
                                ...item,
                                studio_id: null,
                                studio_name: unknownStudioLabel(),
                                include_studio_filter: true,
                              })
                            }}
                          >
                            {unknownStudioLabel()}
                          </a>
                        )}
                      </div>
                    </td>
                    <td className="px-5 py-3 text-gray-700">{censorLabel(item?.is_uncensored)}</td>
                    <td className="px-5 py-3">
                      {item?.inventory_breakdown_available ? (
                        <JavEntityInventoryBadges item={item} showTotal={false} />
                      ) : (
                        <span className="text-gray-400">{zh('未知', 'Unknown')}</span>
                      )}
                    </td>
                    <td className="px-5 py-3 text-right font-medium text-gray-900">
                      {Number(item?.work_count || 0).toLocaleString()}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      <div className="flex justify-end border-t px-5 py-3">
        <Button variant="outlined" onClick={onClose}>
          {zh('关闭', 'Close')}
        </Button>
      </div>
    </AppModal>
  )
}
