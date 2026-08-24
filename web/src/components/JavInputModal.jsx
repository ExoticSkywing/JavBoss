import { useMemo, useState } from 'react'
import AppModal from '@/components/AppModal'
import { resolveJavInput } from '@/api'
import { getErrorMessage } from '@/utils/errors'
import { zh } from '@/utils/i18n'

function parseNumbers(value) {
  return Array.from(
    new Set(
      String(value || '')
        .split(/[\s,，;；]+/)
        .map((item) => item.trim())
        .filter(Boolean)
    )
  )
}

function formatSize(value) {
  const size = Number(value)
  if (!Number.isFinite(size) || size <= 0) return zh('未知', 'Unknown')
  if (size >= 1024) return `${(size / 1024).toFixed(1)} GB`
  return `${Math.round(size)} MB`
}

function parseSizeMiB(value) {
  const text = String(value || '')
    .trim()
    .toUpperCase()
    .replaceAll(' ', '')
  if (!text) return null
  const match = text.match(/^(\d+(?:\.\d+)?)(GB|G|MB|M)?$/)
  if (!match) return null
  const amount = Number(match[1])
  if (!Number.isFinite(amount) || amount <= 0) return null
  return amount * (match[2] === 'GB' || match[2] === 'G' ? 1024 : 1)
}

function magnetKey(magnet) {
  return String(magnet?.hash || magnet?.name || '')
}

function sortMagnets(magnets, sort) {
  return [...magnets].sort((left, right) => {
    if (sort === 'size_asc') return Number(left.size || 0) - Number(right.size || 0)
    if (sort === 'size_desc') return Number(right.size || 0) - Number(left.size || 0)
    if (sort === 'created_desc') {
      return String(right.created_at || '').localeCompare(String(left.created_at || ''))
    }
    if (Boolean(left.cnsub) !== Boolean(right.cnsub)) return left.cnsub ? -1 : 1
    if (Boolean(left.hd) !== Boolean(right.hd)) return left.hd ? -1 : 1
    return Number(right.size || 0) - Number(left.size || 0)
  })
}

function movieLabel(item) {
  const movie = item?.movie
  if (!movie) return item?.input_code || ''
  return [movie.number || item.input_code, movie.title].filter(Boolean).join(' · ')
}

