// Browser-side image pipeline. Mirrors architecture §8:
//   reject > 20 MB raw → HEIC→JPEG (lazy heic2any) → decode via
//   createImageBitmap → resize so max(w,h) ≤ 2000px (downscale only) →
//   re-encode JPEG quality 0.85 → reject if output > 10 MB.
//
// The pipeline is pure: callers pass a File, get back a normalized Blob plus
// the original and processed byte sizes. It never touches the DOM, so it can
// be unit-tested under jsdom with mocked canvas / createImageBitmap.

export const MAX_RAW_BYTES = 20 * 1024 * 1024; // 20 MB decode safety cap
export const MAX_OUT_BYTES = 10 * 1024 * 1024; // 10 MB upload hard cap
export const MAX_DIMENSION = 2000; // longest-side pixel cap
export const JPEG_QUALITY = 0.85;

export class ImagePipelineError extends Error {
  public readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export interface PipelineResult {
  blob: Blob;
  mime: string;
  width: number;
  height: number;
  origSize: number;
  outSize: number;
}

export interface PipelineDeps {
  /** Injectable HEIC converter so the unit test can stub the lazy import. */
  heicConvert?: (blob: Blob) => Promise<Blob>;
  /** Override createImageBitmap for testing. */
  createImageBitmap?: typeof globalThis.createImageBitmap;
  /** Override canvas factory for testing. */
  createCanvas?: (w: number, h: number) => CanvasLike;
}

/* Structural canvas type so we can mock both real OffscreenCanvas and
   HTMLCanvasElement (and a jsdom stub) through one interface. */
export interface CanvasLike {
  width: number;
  height: number;
  getContext(kind: '2d'): CanvasRenderingContext2DLike | null;
  convertToBlob?(opts: { type: string; quality?: number }): Promise<Blob>;
  toBlob?(cb: (blob: Blob | null) => void, type: string, quality?: number): void;
}

export interface CanvasRenderingContext2DLike {
  fillStyle: string;
  fillRect(x: number, y: number, w: number, h: number): void;
  drawImage(image: unknown, dx: number, dy: number, dw: number, dh: number): void;
}

function isHeic(mime: string, name: string): boolean {
  const m = (mime || '').toLowerCase();
  if (m === 'image/heic' || m === 'image/heif') return true;
  // Some browsers do not fill Content-Type for HEIC; fall back to extension.
  const n = name.toLowerCase();
  return n.endsWith('.heic') || n.endsWith('.heif');
}

function defaultCreateCanvas(w: number, h: number): CanvasLike {
  if (typeof OffscreenCanvas !== 'undefined') {
    return new OffscreenCanvas(w, h) as unknown as CanvasLike;
  }
  const c = document.createElement('canvas');
  c.width = w;
  c.height = h;
  return c as unknown as CanvasLike;
}

async function canvasToBlob(canvas: CanvasLike, quality: number): Promise<Blob> {
  if (typeof canvas.convertToBlob === 'function') {
    return canvas.convertToBlob({ type: 'image/jpeg', quality });
  }
  if (typeof canvas.toBlob === 'function') {
    return new Promise<Blob>((resolve, reject) => {
      canvas.toBlob!(
        (b) => (b ? resolve(b) : reject(new ImagePipelineError('ENCODE_FAILED', '图片重编码失败'))),
        'image/jpeg',
        quality,
      );
    });
  }
  throw new ImagePipelineError('ENCODE_FAILED', '浏览器不支持 canvas.toBlob');
}

/**
 * Lazy-load heic2any the first time we actually encounter a HEIC file so the
 * main bundle does not pay for it on every page load.
 */
async function loadHeicConverter(): Promise<(blob: Blob) => Promise<Blob>> {
  const mod = (await import('heic2any')) as unknown as {
    default?: (opts: { blob: Blob; toType: string; quality?: number }) => Promise<Blob | Blob[]>;
  };
  const fn = mod.default;
  if (typeof fn !== 'function') {
    throw new ImagePipelineError('HEIC_CONVERTER_MISSING', '当前浏览器不支持 HEIC 转码');
  }
  return async (blob: Blob) => {
    const out = await fn({ blob, toType: 'image/jpeg', quality: 0.9 });
    return Array.isArray(out) ? out[0] : out;
  };
}

/**
 * Compute the target dimensions so that max(w, h) ≤ MAX_DIMENSION while
 * keeping the aspect ratio and only downscaling (never upscaling).
 */
export function computeTargetSize(srcW: number, srcH: number, maxDim = MAX_DIMENSION): {
  width: number;
  height: number;
} {
  if (srcW <= 0 || srcH <= 0) return { width: srcW, height: srcH };
  const longest = Math.max(srcW, srcH);
  if (longest <= maxDim) return { width: srcW, height: srcH };
  const scale = maxDim / longest;
  return {
    width: Math.max(1, Math.round(srcW * scale)),
    height: Math.max(1, Math.round(srcH * scale)),
  };
}

/**
 * Run the full pipeline on a single File / Blob. Returns the processed JPEG
 * Blob plus bookkeeping for the UI.
 */
export async function processImage(file: File, deps: PipelineDeps = {}): Promise<PipelineResult> {
  const origSize = file.size;
  if (origSize > MAX_RAW_BYTES) {
    throw new ImagePipelineError(
      'RAW_TOO_LARGE',
      '单张原图超过 20 MB，无法处理，请先压缩后再上传',
    );
  }

  let input: Blob = file;
  const name = (file as File).name ?? '';
  if (isHeic(file.type, name)) {
    const convert = deps.heicConvert ?? (await loadHeicConverter());
    try {
      input = await convert(file);
    } catch {
      throw new ImagePipelineError(
        'HEIC_DECODE_FAILED',
        'HEIC 转 JPEG 失败，请换一张图片或改用相机直接拍摄',
      );
    }
  }

  const createBitmap = deps.createImageBitmap ?? globalThis.createImageBitmap;
  if (typeof createBitmap !== 'function') {
    throw new ImagePipelineError('NO_IMAGE_BITMAP', '浏览器不支持 createImageBitmap');
  }

  let bitmap: ImageBitmap;
  try {
    bitmap = await createBitmap(input);
  } catch {
    throw new ImagePipelineError('DECODE_FAILED', '图片解码失败，请重试');
  }

  const { width, height } = computeTargetSize(bitmap.width, bitmap.height);
  const factory = deps.createCanvas ?? defaultCreateCanvas;
  const canvas = factory(width, height);
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new ImagePipelineError('NO_CANVAS_CTX', '无法获取绘图上下文');
  }
  // Paint a white background first so that transparent PNGs flatten cleanly
  // (JPEG has no alpha channel; skipping this produces black fills).
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(bitmap, 0, 0, width, height);

  const out = await canvasToBlob(canvas, JPEG_QUALITY);
  if (out.size > MAX_OUT_BYTES) {
    throw new ImagePipelineError(
      'OUT_TOO_LARGE',
      '压缩后仍超过 10 MB，请降低分辨率或重新拍摄',
    );
  }

  return {
    blob: out,
    mime: 'image/jpeg',
    width,
    height,
    origSize,
    outSize: out.size,
  };
}
