import { useCallback, useEffect, useState } from 'react'
import DownloadOutlinedIcon from '@mui/icons-material/DownloadOutlined'
import InsightsOutlinedIcon from '@mui/icons-material/InsightsOutlined'
import RefreshRoundedIcon from '@mui/icons-material/RefreshRounded'
import AppModal from '@/components/AppModal'
import { fetchJavMagnetSamples } from '@/api'
import { zh } from '@/utils/i18n'

const PAGE_SIZE = 40
const EXPORT_PAGE_SIZE = 500

const STATUS_OPTIONS = [
  { value: 'all', label: zh('全部样本', 'All samples') },
  { value: 'accepted', label: zh('已通过', 'Accepted') },
  { value: 'rejected', label: zh('不合格', 'Rejected') },
]

const SORT_OPTIONS = [
  { value: 'accepted_at', label: zh('验收时间', 'Review date') },
  { value: 'size', label: zh('文件大小', 'Size') },
  { value: 'files', label: zh('文件数', 'Files') },
  { value: 'code', label: zh('番号', 'Code') },
]

function formatSize(sizeMiB) {
  const value = Number(sizeMiB) || 0
  if (value >= 1024) return `${(value / 1024).toFixed(value >= 10240 ? 0 : 1)} GiB`
  return `${Math.round(value)} MiB`
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value).slice(0, 10)
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
}

function csvValue(value) {
  const text = value == null ? '' : String(value)
  return `"${text.replaceAll('"', '""')}"`
}

function downloadCSV(items) {
  const headers = [
    '番号',
    '标题',
    '状态',
    '磁链名称',
    'Hash',
    '大小(MiB)',
    '文件数',
    'HD',
    '中字',
    '清晰度达标',
    '确认1080P',
    '片头广告',
    '水印',
    '跑马灯',
    '无码',
    '来源日期',
    '验收日期',
    '不合格原因',
    '备注',
  ]
  const rows = items.map((item) => [
    item.code,
    item.title,
    item.review_status === 'accepted' ? '已通过' : '不合格',
    item.name,
    item.info_hash,
    item.size_mib,
    item.files,
    item.hd ? '是' : '否',
    item.cnsub ? '是' : '否',
    item.quality_clear == null ? '' : item.quality_clear ? '是' : '否',
    item.confirmed_1080p == null ? '' : item.confirmed_1080p ? '是' : '否',
    item.has_intro_ad == null ? '' : item.has_intro_ad ? '是' : '否',
    item.has_watermark == null ? '' : item.has_watermark ? '是' : '否',
    item.has_marquee == null ? '' : item.has_marquee ? '是' : '否',
    item.is_uncensored == null ? '' : item.is_uncensored ? '是' : '否',
    item.source_created_at,
    formatDate(item.accepted_at || item.reviewed_at),
    item.review_reasons,
    item.review_notes,
  ])
  const csv = `\uFEFF${[headers, ...rows].map((row) => row.map(csvValue).join(',')).join('\n')}`
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `jav-magnet-samples-${new Date().toISOString().slice(0, 10)}.csv`
  anchor.click()
  URL.revokeObjectURL(url)
}

function Fact({ children, tone = 'neutral' }) {
  return (
    <span className={`jav-magnet-sample-fact jav-magnet-sample-fact--${tone}`}>{children}</span>
  )
}

function BoolFact({ value, yes, no, trueTone = 'neutral', falseTone = 'muted' }) {
  if (value == null) return null
  return <Fact tone={value ? trueTone : falseTone}>{value ? yes : no}</Fact>
}

