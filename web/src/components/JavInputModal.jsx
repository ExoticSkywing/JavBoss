import { useCallback, useEffect, useMemo, useState } from 'react'
import AppModal from '@/components/AppModal'
import {
  clearJavInputPreprocessed,
  createJavInputBatch,
  deleteAllJavInputBatches,
  deleteJavInputBatch,
  fetchJavInputBatch,
  fetchJavInputBatches,
  fetchJavInputPreprocessed,
} from '@/api'
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

function sourceKey(item) {
  return `${item?.line_number || 0}\u0000${item?.raw_line || ''}`
}

function buildSourceMetadata(items) {
  const sources = new Map()
  for (const item of Array.isArray(items) ? items : []) {
    if (!item?.code || item.status === STATUS.invalid || item.status === STATUS.note) continue
    const key = sourceKey(item)
    let source = sources.get(key)
    if (!source) {
      source = { firstPositionByCode: new Map(), itemPositions: new Map(), total: 0 }
      sources.set(key, source)
    }
    source.total += 1
    source.itemPositions.set(item, source.total)
    const codeKey = item.normalized_code || item.code
    if (!source.firstPositionByCode.has(codeKey)) {
      source.firstPositionByCode.set(codeKey, source.total)
    }
  }
  return sources
}

function statusLabel(item, source) {
  switch (item?.status) {
    case STATUS.duplicateBatch:
      if (item.duplicate_of_line === item.line_number && source) {
        const position = source.itemPositions.get(item)
        const firstPosition = source.firstPositionByCode.get(item.normalized_code || item.code)
        return zh(
          `本行第 ${position} 个，与第 ${firstPosition} 个重复`,
          `Item ${position} on this line duplicates item ${firstPosition}`
        )
      }
      return item.duplicate_of_line === item.line_number
        ? zh('与本行前面出现的相同番号重复', 'Duplicates the same code earlier on this line')
        : zh(
            `与本批第 ${item.duplicate_of_line} 行重复`,
            `Duplicates line ${item.duplicate_of_line}`
          )
    case STATUS.duplicateLibrary:
      return zh('作品库中已有真实文件', 'A real file already exists in the library')
    case STATUS.duplicateHistory:
      return item.existing_batch_id
        ? zh(
            `历史批次 #${item.existing_batch_id} 已经接收`,
            `Already accepted by history batch #${item.existing_batch_id}`
          )
        : zh('原接收批次已删除', 'The original accepting batch was deleted')
    case STATUS.invalid:
      return zh('未识别到番号', 'No JAV code recognized')
    case STATUS.note:
      return zh('批次备注，不参与去重', 'Batch note; excluded from de-duplication')
    case STATUS.cleared:
      return zh('已从预处理作品全局清除', 'Globally cleared from preprocessed works')
    default:
      return zh('保留', 'Kept')
  }
}