export default function JavInputModal({ open, onClose }) {
  const [input, setInput] = useState('')
  const [response, setResponse] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [minSize, setMinSize] = useState('')
  const [maxSize, setMaxSize] = useState('')
  const [hdOnly, setHdOnly] = useState(false)
  const [cnsubOnly, setCnsubOnly] = useState(false)
  const [sort, setSort] = useState('quality')
  const [selected, setSelected] = useState(() => new Set())

  const numbers = useMemo(() => parseNumbers(input), [input])
  const items = useMemo(() => (Array.isArray(response?.items) ? response.items : []), [response])
  const visibleItems = useMemo(() => {
    const min = parseSizeMiB(minSize)
    const max = parseSizeMiB(maxSize)
    const hasMin = min !== null
    const hasMax = max !== null
    return items.map((item) => {
      const magnets = sortMagnets(
        (Array.isArray(item.magnets) ? item.magnets : []).filter((magnet) => {
          const size = Number(magnet.size || 0)
          if (hdOnly && !magnet.hd) return false
          if (cnsubOnly && !magnet.cnsub) return false
          if (hasMin && size < min) return false
          if (hasMax && size > max) return false
          return true
        }),
        sort
      )
      return { ...item, visibleMagnets: magnets }
    })
  }, [cnsubOnly, hdOnly, items, maxSize, minSize, sort])

  const visibleMagnetCount = visibleItems.reduce(
    (total, item) => total + item.visibleMagnets.length,
    0
  )

  const toggleSelected = (magnet) => {
    const key = magnetKey(magnet)
    if (!key) return
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const selectedMagnets = visibleItems.flatMap((item) =>
    item.visibleMagnets
      .filter((magnet) => selected.has(magnetKey(magnet)))
      .map((magnet) => ({ ...magnet, code: item.movie?.number || item.input_code }))
  )

  const copySelected = async () => {
    const text = selectedMagnets
      .map((magnet) => magnet.uri || `magnet:?xt=urn:btih:${magnet.hash}`)
      .join('\n')
    if (!text) return
    await navigator.clipboard?.writeText(text)
  }

  const submit = async (event) => {
    event.preventDefault()
    if (!numbers.length || loading) return
    setLoading(true)
    setError('')
    setSelected(new Set())
    try {
      setResponse(await resolveJavInput(numbers))
    } catch (requestError) {
      setError(getErrorMessage(requestError))
      setResponse(null)
    } finally {
      setLoading(false)
    }
  }

  return (
    <AppModal
      open={open}
      onClose={onClose}
      ariaLabel={zh('资源输入', 'Resource input')}
      contentClassName="max-h-[92vh] w-[min(1180px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-slate-50 shadow-2xl"
    >
      <div className="flex max-h-[92vh] flex-col">
        <header className="flex items-start justify-between gap-4 border-b border-slate-200 bg-white px-6 py-5">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">
              {zh('资源输入 · JavDB 磁链候选', 'Resource input · JavDB magnet candidates')}
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              {zh(
                '这里只查询和筛选，不提交下载，也不会修改 JavBoss 作品库。',
                'This step only discovers and filters candidates. It does not submit downloads or modify the library.'
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
        </header>

        <form onSubmit={submit} className="space-y-4 overflow-y-auto px-6 py-5">
          <label className="block">
            <span className="mb-1 block text-sm font-medium text-slate-700">
              {zh('番号（可批量粘贴）', 'JAV codes (paste multiple)')}
            </span>
            <textarea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              rows={3}
              placeholder="DPMX-004\nSSIS-589"
              className="w-full resize-y rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm outline-none transition focus:border-indigo-500 focus:ring-2 focus:ring-indigo-100"
            />
            <span className="mt-1 block text-xs text-slate-500">
              {zh(`${numbers.length} 个番号`, `${numbers.length} code(s)`)}
            </span>
          </label>
          <div className="flex flex-wrap items-end gap-3 rounded-xl border border-slate-200 bg-white p-4">
            <label className="text-sm text-slate-700">
              <span className="mb-1 block text-xs text-slate-500">
                {zh('最小体积', 'Min size')}
              </span>
              <input
                value={minSize}
                onChange={(event) => setMinSize(event.target.value)}
                placeholder="5GB"
                className="w-28 rounded border border-slate-300 px-2 py-1.5"
              />
            </label>
            <label className="text-sm text-slate-700">
              <span className="mb-1 block text-xs text-slate-500">
                {zh('最大体积', 'Max size')}
              </span>
              <input
                value={maxSize}
                onChange={(event) => setMaxSize(event.target.value)}
                placeholder="10GB"
                className="w-28 rounded border border-slate-300 px-2 py-1.5"
              />
            </label>
            <label className="flex items-center gap-2 pb-1 text-sm text-slate-700">
              <input
                type="checkbox"
                checked={hdOnly}
                onChange={(event) => setHdOnly(event.target.checked)}
              />
              {zh('仅高清', 'HD only')}
            </label>
            <label className="flex items-center gap-2 pb-1 text-sm text-slate-700">
              <input
                type="checkbox"
                checked={cnsubOnly}
                onChange={(event) => setCnsubOnly(event.target.checked)}
              />
              {zh('仅中文字幕', 'Chinese subtitles')}
            </label>
            <label className="text-sm text-slate-700">
              <span className="mb-1 block text-xs text-slate-500">{zh('排序', 'Sort')}</span>
              <select
                value={sort}
                onChange={(event) => setSort(event.target.value)}
                className="rounded border border-slate-300 bg-white px-2 py-1.5"
              >
                <option value="quality">{zh('字幕 / 高清 / 体积', 'Sub / HD / size')}</option>
                <option value="size_desc">{zh('体积从大到小', 'Size descending')}</option>
                <option value="size_asc">{zh('体积从小到大', 'Size ascending')}</option>
                <option value="created_desc">{zh('创建时间', 'Created date')}</option>
              </select>
            </label>
            <button
              type="submit"
              disabled={loading || !numbers.length}
              className="ml-auto rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {loading ? zh('查询中…', 'Looking up…') : zh('查询 JavDB', 'Query JavDB')}
            </button>
          </div>
          <p className="text-xs text-slate-500">
            {zh(
              '体积可输入 500MB、5GB；不写单位时按 MB 处理。',
              'Size accepts values such as 500MB or 5GB; values without a unit are treated as MB.'
            )}
          </p>
          {error ? (
            <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
              {error}
            </div>
          ) : null}

          {response ? (
            <div className="flex items-center justify-between rounded-lg border border-indigo-100 bg-indigo-50 px-4 py-3 text-sm text-indigo-800">
              <span>
                {zh(
                  `${items.length} 个番号，${visibleMagnetCount} 条候选`,
                  `${items.length} code(s), ${visibleMagnetCount} candidate(s)`
                )}
              </span>
              <div className="flex items-center gap-2">
                <span>
                  {zh(`已选 ${selectedMagnets.length}`, `${selectedMagnets.length} selected`)}
                </span>
                <button
                  type="button"
                  onClick={copySelected}
                  disabled={!selectedMagnets.length}
                  className="rounded border border-indigo-200 bg-white px-3 py-1.5 text-xs font-medium text-indigo-700 disabled:opacity-40"
                >
                  {zh('复制磁链', 'Copy magnets')}
                </button>
              </div>
            </div>
          ) : null}

          <div className="space-y-4">
            {visibleItems.map((item) => (
              <section
                key={item.input_code}
                className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm"
              >
                <div className="border-b border-slate-100 px-4 py-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="font-semibold text-slate-900">{movieLabel(item)}</h3>
                    <span className="text-xs text-slate-500">
                      {item.error || `${item.visibleMagnets.length} candidates`}
                    </span>
                  </div>
                  {item.movie?.release_date ? (
                    <p className="mt-1 text-xs text-slate-500">{item.movie.release_date}</p>
                  ) : null}
                </div>
                {item.visibleMagnets.length ? (
                  <div className="divide-y divide-slate-100">
                    {item.visibleMagnets.map((magnet) => {
                      const key = magnetKey(magnet)
                      const checked = selected.has(key)
                      return (
                        <label
                          key={key}
                          className={`flex cursor-pointer items-center gap-3 px-4 py-3 transition ${checked ? 'bg-indigo-50' : 'hover:bg-slate-50'}`}
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggleSelected(magnet)}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium text-slate-800">
                              {magnet.name || magnet.hash}
                            </span>
                            <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
                              <span>{formatSize(magnet.size)}</span>
                              <span className={magnet.hd ? 'font-medium text-emerald-700' : ''}>
                                {magnet.hd ? 'HD' : 'SD'}
                              </span>
                              <span className={magnet.cnsub ? 'font-medium text-indigo-700' : ''}>
                                {magnet.cnsub
                                  ? zh('中字', 'CN sub')
                                  : zh('无中字标记', 'No CN sub flag')}
                              </span>
                              <span>
                                {magnet.files_count || magnet.files || 0} {zh('文件', 'file(s)')}
                              </span>
                              {magnet.created_at ? <span>{magnet.created_at}</span> : null}
                            </span>
                          </span>
                          <code className="hidden max-w-[28rem] truncate text-[11px] text-slate-400 xl:block">
                            magnet:?xt=urn:btih:{magnet.hash}
                          </code>
                        </label>
                      )
                    })}
                  </div>
                ) : (
                  <div className="px-4 py-5 text-sm text-slate-500">
                    {item.error ||
                      zh('当前筛选条件下没有候选', 'No candidates match the current filters')}
                  </div>
                )}
              </section>
            ))}
          </div>
        </form>
      </div>
    </AppModal>
  )
}
