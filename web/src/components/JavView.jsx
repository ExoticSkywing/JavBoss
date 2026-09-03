import SwapVertIcon from '@mui/icons-material/SwapVert'
import { Popover } from '@mui/material'
import { useCallback, useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import JavGrid from '@/components/JavGrid'
import AppModal from '@/components/AppModal'
import Pagination from '@/components/Pagination'
import WaterfallLoader from '@/components/WaterfallLoader'
import {
  fetchJavImportDays,
  fetchJavMagnetQueue,
  fetchJavQualityReviewQueue,
  executeJavQualityReviewBatch,
  submitJavDownloadBatch,
} from '@/api'
import {
  JAV_INVENTORY_IMPORTED,
  JAV_INVENTORY_PENDING,
  JAV_SORT_OPTIONS,
  JAV_VIEW_PRESET_COMPACT,
  JAV_VIEW_PRESET_DETAILED,
  findSortOption,
  normalizeJavInventory,
  normalizeJavViewPreset,
  reverseSortValue,
  sortLabelParts,
} from '@/constants/jav'
import { zh } from '@/utils/i18n'

function SortText({ option, value, className = '' }) {
  const parts = sortLabelParts(option, value, zh)

  return (
    <span className={`truncate font-semibold ${className}`}>
      <span>{parts.label}</span>
      <span className="font-normal text-gray-500">{parts.separator}</span>
      <span className="font-normal text-gray-500">{parts.direction}</span>
    </span>
  )
}

function JavViewPresetSwitch({ value, onChange }) {
  const selected = normalizeJavViewPreset(value)
  const options = [
    { value: JAV_VIEW_PRESET_DETAILED, label: zh('完整', 'Detailed') },
    { value: JAV_VIEW_PRESET_COMPACT, label: zh('速览', 'Compact') },
  ]

  return (
    <div className="flex shrink-0 items-center gap-1.5">
      <span className="text-xs font-medium text-slate-500">{zh('视图', 'View')}</span>
      <div
        className="inline-flex rounded-lg border border-slate-200 bg-slate-100 p-0.5"
        role="group"
        aria-label={zh('作品视图', 'Work view')}
      >
        {options.map((option) => {
          const active = selected === option.value
          return (
            <button
              key={option.value}
              type="button"
              className={`min-h-8 rounded-md px-2.5 text-xs font-semibold transition-[color,background-color,box-shadow,transform] duration-150 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-stone-600 active:scale-[0.96] ${
                active
                  ? 'bg-white text-stone-800 shadow-sm'
                  : 'text-slate-500 hover:bg-white/70 hover:text-slate-800'
              }`}
              aria-pressed={active}
              onClick={() => onChange?.(option.value)}
            >
              {option.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function JavMagnetQueueButton() {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState([])
  const [total, setTotal] = useState(0)
  const [selectedIDs, setSelectedIDs] = useState(new Set())
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const loadQueue = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const payload = await fetchJavMagnetQueue()
      const nextItems = Array.isArray(payload?.items) ? payload.items : []
      setItems(nextItems)
      setTotal(Number(payload?.total) || 0)
      setSelectedIDs((current) => {
        const available = new Set(nextItems.map((entry) => Number(entry?.jav?.id)))
        return new Set([...current].filter((id) => available.has(id)))
      })
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadQueue()
  }, [loadQueue])

  useEffect(() => {
    if (open) void loadQueue()
  }, [open, loadQueue])

  const toggle = (id) => {
    setSelectedIDs((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const allSelected =
    items.length > 0 && items.every((entry) => selectedIDs.has(Number(entry?.jav?.id)))

  const toggleAll = () => {
    setSelectedIDs((current) => {
      const available = items.map((entry) => Number(entry?.jav?.id))
      const everySelected = available.length > 0 && available.every((id) => current.has(id))
      return everySelected ? new Set() : new Set(available)
    })
  }

  const submit = async () => {
    if (selectedIDs.size === 0 || submitting) return
    setSubmitting(true)
    setError('')
    setMessage('')
    try {
      const result = await submitJavDownloadBatch([...selectedIDs])
      setMessage(
        result?.delivery_status === 'submitted'
          ? zh('已提交云下载。', 'Submitted to cloud download.')
          : result?.delivery_status === 'partial'
            ? zh(
                '部分任务已提交，失败项仍留在待发送队列。',
                'Some tasks were submitted; failed items remain queued.'
              )
            : zh(
                '云下载服务未接收任务，选择仍留在待发送队列。',
                'The cloud downloader did not accept the tasks; selections remain queued.'
              )
      )
      setSelectedIDs(new Set())
      await loadQueue()
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={zh(`待发送 ${total} 部作品`, `Send queue: ${total} works`)}
        className="inline-flex min-h-8 items-center gap-2 rounded-md border border-stone-300 bg-stone-50 px-3 text-xs font-semibold text-stone-700 transition hover:border-stone-400 hover:bg-stone-100 active:scale-[0.96]"
        title={zh(
          '查看已保存的磁链选择并批量提交',
          'Review saved magnet choices and submit them in a batch'
        )}
      >
        {zh('待发送', 'Send queue')}
        <span className="min-w-5 rounded-md bg-stone-800 px-1.5 py-0.5 text-center text-[10px] font-bold tabular-nums text-white">
          {total}
        </span>
      </button>
      {open ? (
        <AppModal
          open
          onClose={() => setOpen(false)}
          ariaLabel={zh('磁链待发送队列', 'Magnet send queue')}
          contentClassName="max-h-[86vh] w-[min(720px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-white shadow-2xl"
        >
          <div className="flex max-h-[86vh] flex-col">
            <header className="flex items-start justify-between gap-3 border-b border-slate-200 px-5 py-4">
              <div>
                <h2 className="text-lg font-semibold text-slate-950">
                  {zh('磁链待发送', 'Magnet send queue')}
                </h2>
                <p className="mt-1 text-xs leading-5 text-slate-500">
                  {zh(
                    '这里汇总详情页中已保存、但尚未提交下载的选择。',
                    'Saved choices from detail pages that have not been submitted yet.'
                  )}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg px-2 py-1 text-sm text-slate-500 hover:bg-slate-100"
              >
                {zh('关闭', 'Close')}
              </button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              {loading ? (
                <div className="py-10 text-center text-sm text-slate-500">
                  {zh('加载中…', 'Loading...')}
                </div>
              ) : null}
              {!loading && items.length === 0 ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">
                  {zh(
                    '暂无待发送磁链。先在作品详情页保存磁链选择。',
                    'No saved magnets are waiting. Save a choice from a work detail first.'
                  )}
                </div>
              ) : null}
              {!loading && items.length > 0 ? (
                <>
                  <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-stone-200 bg-stone-50 px-3 py-2">
                    <span className="text-xs text-stone-600">
                      {zh(
                        `已选择 ${selectedIDs.size} / ${items.length} 部作品`,
                        `${selectedIDs.size} / ${items.length} work(s) selected`
                      )}
                    </span>
                    <button
                      type="button"
                      onClick={toggleAll}
                      aria-pressed={allSelected}
                      className="min-h-8 rounded-md border border-stone-300 bg-white px-3 py-1.5 text-xs font-semibold text-stone-700 transition hover:border-stone-400 hover:bg-stone-100 active:scale-[0.96]"
                    >
                      {allSelected ? zh('取消全选', 'Clear all') : zh('全选', 'Select all')}
                    </button>
                  </div>
                  <div className="space-y-2">
                    {items.map((entry) => {
                      const id = Number(entry?.jav?.id)
                      const checked = selectedIDs.has(id)
                      return (
                        <label
                          key={id}
                          className={`flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-3 ${checked ? 'border-stone-400 bg-stone-100' : 'border-slate-200 bg-white hover:border-slate-300'}`}
                        >
                          <input
                            aria-label={zh(
                              `选择 ${entry?.jav?.code || '作品'}`,
                              `Select ${entry?.jav?.code || 'work'}`
                            )}
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggle(id)}
                            className="mt-1 h-4 w-4 accent-stone-700"
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block text-sm font-semibold text-slate-900">
                              {entry?.jav?.code || '—'}{' '}
                              <span className="font-normal">{entry?.jav?.title || ''}</span>
                            </span>
                            <span className="mt-1 block truncate text-xs text-slate-500">
                              {entry?.candidate?.name || entry?.candidate?.info_hash}
                            </span>
                            {entry?.attempt?.status === 'uncertain' ? (
                              <span className="mt-1 block text-[11px] font-medium text-amber-700">
                                {zh(
                                  '上次发送结果未知；重试会复用幂等键',
                                  'Previous result unknown; retry reuses the idempotency key'
                                )}
                              </span>
                            ) : entry?.attempt?.status === 'failed' ? (
                              <span className="mt-1 block text-[11px] font-medium text-rose-700">
                                {zh(
                                  '上次发送失败，可安全重试',
                                  'Previous submission failed; safe to retry'
                                )}
                              </span>
                            ) : null}
                          </span>
                        </label>
                      )
                    })}
                  </div>
                </>
              ) : null}
              {error ? (
                <div
                  role="alert"
                  className="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700"
                >
                  {error}
                </div>
              ) : null}
              {message ? (
                <div
                  role="status"
                  className="mt-3 rounded-lg border border-stone-200 bg-stone-50 px-3 py-2 text-xs text-stone-700"
                >
                  {message}
                </div>
              ) : null}
            </div>
            <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 px-5 py-4">
              <span className="text-xs text-slate-500">
                {zh(`已选择 ${selectedIDs.size} 部作品`, `${selectedIDs.size} work(s) selected`)}
              </span>
              <button
                type="button"
                onClick={submit}
                disabled={selectedIDs.size === 0 || submitting}
                className="min-h-10 rounded-lg bg-stone-800 px-4 text-sm font-semibold text-white hover:bg-stone-900 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {submitting ? zh('提交中…', 'Submitting…') : zh('批量提交下载', 'Submit selected')}
              </button>
            </footer>
          </div>
        </AppModal>
      ) : null}
    </>
  )
}

function JavImportHistoryButton({ directoryIds = [], gridProps = {} }) {
  const [open, setOpen] = useState(false)
  const [days, setDays] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const loadDays = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const payload = await fetchJavImportDays({ limit: 31, directoryIds })
      setDays(Array.isArray(payload?.items) ? payload.items : [])
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setLoading(false)
    }
  }, [directoryIds])

  useEffect(() => {
    void loadDays()
  }, [loadDays])

  useEffect(() => {
    if (open) void loadDays()
  }, [open, loadDays])

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={zh(`入库记录 ${days.length} 天`, `Import history: ${days.length} days`)}
        className="inline-flex min-h-8 items-center gap-2 rounded-md border border-stone-300 bg-white px-3 text-xs font-semibold text-stone-700 transition hover:border-stone-400 hover:bg-stone-50 active:scale-[0.96]"
        title={zh('按验收日期查看正式入库记录', 'View accepted imports by review date')}
      >
        {zh('入库记录', 'Import history')}
        <span className="min-w-5 rounded-md bg-stone-600 px-1.5 py-0.5 text-center text-[10px] font-bold tabular-nums text-white">
          {days.length}
        </span>
      </button>
      {open ? (
        <AppModal
          open
          onClose={() => setOpen(false)}
          ariaLabel={zh('每日入库记录', 'Daily import history')}
          contentClassName="max-h-[86vh] w-[min(720px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-white shadow-2xl"
        >
          <div className="flex max-h-[86vh] flex-col">
            <header className="flex items-start justify-between gap-3 border-b border-slate-200 px-5 py-4">
              <div>
                <h2 className="text-lg font-semibold text-slate-950">
                  {zh('每日入库记录', 'Daily import history')}
                </h2>
                <p className="mt-1 text-xs leading-5 text-slate-500">
                  {zh(
                    '只记录质量验收通过的作品；没有验收作品的日期不会显示。',
                    'Only quality-accepted works are recorded; empty dates are omitted.'
                  )}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg px-2 py-1 text-sm text-slate-500 hover:bg-slate-100"
              >
                {zh('关闭', 'Close')}
              </button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              {loading ? (
                <div className="py-10 text-center text-sm text-slate-500">
                  {zh('加载中…', 'Loading...')}
                </div>
              ) : null}
              {!loading && days.length === 0 ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">
                  {zh('还没有正式入库记录。', 'No accepted import records yet.')}
                </div>
              ) : null}
              {!loading && days.length > 0 ? (
                <div className="space-y-3">
                  {days.map((entry) => (
                    <section
                      key={entry.day}
                      className="rounded-xl border border-slate-200 bg-slate-50/70 p-3"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="text-sm font-semibold text-slate-900">{entry.day}</h3>
                        <span className="rounded-md bg-stone-200 px-2 py-0.5 text-xs font-semibold text-stone-700">
                          {zh(`${entry.count || 0} 部`, `${entry.count || 0} work(s)`)}
                        </span>
                      </div>
                      <div className="mt-3">
                        <JavGrid
                          {...gridProps}
                          items={entry.items || []}
                          emptyMessage={zh('当日没有作品', 'No works on this day')}
                        />
                      </div>
                    </section>
                  ))}
                </div>
              ) : null}
              {error ? (
                <div
                  role="alert"
                  className="mt-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700"
                >
                  {error}
                </div>
              ) : null}
            </div>
          </div>
        </AppModal>
      ) : null}
    </>
  )
}

function JavQualityReviewQueueButton({ directoryIds = [], gridProps = {} }) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [executing, setExecuting] = useState(false)
  const [message, setMessage] = useState('')

  const loadItems = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const payload = await fetchJavQualityReviewQueue({ limit: 50, directoryIds })
      setItems(Array.isArray(payload?.items) ? payload.items : [])
      setTotal(Number(payload?.total) || 0)
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setLoading(false)
    }
  }, [directoryIds])

  useEffect(() => {
    void loadItems()
  }, [loadItems])

  useEffect(() => {
    if (open) void loadItems()
  }, [open, loadItems])

  const decidedItems = items.filter((item) => {
    const decision = String(item?.quality_review?.decision || '').trim()
    return decision === 'accepted' || decision === 'rejected'
  })
  const approvedCount = decidedItems.filter(
    (item) => item?.quality_review?.decision === 'accepted'
  ).length
  const rejectedCount = decidedItems.length - approvedCount

  const executeDecisions = async () => {
    const attemptIds = decidedItems
      .map((item) =>
        Number(item?.quality_review?.attempt_id || item?.download_attempt?.id || item?.attempt?.id)
      )
      .filter((id) => Number.isFinite(id) && id > 0)
    if (executing) return
    if (attemptIds.length === 0) {
      setError(
        zh(
          '验收决定缺少下载任务编号，请刷新待验收列表后重试。',
          'The saved review decisions have no download task IDs. Refresh the queue and try again.'
        )
      )
      return
    }
    setExecuting(true)
    setError('')
    setMessage(
      zh(
        '正在清扫通过作品并执行验收，请稍候…',
        'Cleaning approved works and executing the review batch…'
      )
    )
    try {
      const result = await executeJavQualityReviewBatch(attemptIds)
      const cleanup = result?.cleanup || {}
      const cleanedFiles = Number(cleanup.files_deleted) || 0
      const cleanedFolders = Number(cleanup.folders_deleted) || 0
      setMessage(
        zh(
          `已批量执行 ${Number(result?.count) || attemptIds.length} 项验收；通过 ${approvedCount} 部，不合格 ${rejectedCount} 部。执行前清扫删除文件 ${cleanedFiles} 个、空目录 ${cleanedFolders} 个。`,
          `Executed ${Number(result?.count) || attemptIds.length} reviews; ${approvedCount} approved, ${rejectedCount} rejected. Pre-promotion cleanup removed ${cleanedFiles} file(s) and ${cleanedFolders} empty directorie(s).`
        )
      )
      await loadItems()
    } catch (requestError) {
      setError(requestError?.message || String(requestError))
    } finally {
      setExecuting(false)
    }
  }

  const queueGridProps = {
    ...gridProps,
    onAcquisitionUpdated: (updated, sourceItem) => {
      const sourceID = Number(sourceItem?.id)
      setItems((current) =>
        current.map((item) => (Number(item?.id) === sourceID ? { ...item, ...updated } : item))
      )
      gridProps.onAcquisitionUpdated?.(updated, sourceItem)
    },
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={zh(`待验收 ${total} 部作品`, `Quality review: ${total} works`)}
        className="inline-flex min-h-8 items-center gap-2 rounded-md border border-orange-300 bg-orange-50 px-3 text-xs font-semibold text-orange-900 transition hover:border-orange-400 hover:bg-orange-100 active:scale-[0.96]"
        title={zh(
          '查看文件已落盘、但尚未通过人工质量验收的作品',
          'View downloaded works awaiting human quality review'
        )}
      >
        {zh('待验收', 'Quality review')}
        <span className="min-w-5 rounded-md bg-orange-700 px-1.5 py-0.5 text-center text-[10px] font-bold tabular-nums text-white">
          {total}
        </span>
      </button>
      {open ? (
        <AppModal
          open
          onClose={() => setOpen(false)}
          ariaLabel={zh('待质量验收作品', 'Works awaiting quality review')}
          contentClassName="max-h-[86vh] w-[min(900px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-white shadow-2xl"
        >
          <div className="flex max-h-[86vh] flex-col">
            <header className="flex items-start justify-between gap-3 border-b border-slate-200 px-5 py-4">
              <div>
                <h2 className="text-lg font-semibold text-slate-950">
                  {zh('待质量验收', 'Quality review queue')}
                </h2>
                <p className="mt-1 text-xs leading-5 text-slate-500">
                  {zh(
                    '这些作品已经扫到真实文件，但还不是正式入库。打开详情核验磁链质量。',
                    'These works have physical files but are not formal imports yet. Open a detail view to verify magnet quality.'
                  )}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg px-2 py-1 text-sm text-slate-500 hover:bg-slate-100"
              >
                {zh('关闭', 'Close')}
              </button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-5" aria-busy={executing}>
              {error ? (
                <div
                  role="alert"
                  className="mb-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-medium text-rose-700"
                >
                  {error}
                </div>
              ) : null}
              {message ? (
                <div
                  role="status"
                  className={`mb-3 rounded-lg border px-3 py-2 text-xs font-medium ${executing ? 'border-amber-200 bg-amber-50 text-amber-900' : 'border-stone-200 bg-stone-50 text-stone-700'}`}
                >
                  {message}
                </div>
              ) : null}
              {loading ? (
                <div className="py-10 text-center text-sm text-slate-500">
                  {zh('加载中…', 'Loading...')}
                </div>
              ) : null}
              {!loading && items.length === 0 ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">
                  {zh('当前没有悬挂待验收的作品。', 'No works are currently awaiting review.')}
                </div>
              ) : null}
              {!loading && items.length > 0 ? (
                <>
                  <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50/70 px-4 py-3">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-semibold text-amber-950">
                          {zh(
                            `已记录 ${decidedItems.length} / ${items.length} 部验收决定`,
                            `${decidedItems.length} / ${items.length} review decisions saved`
                          )}
                        </div>
                        <div className="mt-1 text-xs text-amber-800">
                          {zh(
                            `通过 ${approvedCount} 部 · 不合格 ${rejectedCount} 部 · 其余待判断`,
                            `${approvedCount} approved · ${rejectedCount} rejected · the rest need review`
                          )}
                        </div>
                      </div>
                      <button
                        type="button"
                        onClick={() => void executeDecisions()}
                        disabled={decidedItems.length === 0 || executing}
                        aria-busy={executing}
                        className="min-h-9 rounded-lg bg-stone-800 px-3.5 text-xs font-semibold text-white transition hover:bg-stone-900 active:scale-[0.96] disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {executing ? (
                          <span
                            aria-hidden="true"
                            className="mr-1.5 inline-block h-3 w-3 animate-spin rounded-full border-2 border-white/40 border-t-white align-[-2px]"
                          />
                        ) : null}
                        {executing
                          ? zh('批量执行中…', 'Executing…')
                          : decidedItems.length === 1
                            ? zh('执行 1 项验收', 'Execute 1 review')
                            : zh(
                                `批量执行 ${decidedItems.length} 项验收`,
                                `Execute ${decidedItems.length} reviews`
                              )}
                      </button>
                    </div>
                    {decidedItems.length === 0 ? (
                      <div className="mt-2 text-xs text-amber-800">
                        {zh(
                          '打开作品详情，记录“通过”或“不合格”；记录完成后会在这里一次性执行。',
                          'Open each detail, record approval or rejection, then execute the decisions here in one batch.'
                        )}
                      </div>
                    ) : null}
                  </div>
                  <JavGrid
                    {...queueGridProps}
                    items={items}
                    emptyMessage={zh('暂无待验收作品', 'No works awaiting review')}
                  />
                </>
              ) : null}
            </div>
          </div>
        </AppModal>
      ) : null}
    </>
  )
}

