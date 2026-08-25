import { useCallback, useEffect, useMemo, useState } from 'react'
import AppModal from '@/components/AppModal'
import { createJavInputBatch, fetchJavInputBatch, fetchJavInputBatches } from '@/api'
import { getErrorMessage } from '@/utils/errors'
import { zh } from '@/utils/i18n'
import {
  countJavInputLines,
  groupJavInputItems,
  JAV_INPUT_STATUS as STATUS,
} from '@/utils/javInput'

function formatBatchTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value || '')
  return new Intl.DateTimeFormat(zh('zh-CN', 'en'), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}

function statusLabel(item) {
  switch (item?.status) {
    case STATUS.duplicateBatch:
      return zh(
        `与本批第 ${item.duplicate_of_line} 行重复`,
        `Duplicates line ${item.duplicate_of_line}`
      )
    case STATUS.duplicateLibrary:
      return zh('作品库中已有真实文件', 'A real file already exists in the library')
    case STATUS.duplicateHistory:
      return zh(
        `历史批次 #${item.existing_batch_id} 已经接收`,
        `Already accepted by history batch #${item.existing_batch_id}`
      )
    case STATUS.invalid:
      return zh('未识别到番号', 'No JAV code recognized')
    default:
      return zh('保留', 'Kept')
  }
}

function ResultRows({ items, emptyText, tone = 'slate', showReason = true }) {
  const toneClasses = {
    emerald: 'border-emerald-100 bg-emerald-50/50',
    amber: 'border-amber-100 bg-amber-50/50',
    rose: 'border-rose-100 bg-rose-50/50',
    indigo: 'border-indigo-100 bg-indigo-50/40',
    slate: 'border-slate-100 bg-white',
  }
  if (!items.length) {
    return <div className="px-4 py-5 text-sm text-slate-400">{emptyText}</div>
  }
  return (
    <div className="divide-y divide-slate-100">
      {items.map((item) => (
        <div
          key={item.id || `${item.line_number}-${item.normalized_code}-${item.status}`}
          className={`grid gap-2 border-l-4 px-4 py-3 sm:grid-cols-[4rem_8rem_minmax(0,1fr)] ${toneClasses[tone]}`}
        >
          <span className="text-xs tabular-nums text-slate-400">
            {zh(`第 ${item.line_number} 行`, `Line ${item.line_number}`)}
          </span>
          <span className="text-xs font-semibold text-slate-700">{item.code || '—'}</span>
          <div className="min-w-0">
            <div className="whitespace-pre-wrap break-words font-mono text-sm text-slate-800">
              {item.raw_line}
            </div>
            {showReason && item.status !== STATUS.accepted ? (
              <div className="mt-1 text-xs text-slate-500">{statusLabel(item)}</div>
            ) : null}
          </div>
        </div>
      ))}
    </div>
  )
}

function ResultSection({ title, description, count, items, emptyText, tone, showReason }) {
  return (
    <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div>
          <h3 className="font-semibold text-slate-900">{title}</h3>
          {description ? <p className="mt-1 text-xs text-slate-500">{description}</p> : null}
        </div>
        <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-600">
          {count}
        </span>
      </header>
      <ResultRows items={items} emptyText={emptyText} tone={tone} showReason={showReason} />
    </section>
  )
}

