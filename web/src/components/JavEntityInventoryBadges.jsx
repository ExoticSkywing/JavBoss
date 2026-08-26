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
export default function JavEntityInventoryBadges({ item }) {
  const workCount = normalizeCount(item?.work_count)
  const pendingCount = normalizeCount(item?.pending_count)
  const importedCount = normalizeCount(item?.imported_count)
  const hasBreakdown = pendingCount !== null && importedCount !== null

  if (workCount === null) return null

  const statusLabel = hasBreakdown
    ? zh(
        `共 ${workCount} 部，待入库 ${pendingCount} 部，已入库 ${importedCount} 部`,
        `${workCount} works, ${pendingCount} pending, ${importedCount} imported`
      )
    : zh(`共 ${workCount} 部作品`, `${workCount} works`)

  return (
    <div
      className="pointer-events-none absolute left-2 top-2 flex flex-col items-start gap-1 text-[11px] font-semibold tabular-nums"
      aria-label={statusLabel}
    >
      <span className="rounded bg-slate-950/80 px-2 py-1 text-white shadow-sm">
        {zh(`共 ${workCount} 部`, `${workCount} works`)}
      </span>
      {hasBreakdown ? (
        <span className="inline-flex overflow-hidden rounded shadow-sm">
          <span className="bg-amber-100/95 px-2 py-1 text-amber-900">
            {zh(`待入库 ${pendingCount}`, `Pending ${pendingCount}`)}
          </span>
          <span className="bg-emerald-100/95 px-2 py-1 text-emerald-900">
            {zh(`已入库 ${importedCount}`, `Imported ${importedCount}`)}
          </span>
        </span>
      ) : null}
    </div>
  )
}
