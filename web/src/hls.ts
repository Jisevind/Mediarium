// Which way to play an HLS playlist in this browser.
//
// Chrome reports "maybe" for application/vnd.apple.mpegurl even though its
// <video> element cannot play an m3u8 itself. MediaSource support is the
// reliable test for hls.js; native HLS is the fallback for iPhone and iPad,
// where MediaSource is not available.
export function hlsMode(mediaSource: boolean, nativeHLS: string): 'hlsjs' | 'native' | 'none' {
  if (mediaSource) return 'hlsjs'
  if (nativeHLS) return 'native'
  return 'none'
}
