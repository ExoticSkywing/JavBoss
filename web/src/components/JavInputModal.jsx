import { useEffect, useMemo, useRef, useState } from 'react'
import AppModal from '@/components/AppModal'
import { createJavInputBatch } from '@/api'
import { getErrorMessage } from '@/utils/errors'
import { zh } from '@/utils/i18n'
import { countJavInputLines, getJavInputReceipt, JAV_INPUT_RECEIPT_KIND } from '@/utils/javInput'

function receiptCopy(receipt) {
  switch (receipt.kind) {
    case JAV_INPUT_RECEIPT_KIND.all:
      return {
        eyebrow: zh('全部新增', 'All new'),
        title: zh(
          `${receipt.addedCount} 部作品已加入作品库`,
          `${receipt.addedCount} work(s) added to the library`
        ),
        description: zh(
          '本次识别出的作品都是首次出现，现已标记为未入库并开始补全元数据。',
          'Every recognized work was new. They are now marked as pending while metadata is collected.'
        ),
        tone: 'emerald',
      }
    case JAV_INPUT_RECEIPT_KIND.partial:
      return {
        eyebrow: zh('部分新增', 'Partly new'),
        title: zh(
          `${receipt.addedCount} 部新作品已加入作品库`,
          `${receipt.addedCount} new work(s) added to the library`
        ),
        description: zh(
          `另外 ${receipt.existingCount} 部已经存在，系统已自动合并，没有创建重复作品。`,
          `${receipt.existingCount} other work(s) already existed and were merged automatically; no duplicates were created.`
        ),
        tone: 'blue',
      }
    default:
      return {
        eyebrow: zh('无新增', 'Nothing new'),
        title: zh('作品库没有发生变化', 'The library did not change'),
        description: zh(
          `本次识别出的 ${receipt.existingCount} 部作品都已经存在，无需再次处理。`,
          `All ${receipt.existingCount} recognized work(s) already existed, so nothing needed to be added.`
        ),
        tone: 'slate',
      }
  }
}

function ParseWarnings({ items }) {
  if (!items.length) return null
  return (
    <details className="rounded-xl border border-amber-200 bg-amber-50/70 text-sm text-amber-950">
      <summary className="min-h-11 cursor-pointer select-none px-4 py-3 font-medium hover:bg-amber-100/60">
        {zh(
          `${items.length} 处内容可能包含未识别的番号`,
          `${items.length} fragment(s) may contain an unrecognized code`
        )}
      </summary>
      <div className="border-t border-amber-200 px-4 py-3">
        <p className="text-xs leading-5 text-amber-800">
          {zh(
            '这些内容没有加入作品库；确认确实是番号后，可以修正格式再输入。纯文字备注已自动忽略。',
            'These fragments were not added. If they contain a code, correct the format and enter it again. Plain-text notes were ignored automatically.'
          )}
        </p>
        <ul className="mt-3 space-y-2">
          {items.map((item, index) => (
            <li
              key={item?.id || `${item?.line_number || 0}-${index}`}
              className="rounded-lg border border-amber-200 bg-white px-3 py-2"
            >
              <div className="text-xs tabular-nums text-amber-700">
                {zh(`第 ${item?.line_number || '?'} 行`, `Line ${item?.line_number || '?'}`)}
              </div>
              <div className="mt-1 whitespace-pre-wrap break-words font-mono text-xs leading-5 text-slate-700">
                {item?.raw_line || '—'}
              </div>
            </li>
          ))}
        </ul>
      </div>
    </details>
  )
}

function InputReceipt({ batch, onContinue, onViewPending }) {
  const receipt = useMemo(() => getJavInputReceipt(batch), [batch])
  const copy = receiptCopy(receipt)
  const toneClasses = {
    emerald: {
      panel: 'border-emerald-200 bg-emerald-50/70',
      eyebrow: 'text-emerald-700',
      count: 'text-emerald-700',
    },
    blue: {
      panel: 'border-blue-200 bg-blue-50/70',
      eyebrow: 'text-blue-700',
      count: 'text-blue-700',
    },
    slate: {
      panel: 'border-slate-200 bg-slate-50',
      eyebrow: 'text-slate-600',
      count: 'text-slate-700',
    },
  }
  const tone = toneClasses[copy.tone]

  return (
    <div className="space-y-4">
      <section className={`rounded-2xl border p-5 sm:p-6 ${tone.panel}`} role="status">
        <div className={`text-xs font-bold tracking-wide ${tone.eyebrow}`}>{copy.eyebrow}</div>
        <h3 className="mt-1 text-xl font-bold leading-tight text-slate-950 sm:text-2xl">
          {copy.title}
        </h3>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-600">{copy.description}</p>

        <dl className="mt-5 grid grid-cols-3 overflow-hidden rounded-xl border border-white/80 bg-white/80 shadow-sm">
          <div className="border-r border-slate-100 px-4 py-3">
            <dt className="text-xs text-slate-500">{zh('识别作品', 'Recognized')}</dt>
            <dd className="mt-1 text-2xl font-bold tabular-nums text-slate-900">
              {receipt.uniqueCount}
            </dd>
          </div>
          <div className="border-r border-slate-100 px-4 py-3">
            <dt className="text-xs text-slate-500">{zh('本次新增', 'Newly added')}</dt>
            <dd className={`mt-1 text-2xl font-bold tabular-nums ${tone.count}`}>
              {receipt.addedCount}
            </dd>
          </div>
          <div className="px-4 py-3">
            <dt className="text-xs text-slate-500">{zh('已经存在', 'Already existed')}</dt>
            <dd className="mt-1 text-2xl font-bold tabular-nums text-slate-600">
              {receipt.existingCount}
            </dd>
          </div>
        </dl>
      </section>

      <ParseWarnings items={receipt.invalidItems} />

      <div className="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button
          type="button"
          onClick={onContinue}
          className="min-h-11 rounded-xl border border-slate-300 bg-white px-5 text-sm font-semibold text-slate-700 transition hover:bg-slate-50 active:scale-[0.98]"
        >
          {zh('继续输入', 'Enter more')}
        </button>
        <button
          type="button"
          onClick={onViewPending}
          className="min-h-11 rounded-xl bg-indigo-600 px-5 text-sm font-semibold text-white transition hover:bg-indigo-700 active:scale-[0.98]"
        >
          {zh('查看未入库作品', 'View pending works')}
        </button>
      </div>
    </div>
  )
}