function ResultRows({ items, emptyText, sourceMetadata, tone = 'slate', showReason = true }) {
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

  const rows = []
  const rowsBySource = new Map()
  for (const item of items) {
    const key = sourceKey(item)
    let row = rowsBySource.get(key)
    if (!row) {
      row = { items: [], key }
      rowsBySource.set(key, row)
      rows.push(row)
    }
    row.items.push(item)
  }

  return (
    <div className="divide-y divide-slate-100">
      {rows.map((row) => {
        const item = row.items[0]
        const source = sourceMetadata?.get(row.key)
        const groupedSource = Number(source?.total || 0) > 1
        if (groupedSource) {
          return (
            <div
              key={row.key}
              className={`grid gap-3 border-l-4 px-4 py-4 sm:grid-cols-[5rem_minmax(0,1fr)] ${toneClasses[tone]}`}
            >
              <span className="text-xs tabular-nums text-slate-400">
                {zh(`第 ${item.line_number} 行`, `Line ${item.line_number}`)}
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap gap-2">
                  {row.items.map((rowItem) => (
                    <span
                      key={
                        rowItem.id ||
                        `${rowItem.line_number}-${rowItem.normalized_code}-${rowItem.status}`
                      }
                      className="rounded-md border border-slate-200 bg-white px-2.5 py-1 font-mono text-sm font-semibold text-slate-700 shadow-sm"
                    >
                      {rowItem.code}
                    </span>
                  ))}
                </div>
                {showReason
                  ? row.items
                      .filter((rowItem) => rowItem.status !== STATUS.accepted)
                      .map((rowItem) => (
                        <div key={`reason-${rowItem.id}`} className="mt-2 text-xs text-slate-500">
                          <span className="font-semibold text-slate-700">{rowItem.code}</span>
                          <span className="mx-1.5">·</span>
                          {statusLabel(rowItem, source)}
                        </div>
                      ))
                  : null}
                <details className="mt-3 text-xs text-slate-500">
                  <summary className="cursor-pointer select-none hover:text-slate-700">
                    {zh(
                      `查看原始输入（本行共 ${source.total} 个番号）`,
                      `Show original input (${source.total} codes on this line)`
                    )}
                  </summary>
                  <div className="mt-2 whitespace-pre-wrap break-words rounded-lg bg-white/80 px-3 py-2 font-mono leading-5 text-slate-600">
                    {item.raw_line}
                  </div>
                </details>
              </div>
            </div>
          )
        }
        return (
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
                <div className="mt-1 text-xs text-slate-500">{statusLabel(item, source)}</div>
              ) : null}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function ResultSection({
  title,
  description,
  count,
  items,
  emptyText,
  sourceMetadata,
  tone,
  showReason,
}) {
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
      <ResultRows
        items={items}
        emptyText={emptyText}
        sourceMetadata={sourceMetadata}
        tone={tone}
        showReason={showReason}
      />
    </section>
  )
}

function BatchResult({ batch, deleting = false, onDelete }) {
  const {
    batchDuplicates,
    firstStage,
    accepted,
    globalDuplicates,
    invalid,
    notes,
    globalDuplicateCount,
  } = groupJavInputItems(batch)
  const sourceMetadata = useMemo(() => buildSourceMetadata(batch?.items), [batch?.items])

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
          <div className="flex items-center gap-3">
            <span className="text-xs text-indigo-700">
              {zh('完整输入快照', 'Complete input snapshot')}
            </span>
            {onDelete ? (
              <button
                type="button"
                disabled={deleting}
                onClick={() => onDelete(batch)}
                className="rounded-md border border-rose-200 bg-white px-2.5 py-1.5 text-xs font-semibold text-rose-600 transition hover:bg-rose-50 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {deleting ? zh('删除中…', 'Deleting…') : zh('删除本批次', 'Delete batch')}
              </button>
            ) : null}
          </div>
        </div>
        <div className="mt-4 grid gap-3 sm:grid-cols-3">
          <div className="rounded-lg bg-white px-4 py-3">
            <div className="text-xs text-slate-500">
              {zh('原始非空行', 'Non-empty input lines')}
            </div>
            <div className="mt-1 text-2xl font-semibold text-slate-900">{batch.input_count}</div>
            <div className="mt-1 text-xs text-slate-500">
              {zh(
                `识别 ${batch.parsed_count} 个番号${notes.length ? ` · ${notes.length} 行备注` : ''}`,
                `${batch.parsed_count} code(s) recognized${notes.length ? ` · ${notes.length} note line(s)` : ''}`
              )}
            </div>
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

      {notes.length ? (
        <ResultSection
          title={zh('批次备注', 'Batch notes')}
          description={zh(
            '纯文字标题或说明会完整保留，但不作为作品参与两道去重。',
            'Plain-text titles and notes are retained but excluded from both stages.'
          )}
          count={notes.length}
          items={notes}
          emptyText=""
          sourceMetadata={sourceMetadata}
          tone="slate"
          showReason={false}
        />
      ) : null}
      <ResultSection
        title={zh('第一道剔除：本批重复', 'Stage 1 removed: duplicates in this batch')}
        description={zh(
          '这里明确指出它与本批哪一行重复。',
          'Each item points to the earlier line it duplicates.'
        )}
        count={batchDuplicates.length}
        items={batchDuplicates}
        emptyText={zh('本批没有重复番号', 'No within-batch duplicates')}
        sourceMetadata={sourceMetadata}
        tone="amber"
      />
      <ResultSection
        title={zh('第一道结果：批内去重后', 'Stage 1 result: de-duplicated within this batch')}
        description={zh(
          '只保留每个番号第一次出现的原始行，暂未与全局比较。',
          'Keeps the original first occurrence of each code before the global comparison.'
        )}
        count={firstStage.length}
        items={firstStage}
        emptyText={zh('没有可进入第二道的番号', 'No code can proceed to stage 2')}
        sourceMetadata={sourceMetadata}
        tone="indigo"
        showReason={false}
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
        sourceMetadata={sourceMetadata}
        tone="rose"
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
        sourceMetadata={sourceMetadata}
        tone="emerald"
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
          sourceMetadata={sourceMetadata}
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
  const [deletingBatchID, setDeletingBatchID] = useState(null)
  const [clearingHistory, setClearingHistory] = useState(false)
  const [preprocessed, setPreprocessed] = useState({
    items: [],
    total: 0,
    global_total: 0,
    page: 1,
    page_size: 20,
  })
  const [preprocessedLoading, setPreprocessedLoading] = useState(false)
  const [clearingPreprocessed, setClearingPreprocessed] = useState(false)
  const [preprocessedError, setPreprocessedError] = useState('')
  const [preprocessedQuery, setPreprocessedQuery] = useState('')
  const [appliedPreprocessedQuery, setAppliedPreprocessedQuery] = useState('')

  const inputLineCount = useMemo(() => countJavInputLines(input), [input])
  const historyPages = Math.max(
    1,
    Math.ceil(Number(history.total || 0) / Number(history.page_size || 20))
  )
  const preprocessedPages = Math.max(
    1,
    Math.ceil(Number(preprocessed.total || 0) / Number(preprocessed.page_size || 20))
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

  const loadPreprocessed = useCallback(async (page = 1, query = '') => {
    setPreprocessedLoading(true)
    setPreprocessedError('')
    try {
      const response = await fetchJavInputPreprocessed({ page, pageSize: 20, query })
      setPreprocessed(response)
      setAppliedPreprocessedQuery(query)
    } catch (requestError) {
      setPreprocessedError(getErrorMessage(requestError))
    } finally {
      setPreprocessedLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!open) return
    void loadHistory(1)
    void loadPreprocessed(1)
  }, [loadHistory, loadPreprocessed, open])

  const submit = async (event) => {
    event.preventDefault()
    if (!inputLineCount || saving) return
    setSaving(true)
    setError('')
    try {
      const batch = await createJavInputBatch(input)
      setResult(batch)
      await Promise.all([loadHistory(1), loadPreprocessed(1, appliedPreprocessedQuery)])
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

  const openPreprocessed = async () => {
    setTab('preprocessed')
    await loadPreprocessed(1, appliedPreprocessedQuery)
  }

  const removeBatch = async (batch) => {
    if (!batch?.id || deletingBatchID || clearingHistory) return
    const preview = String(batch.preview || '').trim()
    const label = preview ? `#${batch.id} · ${preview}` : `#${batch.id}`
    if (
      !window.confirm(
        zh(
          `确定删除批次 ${label}？该批历史和预处理结果会一并移除，其中接收的番号可以重新输入。`,
          `Delete batch ${label}? Its history and preprocessed results will be removed, and accepted codes can be entered again.`
        )
      )
    )
      return
    setDeletingBatchID(batch.id)
    setHistoryError('')
    try {
      await deleteJavInputBatch(batch.id)
      if (result?.id === batch.id) setResult(null)
      if (selectedBatch?.id === batch.id) setSelectedBatch(null)
      await Promise.all([
        loadHistory(1, { selectFirst: true }),
        loadPreprocessed(1, appliedPreprocessedQuery),
      ])
    } catch (requestError) {
      setHistoryError(getErrorMessage(requestError))
    } finally {
      setDeletingBatchID(null)
    }
  }

  const clearHistory = async () => {
    if (!history.total || clearingHistory || deletingBatchID) return
    if (
      !window.confirm(
        zh(
          `确定清空全部 ${history.total} 个输入批次？这里只删除番号输入历史和预处理作品，不会删除正式作品或真实文件。`,
          `Clear all ${history.total} input batches? This only removes input history and preprocessed works, never final works or real files.`
        )
      )
    )
      return
    setClearingHistory(true)
    setHistoryError('')
    try {
      await deleteAllJavInputBatches()
      setResult(null)
      setSelectedBatch(null)
      await Promise.all([loadHistory(1), loadPreprocessed(1, appliedPreprocessedQuery)])
    } catch (requestError) {
      setHistoryError(getErrorMessage(requestError))
    } finally {
      setClearingHistory(false)
    }
  }

  const searchPreprocessed = async (event) => {
    event.preventDefault()
    await loadPreprocessed(1, preprocessedQuery.trim())
  }

  const clearPreprocessed = async () => {
    const globalTotal = Number(preprocessed.global_total ?? preprocessed.total ?? 0)
    if (!globalTotal || clearingPreprocessed) return
    if (
      !window.confirm(
        zh(
          `确定全局清空全部 ${globalTotal} 部预处理作品？该操作不受当前检索和分页影响；历史批次仍会保留，但这些番号会释放并允许重新输入。正式作品和真实文件不会受到影响。`,
          `Globally clear all ${globalTotal} preprocessed works? This ignores the current search and page. Batch history remains, but these codes are released for re-entry. Final works and real files are unaffected.`
        )
      )
    )
      return
    setClearingPreprocessed(true)
    setPreprocessedError('')
    try {
      await clearJavInputPreprocessed()
      await Promise.all([
        loadPreprocessed(1, ''),
        loadHistory(1),
        selectedBatch?.id ? loadBatch(selectedBatch.id) : Promise.resolve(),
      ])
      setPreprocessedQuery('')
    } catch (requestError) {
      setPreprocessedError(getErrorMessage(requestError))
    } finally {
      setClearingPreprocessed(false)
    }
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
            <button
              type="button"
              onClick={openPreprocessed}
              className={`border-b-2 pb-3 font-medium ${tab === 'preprocessed' ? 'border-indigo-600 text-indigo-700' : 'border-transparent text-slate-500'}`}
            >
              {zh(
                `预处理作品${preprocessed.total ? ` · ${preprocessed.total}` : ''}`,
                `Preprocessed works${preprocessed.total ? ` · ${preprocessed.total}` : ''}`
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
                      '支持每行一个番号，也支持在同一行用空格或逗号粘贴多个番号；纯文字标题会作为批次备注保留。',
                      'Enter one code per line or paste multiple space/comma-separated codes on one line. Plain-text titles are retained as batch notes.'
                    )}
                  </span>
                  <textarea
                    value={input}
                    onChange={(event) => setInput(event.target.value)}
                    rows={10}
                    placeholder={
                      '极度美感\nVRTM-138 CORE-018 EBOD-502 JUFD-366 JUFD-366\n\nDPMX-004 朋友推荐，优先找无码'
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
              {result ? (
                <BatchResult
                  batch={result}
                  deleting={deletingBatchID === result.id}
                  onDelete={removeBatch}
                />
              ) : null}
            </div>
          ) : tab === 'history' ? (
            <div className="grid min-h-[34rem] gap-5 lg:grid-cols-[20rem_minmax(0,1fr)]">
              <aside className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
                <div className="border-b border-slate-100 px-4 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <h3 className="font-semibold text-slate-900">
                      {zh('每次添加历史', 'Input history')}
                    </h3>
                    <button
                      type="button"
                      disabled={!history.total || clearingHistory || Boolean(deletingBatchID)}
                      onClick={clearHistory}
                      className="text-xs font-semibold text-rose-600 hover:text-rose-700 disabled:cursor-not-allowed disabled:text-slate-300"
                    >
                      {clearingHistory ? zh('清空中…', 'Clearing…') : zh('清空全部', 'Clear all')}
                    </button>
                  </div>
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
                        <div className="flex min-w-0 items-center gap-2">
                          <span className="shrink-0 text-sm font-semibold text-slate-800">
                            #{batch.id}
                          </span>
                          <span
                            title={batch.preview || ''}
                            className="min-w-0 truncate text-sm text-slate-700"
                          >
                            {batch.preview || zh('无内容摘要', 'No preview')}
                          </span>
                        </div>
                        <div className="mt-1 text-xs text-slate-400">
                          {formatBatchTime(batch.created_at)}
                        </div>
                        <div className="mt-1 text-xs text-slate-500">
                          {zh(
                            `输入 ${batch.input_count} 行 · 识别 ${batch.parsed_count} 个 · 新增 ${batch.accepted_count} · 重复 ${Number(batch.batch_duplicate_count || 0) + Number(batch.library_duplicate_count || 0) + Number(batch.history_duplicate_count || 0)}`,
                            `Input ${batch.input_count} line(s) · Recognized ${batch.parsed_count} · New ${batch.accepted_count} · Duplicates ${Number(batch.batch_duplicate_count || 0) + Number(batch.library_duplicate_count || 0) + Number(batch.history_duplicate_count || 0)}`
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
                  <BatchResult
                    batch={selectedBatch}
                    deleting={deletingBatchID === selectedBatch.id}
                    onDelete={removeBatch}
                  />
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
          ) : (
            <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
              <header className="border-b border-slate-100 px-5 py-4">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div>
                    <h3 className="font-semibold text-slate-900">
                      {zh('预处理作品', 'Preprocessed works')}
                    </h3>
                    <p className="mt-1 max-w-3xl text-xs leading-5 text-slate-500">
                      {zh(
                        '这里集中展示经过批内去重和全局去重后保留下来的最终结果。若某个番号后来已有真实文件，它会自动从这里移出。',
                        'This is the final output after both de-duplication stages. A code automatically leaves this list once a real file exists in the library.'
                      )}
                    </p>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold text-emerald-700">
                      {zh(`${preprocessed.total} 部`, `${preprocessed.total} work(s)`)}
                    </span>
                    <button
                      type="button"
                      disabled={
                        !Number(preprocessed.global_total ?? preprocessed.total ?? 0) ||
                        clearingPreprocessed
                      }
                      onClick={clearPreprocessed}
                      className="rounded-lg border border-rose-200 px-3 py-1.5 text-xs font-semibold text-rose-600 transition hover:bg-rose-50 disabled:cursor-not-allowed disabled:border-slate-200 disabled:text-slate-300"
                    >
                      {clearingPreprocessed
                        ? zh('清空中…', 'Clearing…')
                        : zh('全局清空', 'Clear globally')}
                    </button>
                  </div>
                </div>
                <form onSubmit={searchPreprocessed} className="mt-4 flex max-w-xl gap-2">
                  <input
                    type="search"
                    value={preprocessedQuery}
                    onChange={(event) => setPreprocessedQuery(event.target.value)}
                    placeholder={zh('搜索番号', 'Search code')}
                    className="min-w-0 flex-1 rounded-lg border border-slate-300 bg-slate-50 px-3 py-2 text-sm outline-none transition focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100"
                  />
                  <button
                    type="submit"
                    disabled={preprocessedLoading}
                    className="rounded-lg bg-slate-800 px-4 py-2 text-sm font-semibold text-white transition hover:bg-slate-900 disabled:opacity-50"
                  >
                    {zh('检索', 'Search')}
                  </button>
                </form>
              </header>

              {preprocessedError ? (
                <div className="m-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                  {preprocessedError}
                </div>
              ) : null}
              {preprocessedLoading ? (
                <div className="px-5 py-14 text-center text-sm text-slate-400">
                  {zh('读取预处理作品中…', 'Loading preprocessed works…')}
                </div>
              ) : preprocessed.items?.length ? (
                <div className="grid gap-3 bg-slate-50/60 p-4 sm:grid-cols-2 xl:grid-cols-3">
                  {preprocessed.items.map((item) => (
                    <article
                      key={item.id}
                      className="rounded-xl border border-slate-200 bg-white px-4 py-3 shadow-sm"
                    >
                      <div className="font-mono text-base font-semibold text-emerald-700">
                        {item.code}
                      </div>
                      <div className="mt-1.5 text-xs text-slate-400">
                        {zh(
                          `来源批次 #${item.batch_id} · 第 ${item.line_number} 行`,
                          `Source batch #${item.batch_id} · line ${item.line_number}`
                        )}
                      </div>
                    </article>
                  ))}
                </div>
              ) : (
                <div className="px-5 py-16 text-center text-sm text-slate-400">
                  {appliedPreprocessedQuery
                    ? zh('没有匹配的预处理作品', 'No matching preprocessed works')
                    : zh(
                        '两道去重后还没有待处理的最终结果',
                        'No final result remains after both stages yet'
                      )}
                </div>
              )}

              {preprocessed.total > preprocessed.page_size ? (
                <footer className="flex items-center justify-between border-t border-slate-100 px-5 py-3 text-xs">
                  <button
                    type="button"
                    disabled={preprocessed.page <= 1 || preprocessedLoading}
                    onClick={() =>
                      loadPreprocessed(preprocessed.page - 1, appliedPreprocessedQuery)
                    }
                    className="text-indigo-600 disabled:text-slate-300"
                  >
                    {zh('上一页', 'Previous')}
                  </button>
                  <span className="text-slate-500">
                    {preprocessed.page} / {preprocessedPages}
                  </span>
                  <button
                    type="button"
                    disabled={preprocessed.page >= preprocessedPages || preprocessedLoading}
                    onClick={() =>
                      loadPreprocessed(preprocessed.page + 1, appliedPreprocessedQuery)
                    }
                    className="text-indigo-600 disabled:text-slate-300"
                  >
                    {zh('下一页', 'Next')}
                  </button>
                </footer>
              ) : null}
            </section>
          )}
        </div>
      </div>
    </AppModal>
  )
}