function BatchResult({ batch }) {
  const { batchDuplicates, firstStage, accepted, globalDuplicates, invalid, globalDuplicateCount } =
    groupJavInputItems(batch)

  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-indigo-100 bg-indigo-50 p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <span className="font-semibold text-indigo-950">
              {zh(`输入批次 #${batch.id}`, `Input batch #${batch.id}`)}
            </span>
            <span className="ml-2 text-xs text-indigo-600">
              {formatBatchTime(batch.created_at)}
            </span>
          </div>
          <span className="text-xs text-indigo-700">
            {zh('不可变历史记录', 'Immutable history record')}
          </span>
        </div>
        <div className="mt-4 grid gap-3 sm:grid-cols-3">
          <div className="rounded-lg bg-white px-4 py-3">
            <div className="text-xs text-slate-500">
              {zh('原始非空行', 'Non-empty input lines')}
            </div>
            <div className="mt-1 text-2xl font-semibold text-slate-900">{batch.input_count}</div>
            {batch.invalid_count ? (
              <div className="mt-1 text-xs text-rose-600">
                {zh(`${batch.invalid_count} 行未识别`, `${batch.invalid_count} unrecognized`)}
              </div>
            ) : null}
          </div>
          <div className="rounded-lg bg-white px-4 py-3">
            <div className="text-xs text-slate-500">
              {zh('第一道 · 批内去重后', 'Stage 1 · Within-batch')}
            </div>
            <div className="mt-1 text-2xl font-semibold text-slate-900">
              {batch.batch_unique_count}
            </div>
            <div className="mt-1 text-xs text-amber-600">
              {zh(
                `剔除 ${batch.batch_duplicate_count} 个重复`,
                `Removed ${batch.batch_duplicate_count} duplicate(s)`
              )}
            </div>
          </div>
          <div className="rounded-lg bg-white px-4 py-3">
            <div className="text-xs text-slate-500">
              {zh('第二道 · 全局去重后', 'Stage 2 · Global')}
            </div>
            <div className="mt-1 text-2xl font-semibold text-emerald-700">
              {batch.accepted_count}
            </div>
            <div className="mt-1 text-xs text-amber-600">
              {zh(
                `剔除 ${globalDuplicateCount} 个重复`,
                `Removed ${globalDuplicateCount} duplicate(s)`
              )}
            </div>
          </div>
        </div>
      </div>

      <ResultSection
        title={zh('第一道结果：批内去重后', 'Stage 1 result: de-duplicated within this batch')}
        description={zh(
          '只保留每个番号第一次出现的原始行，暂未与全局比较。',
          'Keeps the original first occurrence of each code before the global comparison.'
        )}
        count={firstStage.length}
        items={firstStage}
        emptyText={zh('没有可进入第二道的番号', 'No code can proceed to stage 2')}
        tone="indigo"
        showReason={false}
      />
      <ResultSection
        title={zh('第一道剔除：本批重复', 'Stage 1 removed: duplicates in this batch')}
        description={zh(
          '这里明确指出它与本批哪一行重复。',
          'Each item points to the earlier line it duplicates.'
        )}
        count={batchDuplicates.length}
        items={batchDuplicates}
        emptyText={zh('本批没有重复番号', 'No within-batch duplicates')}
        tone="amber"
      />
      <ResultSection
        title={zh('第二道结果：全局去重后', 'Stage 2 result: globally unique raw codes')}
        description={zh(
          '这些番号既没有真实文件，也没有在以前的裸番号批次中出现，可供下一阶段继续处理。',
          'These codes have neither a real file nor an earlier accepted raw-code record.'
        )}
        count={accepted.length}
        items={accepted}
        emptyText={zh('本批没有新增的全局唯一番号', 'No new globally unique code in this batch')}
        tone="emerald"
      />
      <ResultSection
        title={zh('第二道剔除：全局重复', 'Stage 2 removed: global duplicates')}
        description={zh(
          `作品库 ${batch.library_duplicate_count} 个，历史裸番号 ${batch.history_duplicate_count} 个。`,
          `${batch.library_duplicate_count} in the library and ${batch.history_duplicate_count} in raw-code history.`
        )}
        count={globalDuplicates.length}
        items={globalDuplicates}
        emptyText={zh('没有发现全局重复', 'No global duplicates')}
        tone="rose"
      />
      {invalid.length ? (
        <ResultSection
          title={zh('未识别行', 'Unrecognized lines')}
          description={zh(
            '原文仍保存在历史中，但没有参与两道去重。',
            'Original text is retained in history but excluded from de-duplication.'
          )}
          count={invalid.length}
          items={invalid}
          emptyText=""
          tone="slate"
        />
      ) : null}
    </div>
  )
}

