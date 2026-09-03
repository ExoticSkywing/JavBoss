import { useEffect, useMemo, useRef, useState } from 'react'
import CloseOutlinedIcon from '@mui/icons-material/CloseOutlined'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline'
import FavoriteBorderRoundedIcon from '@mui/icons-material/FavoriteBorderRounded'
import FavoriteRoundedIcon from '@mui/icons-material/FavoriteRounded'
import { MovieEdit } from '@mui/icons-material'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import RemoveCircleOutlineRoundedIcon from '@mui/icons-material/RemoveCircleOutlineRounded'
import StarBorderRoundedIcon from '@mui/icons-material/StarBorderRounded'
import StarRoundedIcon from '@mui/icons-material/StarRounded'
import { IconButton, Popper, Rating, Tooltip } from '@mui/material'

import {
  deleteVideoScreenshot,
  correctJavCode,
  collectJavMagnets,
  fetchJavItem,
  fetchJavTrailer,
  fetchVideoScreenshotsByIds,
  getResolvedJavSampleImages,
  javSampleImageURL,
  resolveJavSampleImages,
  saveJavQualityReviewDecision,
  selectJavMagnet,
  submitJavDownloadBatch,
} from '@/api'
import AppModal from '@/components/AppModal'
import JavTrailerModal from '@/components/JavTrailerModal'
import { IdolCard, getIdolCardLayoutProps } from '@/components/JavIdolGrid'
import { SeriesCard } from '@/components/JavSeriesView'
import { StudioCard } from '@/components/JavStudioView'
import VideoGrid from '@/components/VideoGrid'
import { ScreenshotPreviewModal } from '@/components/VideoScreenshotsModal'
import { isUserJavTag } from '@/constants/jav'
import { getVideoDisplayName } from '@/utils/display'
import { getIdolDisplayName } from '@/utils/javIdol'
import { zh } from '@/utils/i18n'
import { getErrorMessage } from '@/utils/errors'

function formatScreenshotTime(name) {
  const stem = String(name || '')
    .replace(/\.[^.]+$/, '')
    .replace(/^mpv_/, '')
  const match = stem.match(/^(\d{2})-(\d{2})-(\d{2})(\.\d+)?$/)
  if (!match) return stem || name
  return `${match[1]}:${match[2]}:${match[3]}`
}

function screenshotStartTime(name) {
  const stem = String(name || '')
    .replace(/\.[^.]+$/, '')
    .replace(/^mpv_/, '')
  const match = stem.match(/^(\d{2})-(\d{2})-(\d{2})(\.\d+)?$/)
  if (!match) return null
  return (
    Number.parseInt(match[1], 10) * 3600 +
    Number.parseInt(match[2], 10) * 60 +
    Number.parseInt(match[3], 10) +
    Number.parseFloat(match[4] || '0')
  )
}

function screenshotActionKey(video, screenshot) {
  return `${video?.id || 'video'}:${screenshot?.name || ''}`
}

function screenshotItemsMatch(current, next) {
  if (current.length !== next.length) return false
  return current.every((item, index) => {
    const candidate = next[index]
    return (
      Number(item?.video?.id) === Number(candidate?.video?.id) &&
      item?.name === candidate?.name &&
      item?.url === candidate?.url &&
      Boolean(item?.is_cover) === Boolean(candidate?.is_cover)
    )
  })
}

function normalizeSampleImages(images) {
  if (!Array.isArray(images)) return []
  const seen = new Set()
  return images.flatMap((image) => {
    const thumbnailURL = String(image?.thumbnail_url || image?.detail_url || '').trim()
    const detailURL = String(image?.detail_url || image?.thumbnail_url || '').trim()
    if (thumbnailURL === ':not_found' || detailURL === ':not_found') return []
    if (!thumbnailURL || !detailURL) return []
    const key = `${thumbnailURL}\u0000${detailURL}`
    if (seen.has(key)) return []
    seen.add(key)
    return [{ thumbnail_url: thumbnailURL, detail_url: detailURL }]
  })
}

function sampleImagesNotFound(images) {
  return (
    Array.isArray(images) &&
    images.length === 1 &&
    images[0]?.thumbnail_url === ':not_found' &&
    images[0]?.detail_url === ':not_found'
  )
}

function formatMagnetSize(sizeMiB) {
  const value = Number(sizeMiB)
  if (!Number.isFinite(value) || value <= 0) return '—'
  if (value >= 1024) return `${(value / 1024).toFixed(value >= 10 * 1024 ? 0 : 1)} GB`
  return `${Math.round(value)} MB`
}

function magnetStageLabel(stage) {
  return (
    {
      metadata_pending: zh('正在补全元数据', 'Collecting metadata'),
      code_review: zh('待核对番号', 'Code needs review'),
      magnet_collecting: zh('正在收集磁链', 'Collecting magnets'),
      magnet_review: zh('待筛选磁链', 'Magnet review'),
      ready_to_download: zh('等待提交下载', 'Ready to download'),
      download_submitted: zh('已提交下载', 'Download submitted'),
      quality_review: zh('暂存待验收', 'Staged for review'),
      awaiting_scan: zh('质量通过 · 等待扫盘', 'Approved · Awaiting scan'),
      imported: zh('已确认入库', 'Accepted import'),
    }[String(stage || '').trim()] || zh('等待处理', 'Awaiting processing')
  )
}

function needsJavCodeCorrection(item) {
  const stage = String(item?.acquisition_stage || '').trim()
  return (
    stage === 'code_review' || (stage === 'magnet_collecting' && !String(item?.title || '').trim())
  )
}

const magnetReviewFacts = [
  { key: 'qualityClear', label: zh('清晰度达标', 'Clear quality') },
  { key: 'hasIntroAd', label: zh('片头广告', 'Intro ad') },
  { key: 'hasWatermark', label: zh('水印', 'Watermark') },
  { key: 'hasMarquee', label: zh('跑马灯', 'Marquee') },
  { key: 'isUncensored', label: zh('无码', 'Uncensored') },
]

const magnetRejectionReasons = [
  { value: 'low_clarity', label: zh('清晰度不达标', 'Clarity below standard') },
  { value: 'intro_ad', label: zh('片头广告', 'Intro ad') },
  { value: 'watermark', label: zh('水印', 'Watermark') },
  { value: 'marquee', label: zh('跑马灯', 'Marquee') },
  { value: 'too_large', label: zh('体积过大', 'Too large') },
  { value: 'too_small', label: zh('体积过小', 'Too small') },
]

const rejectionReasonLabels = new Map([
  ...magnetRejectionReasons.map((reason) => [reason.value, reason.label]),
  // Keep historical records readable after the duplicate 1080P label is removed.
  ['not_1080p', zh('清晰度不达标', 'Clarity below standard')],
])

function formatRejectionReasons(value) {
  const reasons = String(value || '')
    .split(',')
    .map((reason) => reason.trim())
    .filter(Boolean)
  if (reasons.length === 0) return zh('未填写原因', 'No reason recorded')
  return reasons.map((reason) => rejectionReasonLabels.get(reason) || reason).join('、')
}

function reviewFactValue(value) {
  if (value === true) return 'yes'
  if (value === false) return 'no'
  return ''
}

function parseReviewFact(value) {
  if (value === 'yes') return true
  if (value === 'no') return false
  return null
}

