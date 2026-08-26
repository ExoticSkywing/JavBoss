import { zh } from '@/utils/i18n'

const ENTITY_LABELS = {
  idol: ['女优', 'idol'],
  studio: ['片商', 'studio'],
  series: ['系列', 'series'],
}

/**
 * Explains why metadata entities can be present before a work has a file.
 * Keeping this beside the entity index prevents a zero-file library from
 * being mistaken for a failed metadata scrape.
 */
export default function JavEntityScopeNotice({ entity = 'idol' }) {
  const [labelZh, labelEn] = ENTITY_LABELS[entity] || ENTITY_LABELS.idol

  return (
    <div
      role="note"
      className="mb-3 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border border-blue-100 bg-blue-50 px-3 py-2 text-xs text-blue-900"
    >
      <span className="font-semibold">
        {zh(`${labelZh}是元数据视图`, `${labelEn} metadata view`)}
      </span>
      <span className="text-blue-800">
        {zh(
          '按作品元数据聚合，未入库作品也会显示；卡片分列待入库/已入库数量。缺失字段会在作品卡标记“待补全”，不会伪造未知实体。',
          'Aggregated from visible work metadata, including pending imports. Cards split pending/imported counts; missing fields stay marked as pending on the work instead of creating fake unknown entities.'
        )}
      </span>
    </div>
  )
}
