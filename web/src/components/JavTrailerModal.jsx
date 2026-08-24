import { useCallback, useRef } from 'react'
import CloseOutlinedIcon from '@mui/icons-material/CloseOutlined'
import videojs from 'video.js'
import 'video.js/dist/video-js.css'

import AppModal from '@/components/AppModal'
import { zh } from '@/utils/i18n'

export default function JavTrailerModal({ title, url, onClose }) {
  const playerRef = useRef(null)

  const disposePlayer = useCallback(() => {
    if (playerRef.current && !playerRef.current.isDisposed()) playerRef.current.dispose()
    playerRef.current = null
  }, [])

  const attachPlayer = useCallback(
    (container) => {
      disposePlayer()
      if (!container || !url) return

      const videoElement = document.createElement('video-js')
      videoElement.classList.add('vjs-big-play-centered')
      container.appendChild(videoElement)

      playerRef.current = videojs(videoElement, {
        controls: true,
        autoplay: true,
        preload: 'auto',
        fluid: true,
        sources: [
          {
            src: url,
            type: /\.m3u8(?:$|[?#])/i.test(url) ? 'application/x-mpegURL' : 'video/mp4',
          },
        ],
      })
    },
    [disposePlayer, url]
  )

  if (!url) return null
  return (
    <AppModal
      ariaLabel={zh('作品预告', 'Trailer')}
      backdropBlur="2px"
      backdropColor="rgba(2, 6, 23, 0.82)"
      className="p-4 sm:p-8"
      contentClassName="w-full max-w-5xl overflow-hidden rounded-xl bg-slate-950 shadow-2xl"
      onClose={onClose}
      zIndex={1650}
    >
      <div className="flex items-center justify-between gap-3 border-b border-white/10 px-4 py-3 text-white">
        <h3 className="truncate text-sm font-semibold">{title || zh('作品预告', 'Trailer')}</h3>
        <button
          type="button"
          className="flex h-7 w-7 items-center justify-center rounded-full text-white/70 transition hover:bg-white/10 hover:text-white"
          onClick={onClose}
          aria-label={zh('关闭预告', 'Close trailer')}
        >
          <CloseOutlinedIcon sx={{ fontSize: 18 }} />
        </button>
      </div>
      <div data-vjs-player className="aspect-video w-full">
        <div ref={attachPlayer} className="h-full w-full" />
      </div>
    </AppModal>
  )
}