function useJavViewPresetTransition(value, onChange) {
  const activeTransitionRef = useRef(null)
  const transitionCardsRef = useRef([])
  const transitionGenerationRef = useRef(0)

  const clearCardTransitionNames = useCallback(() => {
    transitionCardsRef.current.forEach((card) => {
      card.style.removeProperty('view-transition-name')
    })
    transitionCardsRef.current = []
  }, [])

  useEffect(
    () => () => {
      transitionGenerationRef.current += 1
      activeTransitionRef.current?.skipTransition?.()
      clearCardTransitionNames()
      document.documentElement.classList.remove('jav-view-preset-transition')
    },
    [clearCardTransitionNames]
  )

  return useCallback(
    (nextValue) => {
      const next = normalizeJavViewPreset(nextValue)
      if (next === normalizeJavViewPreset(value)) return

      const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
      if (reduceMotion || typeof document.startViewTransition !== 'function') {
        onChange?.(next)
        return
      }

      const generation = (transitionGenerationRef.current += 1)
      activeTransitionRef.current?.skipTransition?.()
      clearCardTransitionNames()

      const viewportHeight = window.innerHeight
      const current = normalizeJavViewPreset(value)
      const lowerCaptureBoundary = viewportHeight * (current === JAV_VIEW_PRESET_DETAILED ? 2 : 1.2)
      transitionCardsRef.current = Array.from(
        document.querySelectorAll('[data-jav-view-transition-card]')
      ).filter((card) => {
        const bounds = card.getBoundingClientRect()
        return bounds.bottom >= -viewportHeight * 0.2 && bounds.top <= lowerCaptureBoundary
      })
      transitionCardsRef.current.forEach((card, index) => {
        card.style.viewTransitionName = `jav-card-${index}`
      })

      document.documentElement.classList.add('jav-view-preset-transition')

      const transition = document.startViewTransition(() => {
        flushSync(() => onChange?.(next))
      })
      activeTransitionRef.current = transition
      void transition.finished
        .catch(() => {})
        .finally(() => {
          if (transitionGenerationRef.current !== generation) return
          activeTransitionRef.current = null
          clearCardTransitionNames()
          document.documentElement.classList.remove('jav-view-preset-transition')
        })
    },
    [clearCardTransitionNames, onChange, value]
  )
}