export default function JavInputModal({ open, onClose }) {
  const [tab, setTab] = useState('new')
  const [input, setInput] = useState('')
  const [result, setResult] = useState(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [history, setHistory] = useState({ items: [], total: 0, page: 1, page_size: 20 })
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState('')
  const [selectedBatch, setSelectedBatch] = useState(null)
  const [selectedBatchLoading, setSelectedBatchLoading] = useState(false)

  const inputLineCount = useMemo(() => countJavInputLines(input), [input])
  const historyPages = Math.max(
    1,
    Math.ceil(Number(history.total || 0) / Number(history.page_size || 20))
  )

  const loadBatch = useCallback(async (id) => {
    setSelectedBatchLoading(true)
    setHistoryError('')
    try {
      setSelectedBatch(await fetchJavInputBatch(id))
    } catch (requestError) {
      setHistoryError(getErrorMessage(requestError))
    } finally {
      setSelectedBatchLoading(false)
    }
  }, [])

  const loadHistory = useCallback(
    async (page = 1, { selectFirst = false } = {}) => {
      setHistoryLoading(true)
      setHistoryError('')
      try {
        const response = await fetchJavInputBatches({ page, pageSize: 20 })
        setHistory(response)
        const batches = Array.isArray(response?.items) ? response.items : []
        if (selectFirst && batches.length) await loadBatch(batches[0].id)
        if (!batches.length) setSelectedBatch(null)
      } catch (requestError) {
        setHistoryError(getErrorMessage(requestError))
      } finally {
        setHistoryLoading(false)
      }
    },
    [loadBatch]
  )

  useEffect(() => {
    if (!open) return
    void loadHistory(1)
  }, [loadHistory, open])

  const submit = async (event) => {
    event.preventDefault()
    if (!inputLineCount || saving) return
    setSaving(true)
    setError('')
    try {
      const batch = await createJavInputBatch(input)
      setResult(batch)
      await loadHistory(1)
    } catch (requestError) {
      setError(getErrorMessage(requestError))
    } finally {
      setSaving(false)
    }
  }

  const openHistory = async () => {
    setTab('history')
    const first = Array.isArray(history.items) ? history.items[0] : null
    if (!selectedBatch && first) await loadBatch(first.id)
  }

  return (
    <AppModal
      open={open}
      onClose={onClose}
      ariaLabel={zh('番号输入', 'JAV code input')}
      contentClassName="max-h-[94vh] w-[min(1280px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-slate-50 shadow-2xl"
    >
      <div className="flex max-h-[94vh] flex-col">
        <header className="border-b border-slate-200 bg-white px-6 pt-5">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 className="text-lg font-semibold text-slate-900">
                {zh('裸番号 · 输入、去重与历史', 'Raw codes · input, de-duplication and history')}
              </h2>
              <p className="mt-1 max-w-3xl text-sm text-slate-500">
                {zh(
                  '作品库保存已经入库并完成刮削的最终作品；这里保存尚未拥有真实文件的裸番号。当前阶段只建立历史和两道去重，不查询磁链、不提交下载。',
                  'The library contains final scraped works with real files. This area stores raw codes without files. This stage only records history and runs two de-duplication passes.'
                )}
              </p>
            </div>
            <button
              type="button"
              onClick={onClose}
              className="text-sm text-slate-500 hover:text-slate-900"
            >
              {zh('关闭', 'Close')}
            </button>
          </div>
          <nav
            className="mt-5 flex gap-5 text-sm"
            aria-label={zh('番号输入页面', 'JAV input views')}
          >
            <button
              type="button"
              onClick={() => setTab('new')}
              className={`border-b-2 pb-3 font-medium ${tab === 'new' ? 'border-indigo-600 text-indigo-700' : 'border-transparent text-slate-500'}`}
            >
              {zh('新建批次', 'New batch')}
            </button>
            <button
              type="button"
              onClick={openHistory}
              className={`border-b-2 pb-3 font-medium ${tab === 'history' ? 'border-indigo-600 text-indigo-700' : 'border-transparent text-slate-500'}`}
            >
              {zh(
                `历史记录${history.total ? ` · ${history.total}` : ''}`,
                `History${history.total ? ` · ${history.total}` : ''}`
              )}
            </button>
          </nav>
        </header>

        <div className="overflow-y-auto px-6 py-5">
          {tab === 'new' ? (
            <div className="space-y-6">
              <form
                onSubmit={submit}
                className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"
              >
                <label className="block">
                  <span className="text-sm font-semibold text-slate-800">
                    {zh('原始番号清单', 'Original raw-code list')}
                  </span>
                  <span className="mt-1 block text-xs text-slate-500">
                    {zh(
                      '每行一个作品。番号后的中文备注、来源说明等会原封不动保存和展示。',
                      'Use one work per line. Notes and source descriptions after the code are retained verbatim.'
                    )}
                  </span>
                  <textarea
                    value={input}
                    onChange={(event) => setInput(event.target.value)}
                    rows={10}
                    placeholder={
                      'DPMX-004 朋友推荐，优先找无码\nSSIS-589 收藏于 2026-08-25\nDPMX-004 重复示例'
                    }
                    className="mt-3 w-full resize-y rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 font-mono text-sm leading-6 outline-none transition focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100"
                  />
                </label>
                <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                  <div className="text-xs text-slate-500">
                    {zh(
                      `${inputLineCount} 个非空输入行`,
                      `${inputLineCount} non-empty input line(s)`
                    )}
                    <span className="mx-2">·</span>
                    {zh('单批最多 5000 行', 'Up to 5,000 lines per batch')}
                  </div>
                  <button
                    type="submit"
                    disabled={saving || !inputLineCount}
                    className="rounded-lg bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {saving
                      ? zh('保存并去重中…', 'Saving and de-duplicating…')
                      : zh('保存批次并执行两道去重', 'Save batch and run both stages')}
                  </button>
                </div>
                {error ? (
                  <div className="mt-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                    {error}
                  </div>
                ) : null}
              </form>
              {result ? <BatchResult batch={result} /> : null}
            </div>
          ) : (
            <div className="grid min-h-[34rem] gap-5 lg:grid-cols-[20rem_minmax(0,1fr)]">
              <aside className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
                <div className="border-b border-slate-100 px-4 py-3">
                  <h3 className="font-semibold text-slate-900">
                    {zh('每次添加历史', 'Input history')}
                  </h3>
                  <p className="mt-1 text-xs text-slate-500">
                    {zh(
                      '每一次提交都保留，包括全重复或无法识别的批次。',
                      'Every submission is retained.'
                    )}
                  </p>
                </div>
                {historyLoading ? (
                  <div className="px-4 py-6 text-sm text-slate-400">
                    {zh('加载中…', 'Loading…')}
                  </div>
                ) : (
                  <div className="divide-y divide-slate-100">
                    {(Array.isArray(history.items) ? history.items : []).map((batch) => (
                      <button
                        key={batch.id}
                        type="button"
                        onClick={() => loadBatch(batch.id)}
                        className={`block w-full px-4 py-3 text-left transition hover:bg-slate-50 ${selectedBatch?.id === batch.id ? 'bg-indigo-50' : ''}`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-sm font-semibold text-slate-800">#{batch.id}</span>
                          <span className="text-xs text-slate-400">
                            {formatBatchTime(batch.created_at)}
                          </span>
                        </div>
                        <div className="mt-1 text-xs text-slate-500">
                          {zh(
                            `输入 ${batch.input_count} · 新增 ${batch.accepted_count} · 重复 ${Number(batch.batch_duplicate_count || 0) + Number(batch.library_duplicate_count || 0) + Number(batch.history_duplicate_count || 0)}`,
                            `Input ${batch.input_count} · New ${batch.accepted_count} · Duplicates ${Number(batch.batch_duplicate_count || 0) + Number(batch.library_duplicate_count || 0) + Number(batch.history_duplicate_count || 0)}`
                          )}
                        </div>
                      </button>
                    ))}
                    {!historyLoading && !history.items?.length ? (
                      <div className="px-4 py-8 text-center text-sm text-slate-400">
                        {zh('还没有输入历史', 'No input history yet')}
                      </div>
                    ) : null}
                  </div>
                )}
                {history.total > history.page_size ? (
                  <div className="flex items-center justify-between border-t border-slate-100 px-4 py-3 text-xs">
                    <button
                      type="button"
                      disabled={history.page <= 1 || historyLoading}
                      onClick={() => loadHistory(history.page - 1, { selectFirst: true })}
                      className="text-indigo-600 disabled:text-slate-300"
                    >
                      {zh('上一页', 'Previous')}
                    </button>
                    <span className="text-slate-500">
                      {history.page} / {historyPages}
                    </span>
                    <button
                      type="button"
                      disabled={history.page >= historyPages || historyLoading}
                      onClick={() => loadHistory(history.page + 1, { selectFirst: true })}
                      className="text-indigo-600 disabled:text-slate-300"
                    >
                      {zh('下一页', 'Next')}
                    </button>
                  </div>
                ) : null}
              </aside>
              <main className="min-w-0">
                {historyError ? (
                  <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                    {historyError}
                  </div>
                ) : null}
                {selectedBatchLoading ? (
                  <div className="rounded-xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-400">
                    {zh('读取批次详情中…', 'Loading batch details…')}
                  </div>
                ) : selectedBatch ? (
                  <BatchResult batch={selectedBatch} />
                ) : (
                  <div className="rounded-xl border border-dashed border-slate-300 bg-white px-5 py-16 text-center text-sm text-slate-400">
                    {zh(
                      '选择左侧历史批次查看两道去重结果',
                      'Select a history batch to inspect both stages'
                    )}
                  </div>
                )}
              </main>
            </div>
          )}
        </div>
      </div>
    </AppModal>
  )
}