export default function JavInputModal({ open, onClose, onCompleted, onViewPending }) {
  const inputRef = useRef(null)
  const [input, setInput] = useState('')
  const [result, setResult] = useState(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const inputLineCount = useMemo(() => countJavInputLines(input), [input])

  useEffect(() => {
    if (open) return
    setInput('')
    setResult(null)
    setSaving(false)
    setError('')
  }, [open])

  useEffect(() => {
    if (!open || result) return undefined
    const frame = window.requestAnimationFrame(() => inputRef.current?.focus())
    return () => window.cancelAnimationFrame(frame)
  }, [open, result])

  const submit = async (event) => {
    event.preventDefault()
    if (!input.trim() || saving) return
    setSaving(true)
    setError('')
    try {
      const batch = await createJavInputBatch(input)
      const receipt = getJavInputReceipt(batch)
      if (!receipt.recognized) {
        setError(
          zh(
            '没有识别到番号，请检查格式后再试。',
            'No JAV code was recognized. Check the format and try again.'
          )
        )
        return
      }
      setInput('')
      setResult(batch)
      onCompleted?.(batch)
    } catch (requestError) {
      setError(getErrorMessage(requestError))
    } finally {
      setSaving(false)
    }
  }

  const continueInput = () => {
    setResult(null)
    setError('')
    window.requestAnimationFrame(() => inputRef.current?.focus())
  }

  return (
    <AppModal
      open={open}
      onClose={onClose}
      ariaLabel={zh('加入作品库', 'Add works to library')}
      contentClassName="max-h-[94vh] w-[min(760px,calc(100vw-2rem))] overflow-hidden rounded-2xl bg-white shadow-2xl"
    >
      <div className="flex max-h-[94vh] flex-col">
        <header className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4 sm:px-6">
          <div>
            <h2 className="text-lg font-semibold text-slate-950">
              {zh('加入作品库', 'Add works to library')}
            </h2>
            <p className="mt-1 max-w-2xl text-sm leading-6 text-slate-500">
              {zh(
                '直接粘贴原始内容。系统会识别番号、统一格式并与整个作品库合并，只加入从未出现过的作品。',
                'Paste the source as-is. JavBoss recognizes codes, normalizes them, and merges them into one library, adding only works never seen before.'
              )}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="min-h-9 shrink-0 rounded-lg px-2 text-sm text-slate-500 transition hover:bg-slate-100 hover:text-slate-900"
          >
            {zh('关闭', 'Close')}
          </button>
        </header>

        <div className="overflow-y-auto p-5 sm:p-6">
          {result ? (
            <InputReceipt batch={result} onContinue={continueInput} onViewPending={onViewPending} />
          ) : (
            <form onSubmit={submit}>
              <label className="block">
                <span className="text-sm font-semibold text-slate-800">
                  {zh('番号原始内容', 'Raw code input')}
                </span>
                <span className="mt-1 block text-xs leading-5 text-slate-500">
                  {zh(
                    '一行一个、同一行用空格或逗号分隔、夹杂标题和备注都可以，无需先整理或去重。',
                    'One per line, several separated by spaces or commas, and surrounding titles or notes are all accepted. No cleanup or de-duplication is needed.'
                  )}
                </span>
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(event) => setInput(event.target.value)}
                  rows={12}
                  placeholder={'VRTM-138 CORE-018\n收藏清单第二批\nEBOD-502，JUFD-366'}
                  className="mt-3 w-full resize-y rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 font-mono text-sm leading-6 outline-none transition focus:border-indigo-500 focus:bg-white focus:ring-2 focus:ring-indigo-100"
                />
              </label>

              <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                <span className="text-xs tabular-nums text-slate-500">
                  {inputLineCount
                    ? zh(`${inputLineCount} 个非空行`, `${inputLineCount} non-empty line(s)`)
                    : zh('等待输入', 'Waiting for input')}
                </span>
                <button
                  type="submit"
                  disabled={saving || !input.trim()}
                  className="min-h-11 rounded-xl bg-indigo-600 px-6 text-sm font-semibold text-white transition hover:bg-indigo-700 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {saving ? zh('正在合并…', 'Merging…') : zh('加入作品库', 'Add to library')}
                </button>
              </div>

              {error ? (
                <div
                  role="alert"
                  className="mt-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700"
                >
                  {error}
                </div>
              ) : null}
            </form>
          )}
        </div>
      </div>
    </AppModal>
  )
}