export default function JavView({
  javPage,
  javLastPage,
  javTotal,
  javHasPrev,
  javHasNext,
  javLoading,
  javInventory = 'all',
  javRandomMode,
  javResolvedSort,
  javSortSource,
  buildJavUrl,
  setJavPage,
  setJavTempSort,
  javItems,
  javGridColumns,
  javTitleMaxRows,
  javIdolTagMaxRows,
  javTagMaxRows,
  javViewPreset = JAV_VIEW_PRESET_DETAILED,
  directoryIds = [],
  onJavViewPresetChange,
  onPlay,
  onIdolClick,
  onOpenFavorites,
  onOpenJavFavorites,
  onOpenStudioFavorites,
  onOpenSeriesFavorites,
  onPrefixClick,
  onStudioClick,
  onSeriesClick,
  onTagClick,
  onOpenFile,
  openFileLabel,
  onRevealFile,
  onOpenScreenshots,
  onManageVideoPlay,
  onManageVideoPlayAtTime,
  onManageVideoCoverChanged,
  onManageVideoOpenFile,
  onManageVideoRevealFile,
  onManageVideoOpenTagPicker,
  onManageVideoOpenScreenshots,
  onManageVideoOpenScrapeSettings,
  onManageVideoRename,
  onManageVideoDelete,
  onManageVideoTagClick,
  waterfallMode,
  onWaterfallModeChange,
  onLoadMore,
  loadingMore,
  hasMore,
}) {
  const contentClass = javRandomMode ? 'mt-4' : ''
  const [sortAnchorEl, setSortAnchorEl] = useState(null)
  const effectiveSort = javResolvedSort
  const currentOption = findSortOption(JAV_SORT_OPTIONS, effectiveSort) || JAV_SORT_OPTIONS[0]
  const activeWaterfallMode = waterfallMode && !javRandomMode
  const normalizedInventory = normalizeJavInventory(javInventory)
  const changeJavViewPreset = useJavViewPresetTransition(javViewPreset, onJavViewPresetChange)
  const emptyMessage =
    normalizedInventory === JAV_INVENTORY_PENDING
      ? zh('暂无未入库作品', 'No pending works')
      : normalizedInventory === JAV_INVENTORY_IMPORTED
        ? zh('暂无已入库作品', 'No imported works')
        : zh('暂无 JAV 数据', 'No JAV data')

  const workflowGridProps = {
    columns: Math.min(Number(javGridColumns) || 4, 3),
    titleMaxRows: javTitleMaxRows,
    idolTagMaxRows: javIdolTagMaxRows,
    tagMaxRows: javTagMaxRows,
    buildJavUrl,
    onPlay,
    onIdolClick,
    onOpenFavorites,
    onOpenJavFavorites,
    onOpenStudioFavorites,
    onOpenSeriesFavorites,
    onPrefixClick,
    onStudioClick,
    onSeriesClick,
    onTagClick,
    onOpenFile,
    openFileLabel,
    onRevealFile,
    onOpenScreenshots,
    onManageVideoPlay,
    onManageVideoPlayAtTime,
    onManageVideoCoverChanged,
    onManageVideoOpenFile,
    onManageVideoRevealFile,
    onManageVideoOpenTagPicker,
    onManageVideoOpenScreenshots,
    onManageVideoOpenScrapeSettings,
    onManageVideoRename,
    onManageVideoDelete,
    onManageVideoTagClick,
  }

  const isOptionActive = (option) => {
    return findSortOption([option], effectiveSort)
  }

  const openSortMenu = (event) => {
    setSortAnchorEl(event.currentTarget)
  }

  const closeSortMenu = () => {
    setSortAnchorEl(null)
  }

  return (
    <>
      {!javRandomMode && (
        <div className="sticky-pagination pagination-toolbar-grid mb-4 grid md:grid-cols-[1fr_auto_1fr] md:items-center">
          <div className="hidden md:block" />
          <div className="flex justify-center overflow-x-auto">
            <Pagination
              page={javPage}
              lastPage={javLastPage}
              totalItems={javTotal}
              hasPrev={javHasPrev}
              hasNext={javHasNext}
              loading={javLoading}
              buildPageUrl={({ page: targetPage }) => buildJavUrl({ page: targetPage })}
              onFirst={() => setJavPage(1)}
              onPrev={() => {
                if (javHasPrev) setJavPage(javPage - 1)
              }}
              onGoToPage={(p) => setJavPage(p)}
              onNext={() => {
                if (javHasNext) setJavPage(javPage + 1)
              }}
              onLast={() => setJavPage(javLastPage)}
              waterfallMode={activeWaterfallMode}
              onWaterfallModeChange={onWaterfallModeChange}
            />
          </div>
          <div className="flex flex-wrap items-center justify-end gap-3">
            <JavMagnetQueueButton />
            <JavQualityReviewQueueButton
              directoryIds={directoryIds}
              gridProps={workflowGridProps}
            />
            <JavImportHistoryButton directoryIds={directoryIds} gridProps={workflowGridProps} />
            <JavViewPresetSwitch value={javViewPreset} onChange={changeJavViewPreset} />
            <div className="pagination-sort-group flex items-center">
              <span className="pagination-sort-label text-gray-500">{zh('排序', 'Sort')}</span>
              <button
                type="button"
                onClick={openSortMenu}
                aria-haspopup="dialog"
                aria-expanded={Boolean(sortAnchorEl)}
                aria-label={zh('修改当前 JAV 排序方式', 'Change current JAV sort')}
                className="pagination-sort-button"
              >
                <SortText option={currentOption} value={effectiveSort} />
                <span aria-hidden="true" className="pagination-sort-caret" />
              </button>
            </div>
            <Popover
              open={Boolean(sortAnchorEl)}
              anchorEl={sortAnchorEl}
              onClose={closeSortMenu}
              disableScrollLock
              anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
              transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            >
              <div className="pagination-sort-menu">
                {javSortSource === 'temporary' ? (
                  <button
                    type="button"
                    onClick={() => {
                      closeSortMenu()
                      setJavTempSort?.('')
                    }}
                    className="w-full border-b border-slate-100 px-3 py-2 text-left text-xs font-medium text-stone-700 hover:bg-stone-50"
                  >
                    {zh('恢复自动排序', 'Restore automatic sort')}
                  </button>
                ) : null}
                {JAV_SORT_OPTIONS.map((option) => {
                  const active = isOptionActive(option)
                  const displayValue = active ? effectiveSort : option.defaultValue
                  return (
                    <div
                      key={option.base}
                      className={`pagination-sort-row ${
                        active ? 'bg-stone-100 text-stone-800' : 'text-gray-700 hover:bg-gray-50'
                      }`}
                    >
                      <button
                        type="button"
                        onClick={() => {
                          closeSortMenu()
                          setJavTempSort?.(displayValue)
                        }}
                        className="pagination-sort-option"
                      >
                        <SortText option={option} value={displayValue} />
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          closeSortMenu()
                          setJavTempSort?.(
                            reverseSortValue([option], displayValue, option.defaultValue)
                          )
                        }}
                        className="pagination-sort-reverse"
                        title={zh('反转排序', 'Reverse sort')}
                        aria-label={zh(
                          `反转${option.label[0]}排序`,
                          `Reverse ${option.label[1]} sort`
                        )}
                      >
                        <SwapVertIcon fontSize="inherit" />
                      </button>
                    </div>
                  )
                })}
              </div>
            </Popover>
          </div>
        </div>
      )}
      {javLoading ? (
        <div
          className={`${contentClass} flex min-h-[200px] items-center justify-center rounded border border-dashed border-gray-200 text-gray-500`}
        >
          {zh('加载中…', 'Loading...')}
        </div>
      ) : (
        <div
          className={`${contentClass} ${
            normalizeJavViewPreset(javViewPreset) === JAV_VIEW_PRESET_COMPACT
              ? 'jav-view--compact'
              : 'jav-view--detailed'
          }`}
        >
          <JavGrid
            items={javItems}
            emptyMessage={emptyMessage}
            columns={javGridColumns}
            titleMaxRows={javTitleMaxRows}
            idolTagMaxRows={javIdolTagMaxRows}
            tagMaxRows={javTagMaxRows}
            buildJavUrl={buildJavUrl}
            onPlay={onPlay}
            onIdolClick={onIdolClick}
            onOpenFavorites={onOpenFavorites}
            onOpenJavFavorites={onOpenJavFavorites}
            onOpenStudioFavorites={onOpenStudioFavorites}
            onOpenSeriesFavorites={onOpenSeriesFavorites}
            onPrefixClick={onPrefixClick}
            onStudioClick={onStudioClick}
            onSeriesClick={onSeriesClick}
            onTagClick={onTagClick}
            onOpenFile={onOpenFile}
            openFileLabel={openFileLabel}
            onRevealFile={onRevealFile}
            onOpenScreenshots={onOpenScreenshots}
            onManageVideoPlay={onManageVideoPlay}
            onManageVideoPlayAtTime={onManageVideoPlayAtTime}
            onManageVideoCoverChanged={onManageVideoCoverChanged}
            onManageVideoOpenFile={onManageVideoOpenFile}
            onManageVideoRevealFile={onManageVideoRevealFile}
            onManageVideoOpenTagPicker={onManageVideoOpenTagPicker}
            onManageVideoOpenScreenshots={onManageVideoOpenScreenshots}
            onManageVideoOpenScrapeSettings={onManageVideoOpenScrapeSettings}
            onManageVideoRename={onManageVideoRename}
            onManageVideoDelete={onManageVideoDelete}
            onManageVideoTagClick={onManageVideoTagClick}
          />
        </div>
      )}
      <WaterfallLoader
        enabled={activeWaterfallMode && !javLoading}
        hasMore={hasMore}
        loading={loadingMore}
        onLoadMore={onLoadMore}
      />
    </>
  )
}