function SampleRow({ item }) {
  const accepted = item.review_status === 'accepted'
  return (
    <article className="jav-magnet-sample-row">
      <div className="jav-magnet-sample-row__main">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="jav-magnet-sample-code">{item.code || '—'}</span>
          <span className={`jav-magnet-sample-status ${accepted ? 'is-accepted' : 'is-rejected'}`}>
            {accepted ? zh('已通过', 'Accepted') : zh('不合格', 'Rejected')}
          </span>
        </div>
        <div className="jav-magnet-sample-title" title={item.title || ''}>
          {item.title || zh('未命名作品', 'Untitled work')}
        </div>
        <div className="jav-magnet-sample-name" title={item.name || item.info_hash}>
          {item.name || item.info_hash || zh('未命名磁链', 'Unnamed magnet')}
        </div>
        <div
          className="jav-magnet-sample-facts"
          aria-label={zh('磁链质量事实', 'Magnet quality facts')}
        >
          <Fact tone="strong">{formatSize(item.size_mib)}</Fact>
          <Fact>{zh(`${item.files || 0} 个文件`, `${item.files || 0} files`)}</Fact>
          {item.hd ? <Fact tone="strong">HD</Fact> : null}
          {item.cnsub ? <Fact>中字</Fact> : null}
          <BoolFact
            value={item.quality_clear}
            yes="清晰度达标"
            no="清晰度不达标"
            trueTone="positive"
            falseTone="negative"
          />
          <BoolFact
            value={item.confirmed_1080p}
            yes="1080P"
            no="非1080P"
            trueTone="positive"
            falseTone="negative"
          />
          <BoolFact value={item.is_uncensored} yes="无码" no="有码" trueTone="accent" />
          <BoolFact
            value={item.has_intro_ad}
            yes="有片头广告"
            no="无片头广告"
            trueTone="negative"
            falseTone="positive"
          />
          <BoolFact
            value={item.has_watermark}
            yes="有水印"
            no="无水印"
            trueTone="negative"
            falseTone="positive"
          />
          <BoolFact
            value={item.has_marquee}
            yes="有跑马灯"
            no="无跑马灯"
            trueTone="negative"
            falseTone="positive"
          />
        </div>
        {item.review_status === 'rejected' && item.review_reasons ? (
          <div className="jav-magnet-sample-reason">
            {zh('原因：', 'Reason: ')}
            {item.review_reasons}
          </div>
        ) : null}
      </div>
      <div className="jav-magnet-sample-row__meta">
        <div>
          <span className="jav-magnet-sample-meta-label">{zh('验收', 'Reviewed')}</span>
          <span>{formatDate(item.accepted_at || item.reviewed_at)}</span>
        </div>
        {item.source_created_at ? (
          <div>
            <span className="jav-magnet-sample-meta-label">{zh('来源', 'Source')}</span>
            <span>{item.source_created_at}</span>
          </div>
        ) : null}
        <div className="jav-magnet-sample-hash" title={item.info_hash}>
          {item.info_hash || '—'}
        </div>
      </div>
    </article>
  )
}

