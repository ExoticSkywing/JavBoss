import SwapVertIcon from '@mui/icons-material/SwapVert'
import { Popover } from '@mui/material'
import { useCallback, useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import JavGrid from '@/components/JavGrid'
import Pagination from '@/components/Pagination'
import WaterfallLoader from '@/components/WaterfallLoader'
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
              className={`min-h-8 rounded-md px-2.5 text-xs font-semibold transition-[color,background-color,box-shadow,transform] duration-150 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 active:scale-[0.96] ${
                active
                  ? 'bg-white text-indigo-700 shadow-sm'
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
                    className="w-full border-b border-slate-100 px-3 py-2 text-left text-xs font-medium text-blue-700 hover:bg-blue-50"
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
                        active ? 'bg-blue-50 text-blue-700' : 'text-gray-700 hover:bg-gray-50'
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