export function JavCodeCorrectionModal({ open, item, directoryIds, onClose, onSaved }) {
  const [code, setCode] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const savingRef = useRef(false)

  useEffect(() => {
    if (!open) return
    setCode(String(item?.code || ''))
    setSaving(false)
    savingRef.current = false
    setError('')
  }, [item?.code, open])

  if (!open) return null

  const handleSave = async () => {
    if (savingRef.current) return
    const nextCode = code.trim()
    if (!nextCode) {
      setError(zh('请输入番号', 'Enter a JAV code'))
      return
    }
    savingRef.current = true
    setSaving(true)
    setError('')
    try {
      const updated = await correctJavCode(item.id, nextCode, { directoryIds })
      onSaved?.(updated)
    } catch (requestError) {
      const conflictingJavID = Number(requestError?.payload?.conflicting_jav_id)
      const suffix =
        Number.isFinite(conflictingJavID) && conflictingJavID > 0
          ? `（作品 #${conflictingJavID}）`
          : ''
      setError(`${getErrorMessage(requestError)}${suffix}`)
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }

  return (
    <AppModal
      ariaLabel={zh('修正番号', 'Correct JAV code')}
      className="p-4"
      closeDisabled={saving}
      contentClassName="w-full max-w-md rounded-lg bg-white shadow-2xl"
      onClose={onClose}
      zIndex={1700}
    >
      <div className="flex items-start justify-between gap-3 px-5 pt-5">
        <div>
          <h2 className="text-base font-semibold text-gray-900">
            {zh('修正番号', 'Correct JAV code')}
          </h2>
          <p className="mt-1 text-xs leading-5 text-gray-500">
            {zh(
              '只修改这部作品的番号，原始输入记录会保留。保存后会重新开始资料匹配。',
              'Only this work code will change. The original input receipt stays intact, and metadata matching starts again after saving.'
            )}
          </p>
        </div>
        <button
          type="button"
          className="-mr-2 -mt-1 rounded px-2 py-1 text-xl leading-none text-gray-400 hover:bg-gray-100 hover:text-gray-800"
          onClick={onClose}
          disabled={saving}
          aria-label={zh('关闭', 'Close')}
        >
          ×
        </button>
      </div>
      <div className="px-5 py-5">
        <label
          htmlFor={`jav-code-correction-${item?.id || 'item'}`}
          className="block text-sm font-medium text-gray-700"
        >
          {zh('正确番号', 'Correct code')}
        </label>
        <input
          id={`jav-code-correction-${item?.id || 'item'}`}
          type="text"
          value={code}
          onChange={(event) => {
            setCode(event.target.value)
            if (error) setError('')
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
              event.preventDefault()
              void handleSave()
            }
          }}
          spellCheck="false"
          placeholder="例如：IPX-001"
          className="mt-2 w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-sm uppercase text-gray-900 outline-none transition-[border-color,box-shadow] focus:border-stone-500 focus:ring-2 focus:ring-stone-100"
          disabled={saving}
        />
        {error ? (
          <div role="alert" className="mt-2 text-xs leading-5 text-rose-700">
            {error}
          </div>
        ) : null}
      </div>
      <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-4">
        <button
          type="button"
          className="rounded-md border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          onClick={onClose}
          disabled={saving}
        >
          {zh('取消', 'Cancel')}
        </button>
        <button
          type="button"
          className="rounded-md bg-stone-800 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-stone-900 active:scale-[0.96] disabled:cursor-wait disabled:opacity-50"
          onClick={() => void handleSave()}
          disabled={saving}
        >
          {saving ? zh('保存中…', 'Saving…') : zh('保存并重新匹配', 'Save and rematch')}
        </button>
      </div>
    </AppModal>
  )
}

