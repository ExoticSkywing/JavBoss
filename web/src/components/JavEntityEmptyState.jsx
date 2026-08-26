import { zh } from '@/utils/i18n'

const ENTITY_LABELS = {
  idol: ['女优', 'idol'],
  studio: ['片商', 'studio'],
  series: ['系列', 'series'],
}

/**
 * Empty state shared by the metadata entity views.
 *
 * Entity indexes historically only showed entities backed by a visible file.
 * Keep that distinction explicit so a successful zero-result response is not
 * mistaken for a failed metadata scrape. Pending works remain discoverable
 * from the canonical JAV inventory view.
 */
export default function JavEntityEmptyState({ entity = 'idol' }) {
  const [labelZh, labelEn] = ENTITY_LABELS[entity] || ENTITY_LABELS.idol

  return (
    <div
      role="status"
      data-testid={`jav-${entity}-empty-state`}
      className="flex min-h-[200px] flex-col items-center justify-center gap-3 rounded border border-dashed border-gray-200 px-4 text-center text-gray-500"
    >
      <p>
        {zh(
          `暂无可展示的${labelZh}。未入库作品也会参与聚合；此状态通常表示作品的${labelZh}元数据仍待补全。`,
          `No ${labelEn} data is visible yet. Pending works are included, so their ${labelEn} metadata may still need to be completed.`
        )}
      </p>
      <a
        href="?view=jav&inventory=pending&page=1"
        className="rounded border border-blue-200 bg-blue-50 px-3 py-1.5 text-sm font-semibold text-blue-700 transition hover:border-blue-300 hover:bg-blue-100"
      >
        {zh('查看未入库作品', 'View pending works')}
      </a>
    </div>
  )
}
