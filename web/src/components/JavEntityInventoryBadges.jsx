import { zh } from '@/utils/i18n'

function normalizeCount(value) {
  if (value === null || value === undefined || value === '') return null
  const count = Number(value)
  if (!Number.isFinite(count)) return null
  return Math.max(0, Math.floor(count))
}

/**
 * Keeps an entity's metadata count and real-file state visible together.
 * Text labels intentionally accompany the colors so the states remain clear
 * without relying on color perception.
 */
export default function JavEntityInventoryBadges({ item, showTotal = true }) {
  const workCount = normalizeCount(item?.work_count)
  const pendingCount = normalizeCount(item?.pending_count)
  const importedCount = normalizeCount(item?.imported_count)
  const hasBreakdown = pendingCount !== null && importedCount !== null

  if (workCount === null || (!showTotal && !hasBreakdown)) return null

  const statusLabel = hasBreakdown
    ? showTotal
      ? zh(
          `共 ${workCount} 部，待入库 ${pendingCount} 部，已入库 ${importedCount} 部`,
          `${workCount} works, ${pendingCount} pending, ${importedCount} imported`
        )
      : zh(
          `待入库 ${pendingCount} 部，已入库 ${importedCount} 部`,
          `${pendingCount} pending, ${importedCount} imported`
        )
    : zh(`共 ${workCount} 部作品`, `${workCount} works`)

  return (
    <div
      className="flex min-w-0 flex-nowrap items-center gap-0.5 text-[9px] font-semibold tabular-nums leading-3"
      title={statusLabel}
      aria-label={statusLabel}
    >
      {showTotal ? (
        <span className="absolute right-3 top-3 inline-flex shrink-0 items-center whitespace-nowrap rounded bg-slate-900 px-1.5 py-0.5 text-[11px] font-semibold leading-4 text-white shadow-sm">
          {zh(`共${workCount}部`, `${workCount} works`)}
        </span>
      ) : null}
      {hasBreakdown ? (
        <>
          <span className="inline-flex shrink-0 items-center whitespace-nowrap rounded-sm border border-violet-200 bg-violet-100 px-1 py-0.5 text-violet-900">
            {zh(`待入库${pendingCount}`, `Pending ${pendingCount}`)}
          </span>
          <span className="inline-flex shrink-0 items-center whitespace-nowrap rounded-sm border border-pink-200 bg-pink-100 px-1 py-0.5 text-pink-900">
            {zh(`已入库${importedCount}`, `Imported ${importedCount}`)}
          </span>
        </>
      ) : null}
    </div>
  )
}