export default function JavMagnetSamplesButton() {
  const [open, setOpen] = useState(false)
  const [status, setStatus] = useState('accepted')
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [sort, setSort] = useState('accepted_at')
  const [direction, setDirection] = useState('desc')
  const [offset, setOffset] = useState(0)
  const [items, setItems] = useState([])
  const [total, setTotal] = useState(0)
  const [stats, setStats] = useState({})
  const [overallStats, setOverallStats] = useState({})
  const [loading, setLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [error, setError] = useState('')

  const loadOverall = useCallback(async () => {
    try {
      const payload = await fetchJavMagnetSamples({ status: 'all', limit: 1, offset: 0 })
      setOverallStats(payload?.stats || {})
    } catch {
      // The modal request below shows the actionable error if the API is unavailable.
    }
  }, [])

  const loadSamples = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const payload = await fetchJavMagnetSamples({
        status,
        search,
        sort,
        direction,
        limit: PAGE_SIZE,
        offset,
      })
      setItems(Array.isArray(payload?.items) ? payload.items : [])
      setTotal(Number(payload?.total) || 0)
      setStats(payload?.stats || {})
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setLoading(false)
    }
  }, [direction, offset, search, sort, status])

  useEffect(() => {
    void loadOverall()
  }, [loadOverall])

  useEffect(() => {
    if (!open) return
    void loadSamples()
    void loadOverall()
  }, [loadOverall, loadSamples, open])

  const sampleTotal = Number(overallStats.total) || 0

  const page = Math.floor(offset / PAGE_SIZE) + 1
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const applySearch = (event) => {
    event.preventDefault()
    const nextSearch = searchInput.trim()
    if (nextSearch === search && offset === 0) {
      void loadSamples()
      return
    }
    setSearch(nextSearch)
    setOffset(0)
  }

  const changeStatus = (nextStatus) => {
    setStatus(nextStatus)
    setOffset(0)
  }

  const exportSamples = async () => {
    if (exporting || total === 0) return
    setExporting(true)
    setError('')
    try {
      const allItems = []
      for (let nextOffset = 0; nextOffset < total; nextOffset += EXPORT_PAGE_SIZE) {
        const payload = await fetchJavMagnetSamples({
          status,
          search,
          sort,
          direction,
          limit: EXPORT_PAGE_SIZE,
          offset: nextOffset,
        })
        const pageItems = Array.isArray(payload?.items) ? payload.items : []
        allItems.push(...pageItems)
        if (pageItems.length < EXPORT_PAGE_SIZE) break
      }
      downloadCSV(allItems)
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setExporting(false)
    }
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="inline-flex min-h-8 items-center gap-2 rounded-md border border-stone-300 bg-white px-3 text-xs font-semibold text-stone-700 transition hover:border-stone-400 hover:bg-stone-50 active:scale-[0.96]"
        aria-label={zh(`磁链样本 ${sampleTotal} 条`, `Magnet samples: ${sampleTotal}`)}
        title={zh(
          '集中查看已通过和不合格的磁链质量事实',
          'Compare accepted and rejected magnet facts'
        )}
      >
        <InsightsOutlinedIcon fontSize="inherit" />
        {zh('磁链样本', 'Magnet samples')}
        <span className="min-w-5 rounded-md bg-stone-700 px-1.5 py-0.5 text-center text-[10px] font-bold tabular-nums text-white">
          {sampleTotal}
        </span>
      </button>
      {open ? (
        <AppModal
          open
          onClose={() => setOpen(false)}
          ariaLabel={zh('磁链样本库', 'Magnet sample library')}
          contentClassName="max-h-[90vh] w-[min(1120px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-stone-50 shadow-2xl"
        >
          <div className="flex max-h-[90vh] flex-col">
            <header className="border-b border-stone-200 bg-white px-5 py-4 sm:px-6">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="text-lg font-semibold text-stone-950">
                    {zh('磁链样本库', 'Magnet sample library')}
                  </h2>
                  <p className="mt-1 max-w-3xl text-xs leading-5 text-stone-500">
                    {zh(
                      '这里只收录已经人工验收的磁链：已通过是正样本，不合格保留作对照。待筛选磁链仍在作品详情页处理。',
                      'Only human-reviewed magnets appear here: accepted items are positive samples, while rejected items remain as controls. Pending magnets stay in work details.'
                    )}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      void loadSamples()
                      void loadOverall()
                    }}
                    className="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-stone-300 bg-white px-2.5 text-xs font-semibold text-stone-700 hover:bg-stone-50"
                    title={zh('刷新样本', 'Refresh samples')}
                  >
                    <RefreshRoundedIcon fontSize="inherit" />
                    {zh('刷新', 'Refresh')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setOpen(false)}
                    className="rounded-lg px-2 py-1 text-sm text-stone-500 hover:bg-stone-100"
                  >
                    {zh('关闭', 'Close')}
                  </button>
                </div>
              </div>
            </header>

            <div className="min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
              {error ? (
                <div
                  role="alert"
                  className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-medium text-rose-800"
                >
                  {error}
                </div>
              ) : null}

              <div
                className="jav-magnet-sample-stats"
                aria-label={zh('样本统计', 'Sample statistics')}
              >
                <div>
                  <strong>{Number(stats.total) || 0}</strong>
                  <span>{zh('当前样本', 'Current')}</span>
                </div>
                <div>
                  <strong>{Number(stats.preferred_size) || 0}</strong>
                  <span>{zh('5–10 GiB', '5–10 GiB')}</span>
                </div>
                <div>
                  <strong>{Number(stats.confirmed_1080p) || 0}</strong>
                  <span>1080P</span>
                </div>
                <div>
                  <strong>{Number(stats.uncensored) || 0}</strong>
                  <span>{zh('无码', 'Uncensored')}</span>
                </div>
                <div>
                  <strong>{Number(stats.quality_clear) || 0}</strong>
                  <span>{zh('清晰度达标', 'Clear')}</span>
                </div>
                <div>
                  <strong>{Number(stats.intro_ad) || 0}</strong>
                  <span>{zh('片头广告', 'Intro ads')}</span>
                </div>
                <div>
                  <strong>{Number(stats.watermark) || 0}</strong>
                  <span>{zh('水印', 'Watermark')}</span>
                </div>
                <div>
                  <strong>{Number(stats.marquee) || 0}</strong>
                  <span>{zh('跑马灯', 'Marquee')}</span>
                </div>
              </div>

              <div className="mt-5 flex flex-col gap-3">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div
                    className="jav-magnet-sample-tabs"
                    role="group"
                    aria-label={zh('样本状态', 'Sample status')}
                  >
                    {STATUS_OPTIONS.map((option) => {
                      const active = status === option.value
                      const count =
                        option.value === 'accepted'
                          ? Number(overallStats.accepted) || 0
                          : option.value === 'rejected'
                            ? Number(overallStats.rejected) || 0
                            : Number(overallStats.total) || 0
                      return (
                        <button
                          key={option.value}
                          type="button"
                          aria-pressed={active}
                          onClick={() => changeStatus(option.value)}
                          className={active ? 'is-active' : ''}
                        >
                          {option.label}
                          <span>{count}</span>
                        </button>
                      )
                    })}
                  </div>
                  <button
                    type="button"
                    onClick={() => void exportSamples()}
                    disabled={exporting || total === 0}
                    className="inline-flex min-h-8 items-center gap-1.5 rounded-md border border-stone-300 bg-white px-2.5 text-xs font-semibold text-stone-700 hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <DownloadOutlinedIcon fontSize="inherit" />
                    {exporting
                      ? zh('导出中…', 'Exporting…')
                      : zh('导出当前筛选', 'Export filtered')}
                  </button>
                </div>

                <form
                  onSubmit={applySearch}
                  className="flex flex-wrap items-center gap-2 rounded-xl border border-stone-200 bg-white p-3"
                >
                  <label className="sr-only" htmlFor="jav-magnet-sample-search">
                    {zh('搜索磁链样本', 'Search magnet samples')}
                  </label>
                  <input
                    id="jav-magnet-sample-search"
                    name="magnet-sample-search"
                    type="search"
                    autoComplete="off"
                    value={searchInput}
                    onChange={(event) => setSearchInput(event.target.value)}
                    placeholder={zh(
                      '搜索番号、标题、磁链名称或 Hash',
                      'Search code, title, name or hash'
                    )}
                    className="min-h-9 min-w-[220px] flex-1 rounded-md border border-stone-300 bg-white px-3 text-xs text-stone-900 outline-none placeholder:text-stone-400 focus:border-stone-600 focus:ring-2 focus:ring-stone-200"
                  />
                  <button
                    type="submit"
                    className="min-h-9 rounded-md bg-stone-800 px-3.5 text-xs font-semibold text-white hover:bg-stone-900"
                  >
                    {zh('搜索', 'Search')}
                  </button>
                  <label className="flex min-h-9 items-center gap-2 rounded-md border border-stone-300 bg-white px-2.5 text-xs text-stone-600">
                    <span>{zh('排序', 'Sort')}</span>
                    <select
                      value={sort}
                      onChange={(event) => {
                        setSort(event.target.value)
                        setOffset(0)
                      }}
                      className="bg-transparent font-semibold text-stone-800 outline-none"
                    >
                      {SORT_OPTIONS.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button
                    type="button"
                    onClick={() => {
                      setDirection((current) => (current === 'desc' ? 'asc' : 'desc'))
                      setOffset(0)
                    }}
                    className="min-h-9 rounded-md border border-stone-300 bg-white px-3 text-xs font-semibold text-stone-700 hover:bg-stone-50"
                    title={zh('切换排序方向', 'Toggle sort direction')}
                  >
                    {direction === 'desc'
                      ? zh('从新到旧 ↓', 'Newest ↓')
                      : zh('从旧到新 ↑', 'Oldest ↑')}
                  </button>
                </form>
              </div>

              {loading ? (
                <div className="py-12 text-center text-sm text-stone-500">
                  {zh('加载样本中…', 'Loading samples...')}
                </div>
              ) : null}
              {!loading && items.length === 0 ? (
                <div className="mt-4 rounded-xl border border-dashed border-stone-300 bg-white px-4 py-12 text-center text-sm text-stone-500">
                  {search
                    ? zh('当前筛选没有匹配样本。', 'No samples match the current filter.')
                    : zh('还没有已验收磁链。', 'No reviewed magnets yet.')}
                </div>
              ) : null}
              {!loading && items.length > 0 ? (
                <div className="mt-4 space-y-3">
                  {items.map((item) => (
                    <SampleRow key={item.id} item={item} />
                  ))}
                </div>
              ) : null}
            </div>

            <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-stone-200 bg-white px-5 py-3 sm:px-6">
              <span className="text-xs text-stone-500">
                {zh(
                  `共 ${total} 条 · 第 ${page}/${pageCount} 页`,
                  `${total} samples · Page ${page}/${pageCount}`
                )}
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setOffset((current) => Math.max(0, current - PAGE_SIZE))}
                  disabled={offset === 0 || loading}
                  className="min-h-8 rounded-md border border-stone-300 bg-white px-3 text-xs font-semibold text-stone-700 hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-45"
                >
                  {zh('上一页', 'Previous')}
                </button>
                <button
                  type="button"
                  onClick={() => setOffset((current) => current + PAGE_SIZE)}
                  disabled={offset + PAGE_SIZE >= total || loading}
                  className="min-h-8 rounded-md border border-stone-300 bg-white px-3 text-xs font-semibold text-stone-700 hover:bg-stone-50 disabled:cursor-not-allowed disabled:opacity-45"
                >
                  {zh('下一页', 'Next')}
                </button>
              </div>
            </footer>
          </div>
        </AppModal>
      ) : null}
    </>
  )
}