function JavMagnetSection({ item, directoryIds, onAcquisitionUpdated }) {
  const [detail, setDetail] = useState(item)
  const [loading, setLoading] = useState(true)
  const [collecting, setCollecting] = useState(false)
  const [savingSelection, setSavingSelection] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [reviewing, setReviewing] = useState(false)
  const [reviewReason, setReviewReason] = useState('')
  const [selectedReasons, setSelectedReasons] = useState([])
  const [reviewFacts, setReviewFacts] = useState({
    qualityClear: null,
    hasIntroAd: null,
    hasWatermark: null,
    hasMarquee: null,
    isUncensored: null,
  })
  const [selectedID, setSelectedID] = useState(Number(item?.magnet_selection?.candidate_id) || 0)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [selectionMessage, setSelectionMessage] = useState('')

  const directoryKey = (directoryIds || []).join(',')
  useEffect(() => {
    let cancelled = false
    if (!item?.id) {
      setLoading(false)
      return undefined
    }
    setLoading(true)
    setError('')
    void fetchJavItem(item.id, { directoryIds })
      .then((loaded) => {
        if (cancelled) return
        setDetail(loaded)
        const reviewCandidateID =
          String(loaded?.acquisition_stage || '') === 'quality_review'
            ? Number(loaded?.download_attempt?.candidate_id) || 0
            : 0
        setSelectedID(reviewCandidateID || Number(loaded?.magnet_selection?.candidate_id) || 0)
      })
      .catch((requestError) => {
        if (!cancelled) setError(getErrorMessage(requestError))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [item?.id, directoryKey, directoryIds])

  const magnetPollStage = String(detail?.acquisition_stage || item?.acquisition_stage || '').trim()
  const magnetPollCandidateCount = Array.isArray(detail?.magnet_candidates)
    ? detail.magnet_candidates.length
    : 0
  useEffect(() => {
    if (
      loading ||
      magnetPollCandidateCount > 0 ||
      !['metadata_pending', 'magnet_collecting'].includes(magnetPollStage) ||
      !item?.id
    ) {
      return undefined
    }
    let cancelled = false
    const refresh = () => {
      void fetchJavItem(item.id, { directoryIds })
        .then((loaded) => {
          if (cancelled) return
          setDetail(loaded)
          const nextSelectedID =
            Number(loaded?.download_attempt?.candidate_id) ||
            Number(loaded?.magnet_selection?.candidate_id) ||
            0
          if (nextSelectedID > 0) setSelectedID(nextSelectedID)
        })
        .catch(() => {
          // The main detail request already shows errors. Polling is best
          // effort and should never replace a usable detail view with a
          // transient network error.
        })
    }
    const timer = window.setInterval(refresh, 10000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [directoryIds, item?.id, loading, magnetPollCandidateCount, magnetPollStage])

  const candidates = Array.isArray(detail?.magnet_candidates) ? detail.magnet_candidates : []
  const activeCandidates = candidates.filter((candidate) => candidate?.review_status !== 'rejected')
  const rejectedCandidates = candidates.filter(
    (candidate) => candidate?.review_status === 'rejected'
  )
  const selectedCandidate = activeCandidates.find(
    (candidate) => Number(candidate?.id) === selectedID
  )
  const stage = String(detail?.acquisition_stage || item?.acquisition_stage || '').trim()
  const selectionLocked = [
    'download_submitted',
    'quality_review',
    'awaiting_scan',
    'imported',
  ].includes(stage)
  const savedSelectionID = Number(detail?.magnet_selection?.candidate_id) || 0
  const reviewAttemptCandidateID = Number(detail?.download_attempt?.candidate_id) || 0
  const codeCorrectionAvailable = needsJavCodeCorrection(detail || item) && candidates.length === 0

  useEffect(() => {
    if (!selectedCandidate) return
    setReviewFacts({
      // confirmed_1080p is a legacy fact. Use it only as a fallback so old
      // reviews remain visible after the duplicate field is removed.
      qualityClear: selectedCandidate.quality_clear ?? selectedCandidate.confirmed_1080p ?? null,
      hasIntroAd: selectedCandidate.has_intro_ad ?? null,
      hasWatermark: selectedCandidate.has_watermark ?? null,
      hasMarquee: selectedCandidate.has_marquee ?? null,
      isUncensored: selectedCandidate.is_uncensored ?? null,
    })
    setSelectedReasons(
      String(selectedCandidate.review_reasons || '')
        .split(',')
        .map((reason) => reason.trim())
        .filter(Boolean)
        .map((reason) => (reason === 'not_1080p' ? 'low_clarity' : reason))
    )
    setReviewReason(String(selectedCandidate.review_notes || ''))
  }, [selectedCandidate])

  const collect = async () => {
    setCollecting(true)
    setError('')
    setMessage('')
    setSelectionMessage('')
    try {
      const result = await collectJavMagnets(item.id, { directoryIds })
      setDetail((current) => ({
        ...current,
        magnet_candidates: result?.candidates || [],
        acquisition_stage:
          result?.candidates?.length &&
          ['', 'metadata_pending', 'magnet_collecting'].includes(
            String(current?.acquisition_stage || '')
          )
            ? 'magnet_review'
            : current?.acquisition_stage,
      }))
      if (
        result?.candidates?.length &&
        ['', 'metadata_pending', 'magnet_collecting'].includes(
          String(detail?.acquisition_stage || '')
        )
      ) {
        onAcquisitionUpdated?.({ acquisition_stage: 'magnet_review' })
      }
      setMessage(
        zh(
          `已保存 ${result?.candidates?.length || 0} 条候选磁链`,
          `Saved ${result?.candidates?.length || 0} magnet candidate(s)`
        )
      )
    } catch (requestError) {
      setError(getErrorMessage(requestError))
    } finally {
      setCollecting(false)
    }
  }

  const saveSelection = async () => {
    if (!selectedID) return
    setSavingSelection(true)
    setError('')
    setMessage('')
    setSelectionMessage('')
    try {
      const selection = await selectJavMagnet(item.id, selectedID)
      setDetail((current) => ({
        ...current,
        magnet_selection: selection,
        acquisition_stage: 'ready_to_download',
      }))
      onAcquisitionUpdated?.({ acquisition_stage: 'ready_to_download' })
      setSelectionMessage(
        zh('磁链选择已保存，尚未发送下载', 'Magnet selection saved; nothing has been submitted')
      )
    } catch (requestError) {
      setError(getErrorMessage(requestError))
    } finally {
      setSavingSelection(false)
    }
  }

  const submitSingle = async () => {
    if (!selectedCandidate) return
    setSubmitting(true)
    setError('')
    setMessage('')
    setSelectionMessage('')
    try {
      // The one-click path has the same semantics as “save, then submit”:
      // never send a stale previously persisted selection.
      const selection = await selectJavMagnet(item.id, selectedID)
      setDetail((current) => ({
        ...current,
        magnet_selection: selection,
        acquisition_stage: 'ready_to_download',
      }))
      const result = await submitJavDownloadBatch([item.id])
      const delivered = result?.delivery_status === 'submitted'
      setDetail((current) => ({
        ...current,
        acquisition_stage: delivered ? 'download_submitted' : 'ready_to_download',
      }))
      onAcquisitionUpdated?.({
        acquisition_stage: delivered ? 'download_submitted' : 'ready_to_download',
      })
      setMessage(
        delivered
          ? zh('已提交此作品的下载任务', 'This work was submitted for download')
          : zh(
              '云下载服务未接收这条任务，磁链选择仍保留在待发送队列。',
              'The cloud downloader did not accept this task; the magnet remains in the send queue.'
            )
      )
    } catch (requestError) {
      const originalError = getErrorMessage(requestError)
      try {
        const loaded = await fetchJavItem(item.id, { directoryIds })
        setDetail(loaded)
        setSelectedID(
          Number(loaded?.download_attempt?.candidate_id) ||
            Number(loaded?.magnet_selection?.candidate_id) ||
            selectedID
        )
      } catch {
        // Keep the original submission error. A refresh is best-effort and
        // only exists to reveal a durable `uncertain` hand-off immediately.
      }
      setError(originalError)
    } finally {
      setSubmitting(false)
    }
  }

  const saveReviewDecision = async (accepted) => {
    if (!selectedCandidate) return
    setReviewing(true)
    setError('')
    setMessage('')
    setSelectionMessage('')
    try {
      const inferredReasons = [
        reviewFacts.qualityClear === false ? 'low_clarity' : '',
        reviewFacts.hasIntroAd === true ? 'intro_ad' : '',
        reviewFacts.hasWatermark === true ? 'watermark' : '',
        reviewFacts.hasMarquee === true ? 'marquee' : '',
      ].filter(Boolean)
      const rejectedReasons = [...new Set([...selectedReasons, ...inferredReasons])]
      const reasons = accepted ? [] : rejectedReasons.length > 0 ? rejectedReasons : ['other']
      const reviewResult = await saveJavQualityReviewDecision(item.id, selectedCandidate.id, {
        accepted,
        quality_clear: reviewFacts.qualityClear,
        // Keep the legacy API field empty; clarity is the single displayed
        // quality judgment going forward.
        confirmed_1080p: null,
        has_intro_ad: reviewFacts.hasIntroAd,
        has_watermark: reviewFacts.hasWatermark,
        has_marquee: reviewFacts.hasMarquee,
        is_uncensored: reviewFacts.isUncensored,
        reasons,
        notes: reviewReason.trim(),
      })
      setDetail((current) => ({
        ...current,
        acquisition_stage: 'quality_review',
        download_attempt: { ...(current?.download_attempt || {}), ...reviewResult },
        magnet_candidates: (current?.magnet_candidates || []).map((candidate) =>
          Number(candidate.id) === Number(selectedCandidate.id)
            ? {
                ...candidate,
                review_status: candidate.review_status || 'pending',
                review_reasons: reasons.join(','),
                review_notes: reviewReason.trim(),
                quality_clear: reviewFacts.qualityClear,
                has_intro_ad: reviewFacts.hasIntroAd,
                has_watermark: reviewFacts.hasWatermark,
                has_marquee: reviewFacts.hasMarquee,
                is_uncensored: reviewFacts.isUncensored,
              }
            : candidate
        ),
      }))
      onAcquisitionUpdated?.({
        acquisition_stage: 'quality_review',
        quality_review: {
          decision: accepted ? 'accepted' : 'rejected',
          attempt_id: reviewResult?.id,
          candidate_id: selectedCandidate.id,
        },
      })
      setMessage(
        accepted
          ? zh(
              '已记录“通过”，文件仍留在待验收区；可在待验收入口统一执行',
              'Approval saved; the file remains staged until batch execution'
            )
          : zh(
              '已记录“不合格”，文件仍留在待验收区；可在待验收入口统一执行',
              'Rejection saved; the file remains staged until batch execution'
            )
      )
      setSelectedID(selectedID)
    } catch (requestError) {
      const originalError = getErrorMessage(requestError)
      try {
        // Reload the durable decision when the response is lost.
        const loaded = await fetchJavItem(item.id, { directoryIds })
        setDetail(loaded)
        setSelectedID(
          Number(loaded?.download_attempt?.candidate_id) ||
            Number(loaded?.magnet_selection?.candidate_id) ||
            0
        )
      } catch {
        // Preserve the actionable review/delete error from the first request.
      }
      setError(originalError)
    } finally {
      setReviewing(false)
    }
  }

  return (
    <section
      className="border-t border-gray-200 pt-5"
      aria-labelledby={`jav-detail-${item?.id}-magnets`}
    >
      <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3
            id={`jav-detail-${item?.id}-magnets`}
            className="text-base font-semibold text-gray-900"
          >
            {zh('磁链候选', 'Magnet candidates')}
          </h3>
          <p className="mt-1 text-xs text-gray-500">
            {zh(
              '磁链会在后台自动收集；下载完成后先记录验收决定，再从“待验收”入口批量执行。',
              'Magnets are collected automatically. After download, save a review decision first, then execute it from the quality queue in a batch.'
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600">
            {magnetStageLabel(stage)}
          </span>
          <button
            type="button"
            onClick={collect}
            disabled={loading || collecting}
            className="rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:cursor-wait disabled:opacity-50"
          >
            {collecting
              ? zh('采集中…', 'Collecting…')
              : candidates.length > 0
                ? zh('重新采集', 'Collect again')
                : zh('获取磁链', 'Collect magnets')}
          </button>
        </div>
      </div>
      {loading ? (
        <div className="rounded-lg border border-dashed border-gray-200 px-4 py-6 text-center text-xs text-gray-500">
          {zh('正在加载磁链候选…', 'Loading magnet candidates...')}
        </div>
      ) : null}
      {!loading && activeCandidates.length === 0 ? (
        <div
          className={`rounded-lg border px-4 py-5 text-center text-xs ${
            codeCorrectionAvailable
              ? 'border-amber-200 bg-amber-50/70 text-amber-900'
              : 'border-dashed border-gray-200 text-gray-500'
          }`}
        >
          {codeCorrectionAvailable ? (
            <>
              <div className="font-semibold text-amber-900">
                {zh('暂未匹配到作品资料', 'No work metadata matched this code')}
              </div>
              <p className="mt-1 leading-5 text-amber-800">
                {zh(
                  '多个资料来源都没有返回精确结果。请回到作品卡片，在“待核对番号”旁修正后重新匹配。',
                  'No provider returned an exact result. Use “Correct code” beside the card status to rematch.'
                )}
              </p>
            </>
          ) : (
            <>
              {stage === 'metadata_pending'
                ? zh(
                    '元数据仍在补全，完成后会自动收集磁链。',
                    'Metadata is still being completed; magnet collection starts automatically afterward.'
                  )
                : stage === 'magnet_collecting'
                  ? zh(
                      '磁链正在后台收集，页面会自动刷新；也可以点击“重新采集”立即重试。',
                      'Magnets are being collected in the background. This view refreshes automatically; “Collect again” retries immediately.'
                    )
                  : zh(
                      '暂未保存候选磁链，可以点击“获取磁链”手动重试。',
                      'No candidates are saved yet. Click “Collect magnets” to retry manually.'
                    )}
            </>
          )}
        </div>
      ) : null}
      {!loading && activeCandidates.length > 0 ? (
        <div
          className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3"
          aria-label={zh('磁链候选列表', 'Magnet candidate list')}
          role="radiogroup"
        >
          {activeCandidates.map((candidate) => {
            const candidateID = Number(candidate?.id)
            const checked = candidateID === selectedID
            const preferredForReview =
              candidate?.hd &&
              Number(candidate?.size_mib) >= 5120 &&
              Number(candidate?.size_mib) <= 10240
            const persisted = candidateID === savedSelectionID
            return (
              <div
                key={candidateID}
                className={`overflow-hidden rounded-lg border transition-colors duration-150 ${persisted ? 'border-stone-400 bg-stone-100/80 shadow-sm' : checked ? 'border-orange-400 bg-orange-50/60 shadow-sm' : 'border-stone-200 bg-white hover:border-stone-300'}`}
              >
                <label
                  className={`flex items-start gap-3 px-3 py-3 ${selectionLocked ? 'cursor-default' : 'cursor-pointer'}`}
                >
                  <input
                    aria-label={zh(`选择磁链 ${candidateID}`, `Select magnet ${candidateID}`)}
                    type="radio"
                    name={`jav-magnet-${item?.id}`}
                    checked={checked}
                    disabled={selectionLocked}
                    onChange={() => {
                      setSelectedID(candidateID)
                      setSelectionMessage('')
                      setMessage('')
                      setError('')
                    }}
                    className="mt-1 h-4 w-4 accent-orange-600 disabled:cursor-not-allowed"
                  />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-start justify-between gap-2">
                      <span className="min-w-0 break-words text-sm font-medium text-gray-900">
                        {candidate?.name || zh('未命名磁链', 'Unnamed magnet')}
                      </span>
                      {persisted ? (
                        <span className="shrink-0 rounded-md bg-stone-700 px-2 py-0.5 text-[10px] font-semibold text-white">
                          {zh('已保存选择', 'Saved')}
                        </span>
                      ) : checked ? (
                        <span className="shrink-0 rounded-full bg-orange-100 px-2 py-0.5 text-[10px] font-semibold text-orange-800">
                          {zh('当前选择', 'Selected')}
                        </span>
                      ) : null}
                    </span>
                    <span className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500">
                      <span>{formatMagnetSize(candidate?.size_mib)}</span>
                      <span>
                        {zh(`${candidate?.files || 0} 个文件`, `${candidate?.files || 0} file(s)`)}
                      </span>
                      {candidate?.hd ? (
                        <span className="font-medium text-stone-700">HD</span>
                      ) : null}
                      {preferredForReview ? (
                        <span className="font-semibold text-orange-700">
                          {zh('优先核验', 'Review first')}
                        </span>
                      ) : null}
                      {candidate?.cnsub ? (
                        <span className="font-medium text-stone-600">中字</span>
                      ) : null}
                      {candidate?.review_status === 'accepted' ? (
                        <span className="font-semibold text-stone-700">
                          {zh('最佳磁链', 'Best magnet')}
                        </span>
                      ) : null}
                      {stage === 'quality_review' && candidateID === reviewAttemptCandidateID ? (
                        <span className="font-semibold text-amber-700">
                          {zh('本次验收', 'Current review')}
                        </span>
                      ) : null}
                      {candidate?.source_created_at ? (
                        <span>{candidate.source_created_at}</span>
                      ) : null}
                    </span>
                    <span
                      className="mt-1 block truncate font-mono text-[10px] text-gray-400"
                      title={candidate?.info_hash}
                    >
                      {candidate?.info_hash}
                    </span>
                  </span>
                </label>
                {checked && !selectionLocked ? (
                  <div
                    className={`border-t px-3 py-3 ${persisted ? 'border-stone-300 bg-stone-100/80' : 'border-orange-200 bg-orange-100/55'}`}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <span
                        className={`text-xs font-semibold ${persisted ? 'text-stone-800' : 'text-orange-900'}`}
                      >
                        {persisted
                          ? zh('已保存 · 尚未发送', 'Saved · Not submitted')
                          : zh('当前选择 · 尚未保存', 'Selected · Not saved')}
                      </span>
                      <div className="flex flex-wrap items-center justify-end gap-2">
                        <button
                          type="button"
                          onClick={submitSingle}
                          disabled={submitting || savingSelection}
                          className={`min-h-8 rounded-md px-3 py-1.5 text-xs font-medium disabled:opacity-50 ${persisted ? 'bg-stone-800 text-white hover:bg-stone-900' : 'border border-orange-300 bg-white text-orange-800 hover:bg-orange-50'}`}
                        >
                          {submitting
                            ? zh('发送中…', 'Submitting…')
                            : persisted
                              ? zh('发送此作品', 'Submit this work')
                              : zh('保存并发送', 'Save and submit')}
                        </button>
                        {!persisted ? (
                          <button
                            type="button"
                            onClick={saveSelection}
                            disabled={savingSelection || submitting}
                            className="min-h-8 rounded-md bg-stone-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-stone-900 disabled:opacity-50"
                          >
                            {savingSelection
                              ? zh('保存中…', 'Saving…')
                              : zh('保存选择', 'Save selection')}
                          </button>
                        ) : null}
                      </div>
                    </div>
                    {selectionMessage ? (
                      <div
                        role="status"
                        className="mt-2 rounded-md border border-stone-300 bg-white/75 px-3 py-2 text-xs font-medium text-stone-700"
                      >
                        {selectionMessage}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      ) : null}
      {detail?.download_attempt?.status === 'uncertain' ? (
        <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
          {zh(
            '上次发送的响应丢失，云端是否已接单暂时未知。候选已锁定；可以重试，系统会复用同一个幂等键，避免重复任务。',
            'The last submission response was lost, so cloud acceptance is unknown. The candidate is locked; retrying reuses the same idempotency key to avoid a duplicate task.'
          )}
        </div>
      ) : null}
      {stage === 'quality_review' && selectedCandidate ? (
        <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50/70 p-3">
          <div className="text-xs font-semibold text-amber-900">
            {zh(
              '文件位于 115 待验收区。这里先记录决定，不移动或删除文件；可在“待验收”入口统一执行。',
              'The file is staged in 115. This step only records your decision; the quality queue performs the move or delete in a batch.'
            )}
          </div>
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            {magnetReviewFacts.map((fact) => (
              <label key={fact.key} className="flex items-center gap-2 text-xs text-amber-950">
                <span className="w-24 shrink-0">{fact.label}</span>
                <select
                  value={reviewFactValue(reviewFacts[fact.key])}
                  onChange={(event) =>
                    setReviewFacts((current) => ({
                      ...current,
                      [fact.key]: parseReviewFact(event.target.value),
                    }))
                  }
                  className="min-h-8 flex-1 rounded border border-amber-200 bg-white px-2 text-xs text-gray-800 outline-none focus:border-amber-400"
                >
                  <option value="">{zh('未确认', 'Unknown')}</option>
                  <option value="yes">
                    {fact.key === 'hasIntroAd' ||
                    fact.key === 'hasWatermark' ||
                    fact.key === 'hasMarquee'
                      ? zh('有', 'Yes')
                      : zh('是', 'Yes')}
                  </option>
                  <option value="no">
                    {fact.key === 'hasIntroAd' ||
                    fact.key === 'hasWatermark' ||
                    fact.key === 'hasMarquee'
                      ? zh('无', 'No')
                      : zh('否', 'No')}
                  </option>
                </select>
              </label>
            ))}
          </div>
          <div className="mt-3">
            <div className="text-[11px] font-medium text-amber-900">
              {zh('不合格原因（可多选）', 'Rejection reasons (multiple)')}
            </div>
            <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1.5">
              {magnetRejectionReasons.map((reason) => {
                const checked = selectedReasons.includes(reason.value)
                return (
                  <label
                    key={reason.value}
                    className="inline-flex items-center gap-1.5 text-xs text-amber-950"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() =>
                        setSelectedReasons((current) =>
                          checked
                            ? current.filter((value) => value !== reason.value)
                            : [...current, reason.value]
                        )
                      }
                      className="h-3.5 w-3.5 accent-amber-600"
                    />
                    {reason.label}
                  </label>
                )
              })}
            </div>
          </div>
          <textarea
            value={reviewReason}
            onChange={(event) => setReviewReason(event.target.value)}
            rows={2}
            placeholder={zh(
              '不合格时填写补充原因，例如：有水印、片头广告…',
              'Add a rejection detail, for example: watermark or intro ad...'
            )}
            className="mt-2 w-full resize-y rounded border border-amber-200 bg-white px-2.5 py-2 text-xs text-gray-800 outline-none focus:border-amber-400"
          />
          <div className="mt-2 flex flex-wrap justify-end gap-2">
            <button
              type="button"
              onClick={() => saveReviewDecision(false)}
              disabled={reviewing}
              className="rounded-md border border-rose-200 bg-white px-3 py-1.5 text-xs font-medium text-rose-700 hover:bg-rose-50 disabled:opacity-50"
            >
              {reviewing
                ? zh('保存中…', 'Saving…')
                : detail?.download_attempt?.review_decision === 'rejected'
                  ? zh('已记录不合格', 'Rejection saved')
                  : zh('记录不合格', 'Record rejection')}
            </button>
            <button
              type="button"
              onClick={() => saveReviewDecision(true)}
              disabled={reviewing}
              className="rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
            >
              {reviewing
                ? zh('保存中…', 'Saving…')
                : detail?.download_attempt?.review_decision === 'accepted'
                  ? zh('已记录通过', 'Approval saved')
                  : zh('记录通过', 'Record approval')}
            </button>
          </div>
        </div>
      ) : null}
      {stage === 'awaiting_scan' ? (
        <div className="mt-4 rounded-lg border border-stone-300 bg-stone-50 px-3 py-2.5 text-xs leading-5 text-stone-700">
          <div className="font-semibold text-stone-900">
            {zh('质量已通过，等待扫盘确认', 'Quality approved; awaiting scan')}
          </div>
          <div>
            {zh(
              '文件已经移入正式作品库。JavBoss 扫到真实文件并关联本作品后，会自动标记为正式入库。',
              'The file is in the formal library. JavBoss will mark it imported after the scanner links the real file to this work.'
            )}
          </div>
        </div>
      ) : null}
      {rejectedCandidates.length > 0 ? (
        <details className="mt-4 rounded-lg border border-rose-200 bg-rose-50/50">
          <summary className="cursor-pointer px-3 py-2 text-xs font-semibold text-rose-800">
            {zh(
              `不合格磁链（${rejectedCandidates.length}）`,
              `Rejected magnets (${rejectedCandidates.length})`
            )}
          </summary>
          <div className="space-y-2 border-t border-rose-200 px-3 py-3">
            {rejectedCandidates.map((candidate) => (
              <div
                key={candidate.id}
                className="rounded border border-rose-100 bg-white px-3 py-2 text-xs text-gray-600"
              >
                <div className="font-medium text-gray-800">
                  {candidate.name || candidate.info_hash}
                </div>
                <div className="mt-1">{formatRejectionReasons(candidate.review_reasons)}</div>
                {candidate.review_notes ? (
                  <div className="mt-1 text-gray-500">{candidate.review_notes}</div>
                ) : null}
              </div>
            ))}
          </div>
        </details>
      ) : null}
      {message ? (
        <div
          role="status"
          className="mt-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
        >
          {message}
        </div>
      ) : null}
      {error ? (
        <div
          role="alert"
          className="mt-3 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700"
        >
          {error}
        </div>
      ) : null}
    </section>
  )
}

function JavFavoriteRatingEditor({ value, saving, error, onChange }) {
  const rating = Number(value) || 0
  const [editing, setEditing] = useState(false)
  const [preview, setPreview] = useState(null)
  const tooltipValue = preview ?? rating
  const hasTooltipValue = preview !== null || rating > 0
  const displayCount = Math.ceil(rating)
  const ratingWidth = !editing ? Math.max(displayCount, 1) * 21 : 5 * 21
  const tooltipTitle = error
    ? error
    : preview === 0
      ? zh('清空喜爱度', 'Clear favorite rating')
      : hasTooltipValue
        ? zh(`喜爱度：${tooltipValue.toFixed(1)} 分`, `Favorite rating: ${tooltipValue.toFixed(1)}`)
        : zh('设置喜爱度评分', 'Set favorite rating')

  return (
    <Tooltip title={tooltipTitle} placement="top" arrow>
      <span
        role="group"
        aria-label={zh('喜爱度评分', 'Favorite rating')}
        className={`inline-flex items-center rounded-full bg-gray-100 px-1.5 py-0.5 transition-opacity ${
          saving ? 'opacity-60' : 'opacity-100'
        }`}
        onMouseLeave={() => {
          setEditing(false)
          setPreview(null)
        }}
        onBlur={(event) => {
          if (event.currentTarget.contains(event.relatedTarget)) return
          setEditing(false)
          setPreview(null)
        }}
      >
        <span
          className="flex overflow-hidden transition-[width] duration-150"
          style={{ width: ratingWidth }}
        >
          <Rating
            name="jav-detail-favorite-rating"
            value={rating}
            precision={0.5}
            size="small"
            icon={<FavoriteRoundedIcon fontSize="inherit" />}
            emptyIcon={<FavoriteBorderRoundedIcon fontSize="inherit" />}
            disabled={saving}
            onChange={onChange}
            onMouseEnter={() => setEditing(true)}
            onFocus={() => setEditing(true)}
            onChangeActive={(_, nextValue) => setPreview(nextValue >= 0.5 ? nextValue : null)}
            sx={{
              flexShrink: 0,
              color: '#fbbf24',
              fontSize: 21,
              '& .MuiRating-iconEmpty': { color: '#9ca3af' },
            }}
          />
        </span>
        {editing && rating > 0 ? (
          <button
            type="button"
            className="ml-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-gray-500 transition hover:bg-gray-200 hover:text-gray-800"
            disabled={saving}
            aria-label={zh('清空喜爱度', 'Clear favorite rating')}
            onMouseEnter={() => setPreview(0)}
            onMouseLeave={() => setPreview(null)}
            onClick={(event) => onChange?.(event, 0)}
          >
            <RemoveCircleOutlineRoundedIcon sx={{ fontSize: 16 }} />
          </button>
        ) : null}
        {rating > 0 && !editing ? (
          <span className="ml-1 shrink-0 text-xs font-semibold tabular-nums leading-none text-gray-700">
            {rating.toFixed(1)}
          </span>
        ) : null}
      </span>
    </Tooltip>
  )
}

/* eslint-disable jsx-a11y/no-noninteractive-element-to-interactive-role */
function JavSampleImageGrid({ images, javId, directoryIds = [] }) {
  const [previewItem, setPreviewItem] = useState(null)
  const [failedIndexes, setFailedIndexes] = useState(() => new Set())
  useEffect(() => setFailedIndexes(new Set()), [images, javId])
  const previewItems = useMemo(
    () =>
      images.map((image, index) => ({
        name: zh(`样品图像 ${index + 1}`, `Sample image ${index + 1}`),
        url: javSampleImageURL(javId, index, { variant: 'detail', directoryIds }),
      })),
    [directoryIds, images, javId]
  )

  return (
    <>
      <div className="flex flex-wrap gap-2">
        {images.map((image, index) => {
          const failed = failedIndexes.has(index)
          if (failed) {
            return (
              <div
                key={`${image.detail_url}-${index}`}
                className="flex h-24 w-32 items-center justify-center rounded border border-amber-200 bg-amber-50 px-2 text-center text-[10px] text-amber-700"
              >
                {zh(`图像 ${index + 1} 加载失败`, `Image ${index + 1} failed to load`)}
              </div>
            )
          }
          return (
            <img
              key={`${image.detail_url}-${index}`}
              src={javSampleImageURL(javId, index, { directoryIds })}
              alt={zh(`样品图像 ${index + 1}`, `Sample image ${index + 1}`)}
              onClick={() => setPreviewItem(previewItems[index])}
              onError={() =>
                setFailedIndexes((current) => {
                  const next = new Set(current)
                  next.add(index)
                  return next
                })
              }
              aria-label={zh(
                `放大查看第 ${index + 1} 张样品图像`,
                `Enlarge sample image ${index + 1}`
              )}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  setPreviewItem(previewItems[index])
                }
              }}
              className="h-24 w-auto cursor-pointer object-contain"
              loading="lazy"
              role="button"
              tabIndex={0}
            />
          )
        })}
      </div>
      {previewItem ? (
        <ScreenshotPreviewModal
          item={previewItem}
          items={previewItems}
          onClose={() => setPreviewItem(null)}
          onSelect={setPreviewItem}
        />
      ) : null}
    </>
  )
}
/* eslint-enable jsx-a11y/no-noninteractive-element-to-interactive-role */

function JavScreenshotGrid({ videos, onPlayAtTime, onCoverChanged }) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(false)
  const [failedCount, setFailedCount] = useState(0)
  const [error, setError] = useState('')
  const [deletingKey, setDeletingKey] = useState('')
  const [previewItem, setPreviewItem] = useState(null)
  const videoIdentity = (videos || [])
    .map((video) => `${video?.id || ''}:${video?.updated_at || ''}`)
    .join('|')

  useEffect(() => {
    let cancelled = false
    let refreshInFlight = false
    let initialLoad = true
    const videoById = new Map()
    for (const video of videos || []) {
      const videoId = Number(video?.id)
      if (videoId > 0 && !videoById.has(videoId)) videoById.set(videoId, video)
    }
    setItems([])
    setFailedCount(0)
    setError('')
    setDeletingKey('')
    if (videoById.size === 0) {
      setLoading(false)
      return undefined
    }

    setLoading(true)
    const refreshScreenshots = async () => {
      if (refreshInFlight) return
      refreshInFlight = true
      try {
        const screenshots = await fetchVideoScreenshotsByIds(Array.from(videoById.keys()))
        if (cancelled) return
        setItems((current) => {
          const nextItems = screenshots.flatMap((screenshot) => {
            const video = videoById.get(Number(screenshot?.video_id))
            return video ? [{ ...screenshot, video }] : []
          })
          return screenshotItemsMatch(current, nextItems) ? current : nextItems
        })
        setFailedCount(0)
      } catch {
        if (!cancelled) setFailedCount(1)
      } finally {
        refreshInFlight = false
        if (!cancelled && initialLoad) {
          initialLoad = false
          setLoading(false)
        }
      }
    }

    void refreshScreenshots()
    const refreshTimer = window.setInterval(() => {
      void refreshScreenshots()
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(refreshTimer)
    }
  }, [videoIdentity, videos])

  const handleDeleteScreenshot = async (video, screenshot) => {
    if (!video?.id || !screenshot?.name || deletingKey) return
    const actionKey = screenshotActionKey(video, screenshot)
    setDeletingKey(actionKey)
    setError('')
    try {
      await deleteVideoScreenshot(video.id, screenshot.name)
      setItems((current) =>
        current.filter(
          (candidate) =>
            !(
              Number(candidate?.video?.id) === Number(video.id) &&
              candidate?.name === screenshot.name
            )
        )
      )
      if (screenshot.is_cover) {
        onCoverChanged?.({
          id: video.id,
          cover_screenshot_name: '',
          updated_at: new Date().toISOString(),
        })
      }
    } catch (err) {
      setError(getErrorMessage(err))
    } finally {
      setDeletingKey('')
    }
  }

  if (loading) {
    return (
      <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed border-gray-200 text-xs text-gray-500">
        {zh('正在加载视频截图…', 'Loading video screenshots...')}
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed border-gray-200 px-4 text-center text-xs text-gray-500">
        {failedCount > 0 ? getErrorMessage() : zh('暂无视频截图', 'No video screenshots')}
      </div>
    )
  }

  return (
    <>
      {failedCount > 0 ? (
        <div className="mb-2 rounded border border-amber-200 bg-amber-50 px-2.5 py-1.5 text-xs text-amber-700">
          {zh(
            '截图实时刷新失败，正在保留现有结果并继续重试。',
            'Live screenshot refresh failed. Existing results are preserved while retrying.'
          )}
        </div>
      ) : null}
      {error ? (
        <div className="mb-2 rounded border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs text-red-700">
          {error}
        </div>
      ) : null}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
        {items.map((screenshot) => {
          const video = screenshot.video
          const videoName = getVideoDisplayName(video)
          const startTime = screenshotStartTime(screenshot.name)
          const actionKey = screenshotActionKey(video, screenshot)
          return (
            <div
              key={`${video?.id || 'video'}-${screenshot?.name || screenshot?.url}`}
              className="group overflow-hidden rounded-md border border-gray-200 bg-white text-left"
            >
              <div
                className="relative aspect-video cursor-pointer overflow-hidden bg-gray-100"
                role="button"
                tabIndex={0}
                onClick={() => setPreviewItem(screenshot)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    setPreviewItem(screenshot)
                  }
                }}
                aria-label={zh(
                  `放大查看 ${videoName} 的截图`,
                  `Enlarge screenshot for ${videoName}`
                )}
              >
                <img
                  src={screenshot.url}
                  alt={screenshot.name}
                  className="h-full w-full object-contain"
                  loading="lazy"
                />
                {screenshot.is_cover ? (
                  <span className="absolute left-1.5 top-1.5 rounded bg-emerald-600/90 px-1.5 py-0.5 text-[10px] font-medium text-white">
                    {zh('当前封面', 'Current cover')}
                  </span>
                ) : null}
                <Tooltip title={zh('删除截图', 'Delete screenshot')}>
                  <IconButton
                    size="small"
                    onClick={(event) => {
                      event.stopPropagation()
                      void handleDeleteScreenshot(video, screenshot)
                    }}
                    disabled={Boolean(deletingKey)}
                    aria-label={zh('删除截图', 'Delete screenshot')}
                    className="!absolute !right-1.5 !top-1.5 !z-10 !bg-white/90 !text-red-600 !opacity-0 hover:!bg-white disabled:!opacity-50 group-hover:!opacity-100"
                  >
                    <DeleteOutlineIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <div className="absolute inset-0 flex items-center justify-center bg-transparent opacity-0 transition-opacity group-hover:opacity-100">
                  <Tooltip title={zh('从此处播放', 'Play from here')}>
                    <span>
                      <IconButton
                        onClick={(event) => {
                          event.stopPropagation()
                          onPlayAtTime?.(video, startTime)
                        }}
                        disabled={startTime == null}
                        aria-label={zh('从此处播放', 'Play from here')}
                        className="!h-10 !w-10 !bg-white/90 !text-gray-900 hover:!bg-white disabled:!opacity-50"
                      >
                        <PlayArrowIcon fontSize="small" />
                      </IconButton>
                    </span>
                  </Tooltip>
                </div>
              </div>
              <div className="px-2 py-1.5">
                <div className="truncate text-[11px] font-medium text-gray-800" title={videoName}>
                  {videoName}
                </div>
                <div className="text-[10px] text-gray-500">
                  {formatScreenshotTime(screenshot.name)}
                </div>
              </div>
              {deletingKey === actionKey ? (
                <div className="h-0.5 animate-pulse bg-blue-500" />
              ) : null}
            </div>
          )
        })}
      </div>
      {previewItem ? (
        <ScreenshotPreviewModal
          item={previewItem}
          items={items}
          onClose={() => setPreviewItem(null)}
          onSelect={setPreviewItem}
        />
      ) : null}
    </>
  )
}

export default function JavDetailModal({
  item,
  cover,
  title,
  releaseText,
  durationText,
  studio,
  series,
  tags,
  externalLinks,
  preferChineseName,
  canPlay,
  onClose,
  onPlay,
  onOpenFavorites,
  onEdit,
  favoriteRating,
  favoriteRatingSaving,
  favoriteRatingError,
  onFavoriteRatingChange,
  onSelectStudio,
  onSelectSeries,
  onSelectIdol,
  onSelectPrefix,
  loadIdolPreview,
  loadStudioPreview,
  loadSeriesPreview,
  buildIdolUrl,
  buildStudioUrl,
  buildSeriesUrl,
  buildTagUrl,
  directoryIds,
  onOpenIdolFavorites,
  onOpenStudioFavorites,
  onOpenSeriesFavorites,
  onOpenIdolCoverEditor,
  onOpenIdolEditor,
  onVideoPlay,
  onVideoPlayAtTime,
  onVideoCoverChanged,
  onVideoOpenFile,
  onVideoRevealFile,
  openFileLabel,
  onVideoOpenTagPicker,
  onVideoOpenScreenshots,
  onVideoOpenScrapeSettings,
  onVideoRename,
  onVideoDelete,
  onVideoTagClick,
  onAcquisitionUpdated,
}) {
  const titleId = `jav-detail-title-${item?.id || 'item'}`
  const itemId = item?.id
  const itemSampleImages = item?.sample_images
  const code = String(item?.code || '').trim()
  const idols = useMemo(() => (Array.isArray(item?.idols) ? item.idols : []), [item?.idols])
  const videos = useMemo(() => (Array.isArray(item?.videos) ? item.videos : []), [item?.videos])
  const studioName = String(studio?.name || '').trim()
  const seriesName = String(series?.name || '').trim()
  const favoriteCount = Number(item?.favorite_count) || 0
  const emptyVideoSelection = useMemo(() => new Set(), [])
  const { coverAspectPercent } = useMemo(() => getIdolCardLayoutProps(), [])
  const [hoverPreview, setHoverPreview] = useState(null)
  const [sampleImages, setSampleImages] = useState(() => normalizeSampleImages(itemSampleImages))
  const [sampleImagesMissing, setSampleImagesMissing] = useState(() =>
    sampleImagesNotFound(itemSampleImages)
  )
  const [sampleImagesLoading, setSampleImagesLoading] = useState(false)
  const [sampleImagesError, setSampleImagesError] = useState('')
  const [trailer, setTrailer] = useState(null)
  const [trailerLoading, setTrailerLoading] = useState(false)
  const [trailerError, setTrailerError] = useState('')
  const [trailerOpen, setTrailerOpen] = useState(false)
  const hoverCloseTimerRef = useRef(null)
  const activeHoverKeyRef = useRef('')
  const hoverPreviewLockedRef = useRef(false)
  const directoryIdentity = (directoryIds || []).join(',')

  useEffect(() => {
    return () => {
      if (hoverCloseTimerRef.current) window.clearTimeout(hoverCloseTimerRef.current)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const initialImages = normalizeSampleImages(itemSampleImages)
    setSampleImages(initialImages)
    setSampleImagesMissing(sampleImagesNotFound(itemSampleImages))
    setSampleImagesError('')
    if (!itemId) {
      setSampleImagesLoading(false)
      return undefined
    }
    if (initialImages.length > 0) {
      setSampleImagesLoading(false)
      return undefined
    }
    if (sampleImagesNotFound(itemSampleImages)) {
      setSampleImagesLoading(false)
      return undefined
    }

    const requestOptions = {
      directoryIds: directoryIdentity ? directoryIdentity.split(',') : [],
    }
    const resolvedImages = getResolvedJavSampleImages(itemId, requestOptions)
    if (resolvedImages) {
      setSampleImages(normalizeSampleImages(resolvedImages))
      setSampleImagesMissing(sampleImagesNotFound(resolvedImages))
      setSampleImagesLoading(false)
      return undefined
    }

    setSampleImagesLoading(true)
    void resolveJavSampleImages(itemId, requestOptions)
      .then((images) => {
        if (!cancelled) {
          setSampleImages(normalizeSampleImages(images))
          setSampleImagesMissing(sampleImagesNotFound(images))
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setSampleImagesMissing(false)
          setSampleImagesError(getErrorMessage(error))
        }
      })
      .finally(() => {
        if (!cancelled) setSampleImagesLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [code, directoryIdentity, itemId, itemSampleImages])

  useEffect(() => {
    let cancelled = false
    setTrailer(null)
    setTrailerError('')
    setTrailerOpen(false)
    if (!itemId) {
      setTrailerLoading(false)
      return undefined
    }

    setTrailerLoading(true)
    void fetchJavTrailer(itemId, {
      directoryIds: directoryIdentity ? directoryIdentity.split(',') : [],
    })
      .then((payload) => {
        if (!cancelled) setTrailer(payload)
      })
      .catch((error) => {
        if (!cancelled) setTrailerError(getErrorMessage(error))
      })
      .finally(() => {
        if (!cancelled) setTrailerLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [code, directoryIdentity, itemId])

  const clearHoverCloseTimer = () => {
    if (!hoverCloseTimerRef.current) return
    window.clearTimeout(hoverCloseTimerRef.current)
    hoverCloseTimerRef.current = null
  }

  const closeHoverPreview = () => {
    activeHoverKeyRef.current = ''
    hoverPreviewLockedRef.current = false
    setHoverPreview(null)
  }

  const scheduleHoverClose = () => {
    clearHoverCloseTimer()
    if (hoverPreviewLockedRef.current) return
    hoverCloseTimerRef.current = window.setTimeout(() => {
      closeHoverPreview()
      hoverCloseTimerRef.current = null
    }, 120)
  }

  const handleHoverStart = (type, previewItem, event) => {
    clearHoverCloseTimer()
    const identity = previewItem?.id || previewItem?.name || ''
    const previewKey = `${type}:${identity}`
    activeHoverKeyRef.current = previewKey
    setHoverPreview({ type, item: previewItem, anchorEl: event.currentTarget })

    const loader =
      type === 'idol' ? loadIdolPreview : type === 'studio' ? loadStudioPreview : loadSeriesPreview
    if (!loader) return
    void loader(previewItem)
      .then((loadedItem) => {
        if (!loadedItem || activeHoverKeyRef.current !== previewKey) return
        setHoverPreview((current) =>
          current?.type === type
            ? { ...current, item: { ...current.item, ...loadedItem } }
            : current
        )
      })
      .catch((error) => {
        console.warn(`load ${type} preview failed`, error)
      })
  }

  const handleStudioSeriesListOpenChange = (open) => {
    clearHoverCloseTimer()
    hoverPreviewLockedRef.current = Boolean(open)
    if (!open) scheduleHoverClose()
  }

  const trailerURL = String(trailer?.url || '').trim()
  const detailRows = [
    {
      label: zh('识别码', 'Code'),
      content: (
        <div className="flex flex-wrap items-center gap-2">
          <span>{code || zh('未知', 'Unknown')}</span>
          {typeof item?.is_uncensored === 'boolean' ? (
            <span
              className={`rounded px-2 py-0.5 text-xs font-medium ${
                item.is_uncensored ? 'bg-rose-100 text-rose-700' : 'bg-sky-100 text-sky-700'
              }`}
            >
              {item.is_uncensored ? zh('无码', 'Uncensored') : zh('有码', 'Censored')}
            </span>
          ) : null}
        </div>
      ),
    },
    { label: zh('发行日期', 'Release date'), content: releaseText },
    { label: zh('时长', 'Runtime'), content: durationText || zh('未知', 'Unknown') },
    {
      label: zh('片商', 'Studio'),
      content: studioName ? (
        <a
          href={buildStudioUrl?.(studio) || '#'}
          className="text-left font-medium text-blue-700 hover:underline"
          onMouseEnter={(event) => handleHoverStart('studio', studio, event)}
          onMouseLeave={scheduleHoverClose}
        >
          {studioName}
        </a>
      ) : (
        zh('未知', 'Unknown')
      ),
    },
    {
      label: zh('系列', 'Series'),
      content: seriesName ? (
        <a
          href={buildSeriesUrl?.(series) || '#'}
          className="text-left font-medium text-blue-700 hover:underline"
          onMouseEnter={(event) => handleHoverStart('series', series, event)}
          onMouseLeave={scheduleHoverClose}
        >
          {seriesName}
        </a>
      ) : (
        zh('未知', 'Unknown')
      ),
    },
  ]

  return (
    <AppModal
      ariaLabelledby={titleId}
      backdropBlur="2px"
      backdropColor="rgba(2, 6, 23, 0.7)"
      className="p-3 sm:p-6"
      contentClassName="flex max-h-[92vh] w-full max-w-[90rem] flex-col overflow-hidden rounded-xl bg-white shadow-2xl"
      onClose={onClose}
      zIndex={1300}
    >
      <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-1.5 sm:px-5">
        <div className="min-w-0">
          <h2 id={titleId} className="truncate text-sm font-semibold text-gray-900 sm:text-base">
            {title}
          </h2>
        </div>
        <button
          type="button"
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-gray-500 transition hover:bg-gray-100 hover:text-gray-900"
          onClick={onClose}
          aria-label={zh('关闭', 'Close')}
        >
          <CloseOutlinedIcon sx={{ fontSize: 16 }} />
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
        <div className="grid items-stretch gap-6 lg:grid-cols-[minmax(0,3fr)_minmax(19rem,2fr)]">
          <div className="group relative aspect-[800/538] w-full overflow-hidden rounded-lg border border-gray-200 bg-gray-50 shadow-sm">
            {cover ? (
              <img
                src={cover}
                alt={code || zh('JAV 封面', 'JAV cover')}
                className="h-full w-full object-contain object-top"
              />
            ) : (
              <span className="flex h-full items-center justify-center bg-gradient-to-br from-gray-100 to-gray-200 text-lg font-semibold text-gray-500">
                {code || zh('暂无封面', 'No cover')}
              </span>
            )}
            {canPlay ? (
              <button
                type="button"
                className="absolute left-1/2 top-1/2 z-[1] flex h-20 w-20 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-black/65 text-white opacity-0 shadow-lg transition hover:bg-black/80 focus-visible:opacity-100 group-hover:opacity-100"
                onClick={onPlay}
                aria-label={zh('播放', 'Play')}
              >
                <PlayArrowIcon sx={{ fontSize: 54 }} />
              </button>
            ) : null}
          </div>

          <div className="flex min-w-0 flex-col gap-5">
            <dl className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              {detailRows.map((row, index) => (
                <div
                  key={row.label}
                  className={`grid grid-cols-[5.5rem_minmax(0,1fr)] gap-3 px-4 py-2.5 text-sm ${
                    index > 0 ? 'border-t border-gray-100' : ''
                  }`}
                >
                  <dt className="font-medium text-gray-500">{row.label}</dt>
                  <dd className="min-w-0 break-words text-gray-800">{row.content}</dd>
                </div>
              ))}
            </dl>

            {idols.length > 0 ? (
              <section aria-label={zh('女优', 'Actresses')}>
                <h3 className="mb-2 text-sm font-semibold text-gray-800">
                  {zh('女优', 'Actresses')}
                </h3>
                <div className="flex flex-wrap gap-2">
                  {idols.map((idol) => (
                    <a
                      key={idol?.id || idol?.name}
                      href={buildIdolUrl?.(idol) || '#'}
                      className="rounded-full border border-purple-200 bg-purple-50 px-3 py-1 text-xs font-medium text-purple-700 transition hover:border-purple-300 hover:bg-purple-100"
                      onMouseEnter={(event) => handleHoverStart('idol', idol, event)}
                      onMouseLeave={scheduleHoverClose}
                    >
                      {getIdolDisplayName(idol, preferChineseName)}
                    </a>
                  ))}
                </div>
              </section>
            ) : null}

            {tags.length > 0 ? (
              <section aria-label={zh('标签', 'Tags')}>
                <h3 className="mb-2 text-sm font-semibold text-gray-800">{zh('标签', 'Tags')}</h3>
                <div className="flex flex-wrap gap-2">
                  {tags.map((tag) => {
                    const isUser = isUserJavTag(tag)
                    return (
                      <a
                        key={`${tag?.id || tag?.name}-${tag?.provider || 0}`}
                        href={buildTagUrl?.(tag) || '#'}
                        className={`rounded px-2.5 py-1 text-xs font-medium transition ${
                          isUser
                            ? 'bg-emerald-100 text-emerald-700 hover:bg-emerald-200'
                            : 'bg-orange-100 text-orange-700 hover:bg-orange-200'
                        }`}
                      >
                        {tag?.name}
                      </a>
                    )
                  })}
                </div>
              </section>
            ) : null}

            {externalLinks.length > 0 ? (
              <section aria-label={zh('外部链接', 'External links')}>
                <h3 className="mb-2 text-sm font-semibold text-gray-800">
                  {zh('外部链接', 'External links')}
                </h3>
                <div className="flex flex-wrap gap-2">
                  {externalLinks.map((site) => (
                    <a
                      key={site.key}
                      href={site.href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 rounded border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-700 transition hover:border-gray-300 hover:bg-gray-50"
                      onClick={site.onClick}
                    >
                      <img
                        src={site.icon}
                        alt=""
                        className={`h-4 w-4 ${site.loading ? 'animate-pulse' : ''}`}
                        loading="lazy"
                      />
                      <span>{site.name}</span>
                    </a>
                  ))}
                </div>
              </section>
            ) : null}

            <section className="mt-auto pt-1" aria-label={zh('操作', 'Actions')}>
              <h3 className="mb-2 text-sm font-semibold text-gray-800">
                {zh('操作栏', 'Actions')}
              </h3>
              <div className="group flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 transition hover:border-gray-400 hover:bg-gray-50"
                  onClick={onOpenFavorites}
                >
                  {favoriteCount > 0 ? (
                    <StarRoundedIcon className="text-amber-500" sx={{ fontSize: 16 }} />
                  ) : (
                    <StarBorderRoundedIcon sx={{ fontSize: 16 }} />
                  )}
                  {zh('收藏', 'Favorite')}
                </button>
                <button
                  type="button"
                  className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 transition hover:border-gray-400 hover:bg-gray-50"
                  onClick={onEdit}
                >
                  <MovieEdit sx={{ fontSize: 16 }} />
                  {zh('编辑', 'Edit')}
                </button>
                <JavFavoriteRatingEditor
                  value={favoriteRating}
                  saving={favoriteRatingSaving}
                  error={favoriteRatingError}
                  onChange={onFavoriteRatingChange}
                />
                {trailerLoading ? (
                  <button
                    type="button"
                    className="inline-flex cursor-wait items-center gap-1.5 rounded-md border border-indigo-100 bg-indigo-50 px-3 py-1.5 text-xs font-medium text-indigo-400"
                    disabled
                  >
                    <PlayArrowIcon sx={{ fontSize: 16 }} />
                    {zh('查找预告…', 'Finding trailer…')}
                  </button>
                ) : trailerURL ? (
                  <button
                    type="button"
                    className="inline-flex items-center gap-1.5 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-xs font-medium text-indigo-700 transition hover:border-indigo-300 hover:bg-indigo-100"
                    onClick={() => setTrailerOpen(true)}
                    title={zh(
                      `播放预告 · 来源：${trailer?.source || 'unknown'}`,
                      `Play trailer · Source: ${trailer?.source || 'unknown'}`
                    )}
                  >
                    <PlayArrowIcon sx={{ fontSize: 16 }} />
                    {zh('播放预告', 'Play trailer')}
                  </button>
                ) : trailerError ? (
                  <span className="text-xs text-amber-600" title={trailerError}>
                    {zh('预告暂不可用', 'Trailer unavailable')}
                  </span>
                ) : null}
              </div>
            </section>
          </div>
        </div>

        <div className="mt-7 space-y-7">
          <section className="border-t border-gray-200 pt-5" aria-labelledby={`${titleId}-videos`}>
            <div className="mb-3">
              <h3 id={`${titleId}-videos`} className="text-base font-semibold text-gray-900">
                {zh('关联视频', 'Related videos')}
              </h3>
            </div>
            {videos.length > 0 ? (
              <VideoGrid
                videos={videos}
                selectedIds={emptyVideoSelection}
                onToggleSelect={() => {}}
                showSelection={false}
                onPlay={onVideoPlay}
                onOpenFile={onVideoOpenFile}
                onRevealFile={onVideoRevealFile}
                openFileLabel={openFileLabel}
                onOpenTagPicker={onVideoOpenTagPicker}
                showTagEditor
                onOpenScreenshots={onVideoOpenScreenshots}
                onOpenScrapeSettings={onVideoOpenScrapeSettings}
                onRenameVideo={onVideoRename}
                onDeleteVideo={onVideoDelete}
                onTagClick={onVideoTagClick}
              />
            ) : (
              <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed border-gray-200 text-xs text-gray-500">
                {zh('暂无关联视频', 'No related videos')}
              </div>
            )}
          </section>

          {sampleImagesLoading ||
          sampleImagesError ||
          sampleImagesMissing ||
          sampleImages.length > 0 ? (
            <section
              className="border-t border-gray-200 pt-5"
              aria-labelledby={`${titleId}-sample-images`}
            >
              <h3
                id={`${titleId}-sample-images`}
                className="mb-3 text-base font-semibold text-gray-900"
              >
                {zh('样品图像', 'Sample images')}
              </h3>
              {sampleImagesLoading ? (
                <div className="flex min-h-28 items-center justify-center rounded-lg border border-dashed border-gray-200 text-xs text-gray-500">
                  {zh('正在加载样品图像…', 'Loading sample images...')}
                </div>
              ) : sampleImages.length > 0 ? (
                <JavSampleImageGrid
                  images={sampleImages}
                  javId={itemId}
                  directoryIds={directoryIds}
                />
              ) : (
                <div className="rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700">
                  {sampleImagesError ||
                    zh(
                      '资料来源确认暂无样品图像',
                      'The providers confirmed that no sample images are available'
                    )}
                </div>
              )}
            </section>
          ) : null}

          <JavMagnetSection
            item={item}
            directoryIds={directoryIds}
            onAcquisitionUpdated={onAcquisitionUpdated}
          />

          <section
            className="border-t border-gray-200 pt-5"
            aria-labelledby={`${titleId}-screenshots`}
          >
            <h3
              id={`${titleId}-screenshots`}
              className="mb-3 text-base font-semibold text-gray-900"
            >
              {zh('视频截图', 'Video screenshots')}
            </h3>
            <JavScreenshotGrid
              videos={videos}
              onPlayAtTime={onVideoPlayAtTime}
              onCoverChanged={onVideoCoverChanged}
            />
          </section>
        </div>
      </div>

      <Popper
        open={Boolean(hoverPreview?.item && hoverPreview?.anchorEl)}
        anchorEl={hoverPreview?.anchorEl || null}
        placement="right-start"
        className="z-[1550]"
        modifiers={[{ name: 'offset', options: { offset: [10, 0] } }]}
      >
        <div
          className={
            hoverPreview?.type === 'studio'
              ? 'w-[320px]'
              : hoverPreview?.type === 'series'
                ? 'w-[260px]'
                : 'w-[220px]'
          }
          onMouseEnter={clearHoverCloseTimer}
          onMouseLeave={scheduleHoverClose}
        >
          {hoverPreview?.type === 'studio' ? (
            <StudioCard
              item={hoverPreview.item}
              href={buildStudioUrl?.(hoverPreview.item)}
              onSelectStudio={onSelectStudio}
              onSelectSeries={onSelectSeries}
              onSelectPrefix={onSelectPrefix}
              onOpenFavorites={onOpenStudioFavorites}
              buildSeriesUrl={buildSeriesUrl}
              onOpenSeriesFavorites={onOpenSeriesFavorites}
              onSeriesListOpenChange={handleStudioSeriesListOpenChange}
              directoryIds={directoryIds}
              seriesListModalZIndex={1600}
            />
          ) : null}
          {hoverPreview?.type === 'series' ? (
            <SeriesCard
              item={hoverPreview.item}
              href={buildSeriesUrl?.(hoverPreview.item)}
              onSelectSeries={onSelectSeries}
              onSelectStudio={onSelectStudio}
              onOpenFavorites={onOpenSeriesFavorites}
            />
          ) : null}
          {hoverPreview?.type === 'idol' ? (
            <IdolCard
              item={hoverPreview.item}
              onSelectIdol={onSelectIdol}
              onOpenFavorites={onOpenIdolFavorites}
              onOpenCoverEditor={onOpenIdolCoverEditor}
              onOpenEditor={onOpenIdolEditor}
              href={buildIdolUrl?.(hoverPreview.item)}
              coverAspectPercent={coverAspectPercent}
              showWorkCount={
                typeof hoverPreview.item?.work_count === 'number' &&
                hoverPreview.item.work_count > 0
              }
              preferChineseName={preferChineseName}
            />
          ) : null}
        </div>
      </Popper>
      {trailerOpen && trailerURL ? (
        <JavTrailerModal title={title} url={trailerURL} onClose={() => setTrailerOpen(false)} />
      ) : null}
    </AppModal>
  )
}
