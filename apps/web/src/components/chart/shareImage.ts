/**
 * Turning the chart SVG into a PNG, in the browser.
 *
 * ── Why the client and not the PDF worker ──
 *
 * The worker already renders a document, so an image endpoint would be a
 * small change there. It would also mean a round trip, a queue, a poll
 * and a signed URL for something the browser already has on screen as
 * vector graphics. Sharing a chart to WhatsApp should be instant.
 *
 * ── The one hard part ──
 *
 * An `<img>` given an SVG data URL renders it in a SEPARATE document
 * that inherits none of the page's CSS. Every colour in `ChartSVG` comes
 * from a Tailwind class, so the naive version draws a black-on-black
 * square: technically a PNG, visibly nothing.
 *
 * So the styles are resolved to literal attributes first, by reading the
 * computed style of each element and writing it onto the clone. That is
 * the whole trick, and it is why this cannot be a three-line function.
 */

/** A4-ish at a density that survives being forwarded and re-compressed. */
const EXPORT_SIZE = 1200

/**
 * Properties that carry the chart's appearance.
 *
 * An allowlist rather than copying the whole computed style. The full
 * set is ~340 properties per element; inlining all of them on a few
 * hundred nodes produces a multi-megabyte SVG string that is slow to
 * serialise and slower to rasterise, for no visual difference.
 */
const VISUAL_PROPERTIES = [
  'fill',
  'stroke',
  'stroke-width',
  'stroke-linecap',
  'stroke-linejoin',
  'opacity',
  'fill-opacity',
  'stroke-opacity',
  'font-family',
  'font-size',
  'font-weight',
  'font-style',
  'text-anchor',
  'dominant-baseline',
  'letter-spacing',
] as const

export class ShareImageError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ShareImageError'
  }
}

/**
 * Renders an on-page `<svg>` to a PNG blob.
 *
 * `background` is painted first, because a transparent PNG shared into a
 * dark-themed chat app is a chart nobody can read — the glyphs are near
 * white.
 */
export async function svgToPng(
  source: SVGSVGElement,
  background: string,
  size: number = EXPORT_SIZE,
): Promise<Blob> {
  const clone = source.cloneNode(true) as SVGSVGElement
  inlineComputedStyles(source, clone)

  // An explicit viewBox, because the clone has no layout to derive one
  // from and a missing viewBox makes the raster silently empty.
  if (!clone.getAttribute('viewBox')) {
    const box = source.viewBox.baseVal
    clone.setAttribute('viewBox', `${box.x} ${box.y} ${box.width} ${box.height}`)
  }
  clone.setAttribute('width', String(size))
  clone.setAttribute('height', String(size))
  clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg')

  const markup = new XMLSerializer().serializeToString(clone)

  /*
    A blob URL, not a data: URL.

    Both work for small charts. A data URL is base64, so it is a third
    larger than the bytes it carries, and some browsers cap the length of
    a URL an <img> will accept — which fails as a blank image rather than
    as an error, on exactly the large charts where it matters.
  */
  const svgBlob = new Blob([markup], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(svgBlob)

  try {
    const image = await loadImage(url)

    const canvas = document.createElement('canvas')
    canvas.width = size
    canvas.height = size

    const ctx = canvas.getContext('2d')
    if (!ctx) throw new ShareImageError('this browser gave us no 2d canvas context')

    ctx.fillStyle = background
    ctx.fillRect(0, 0, size, size)
    ctx.drawImage(image, 0, 0, size, size)

    return await canvasToBlob(canvas)
  } finally {
    // Always, including on the error paths. A leaked object URL keeps
    // the whole SVG alive for the life of the document.
    URL.revokeObjectURL(url)
  }
}

/**
 * Copies computed styles from the live tree onto the clone, node by
 * node.
 *
 * Walks both trees in parallel rather than re-querying, because the
 * clone has no layout and `getComputedStyle` on it returns defaults —
 * the values have to come from the element that is actually on screen.
 */
function inlineComputedStyles(source: Element, clone: Element): void {
  const computed = window.getComputedStyle(source)

  const declarations: string[] = []
  for (const property of VISUAL_PROPERTIES) {
    const value = computed.getPropertyValue(property)
    if (value) declarations.push(`${property}:${value}`)
  }
  if (declarations.length > 0) {
    clone.setAttribute('style', declarations.join(';'))
  }

  /*
    Tailwind classes are dropped from the clone once their effect has
    been inlined.

    Not tidiness: a class attribute naming rules that do not exist in the
    exported document is a promise the file cannot keep, and some SVG
    renderers will happily apply a stale `fill-current` over the style we
    just wrote.
  */
  clone.removeAttribute('class')

  const sourceChildren = source.children
  const cloneChildren = clone.children
  for (let i = 0; i < sourceChildren.length && i < cloneChildren.length; i += 1) {
    inlineComputedStyles(sourceChildren[i]!, cloneChildren[i]!)
  }
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () =>
      reject(new ShareImageError('the browser could not rasterise the chart'))
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob)
      else reject(new ShareImageError('the browser produced no image data'))
    }, 'image/png')
  })
}

/**
 * The filename a shared image lands under.
 *
 * `kundli.png`, with no name, no date and no profile id. A filename is
 * visible in a chat thread, a download list and a file manager — and a
 * birth date in one is the same leak as a birth date in a URL.
 */
export const SHARE_IMAGE_FILENAME = 'kundli.png'
