import { describe, it, expect, vi } from 'vitest';
import {
  processImage,
  computeTargetSize,
  ImagePipelineError,
  MAX_RAW_BYTES,
  MAX_OUT_BYTES,
  type PipelineDeps,
  type CanvasLike,
  type CanvasRenderingContext2DLike,
} from '@/lib/imagePipeline';

/**
 * Build a dummy File of a given size. Content is zero-filled bytes.
 */
function makeFile(size: number, type = 'image/jpeg', name = 'photo.jpg'): File {
  const buf = new Uint8Array(size);
  return new File([buf], name, { type });
}

/**
 * Tiny mock canvas/context stack. Honors the width/height set by the
 * pipeline so we can assert resize math without a real browser.
 */
function makeMockCanvas(): { canvas: CanvasLike; calls: { fillRects: Array<[number, number, number, number]>; draws: Array<[number, number, number, number]> } } {
  const calls = { fillRects: [] as Array<[number, number, number, number]>, draws: [] as Array<[number, number, number, number]> };
  const ctx: CanvasRenderingContext2DLike = {
    fillStyle: '',
    fillRect(x, y, w, h) {
      calls.fillRects.push([x, y, w, h]);
    },
    drawImage(_img, dx, dy, dw, dh) {
      calls.draws.push([dx, dy, dw, dh]);
    },
  };
  const canvas: CanvasLike = {
    width: 0,
    height: 0,
    getContext: () => ctx,
    async convertToBlob({ type, quality }) {
      // Produce a tiny blob whose size is deterministic from w*h so we can
      // assert the resize actually happened.
      const bytes = new Uint8Array(Math.min(1024, canvas.width * canvas.height));
      return new Blob([bytes], { type });
    },
  };
  return { canvas, calls };
}

describe('computeTargetSize', () => {
  it('does not upscale small images', () => {
    expect(computeTargetSize(800, 600)).toEqual({ width: 800, height: 600 });
  });

  it('scales the longest side to 2000 px while keeping aspect ratio', () => {
    const { width, height } = computeTargetSize(4000, 3000);
    expect(width).toBe(2000);
    expect(height).toBe(1500);
  });

  it('handles portrait orientation', () => {
    const { width, height } = computeTargetSize(3000, 4000);
    expect(width).toBe(1500);
    expect(height).toBe(2000);
  });

  it('handles square', () => {
    expect(computeTargetSize(5000, 5000)).toEqual({ width: 2000, height: 2000 });
  });
});

describe('processImage', () => {
  it('rejects files larger than 20 MB before decoding', async () => {
    const big = makeFile(MAX_RAW_BYTES + 1);
    await expect(processImage(big)).rejects.toBeInstanceOf(ImagePipelineError);
  });

  it('goes through the HEIC branch when MIME is image/heic', async () => {
    const heic = makeFile(1024, 'image/heic', 'x.heic');
    const convert = vi.fn(async (blob: Blob) => new Blob([new Uint8Array(512)], { type: 'image/jpeg' }));
    const createImageBitmap = vi.fn(async () => ({ width: 1000, height: 800, close() {} }) as unknown as ImageBitmap);
    const mock = makeMockCanvas();
    const deps: PipelineDeps = {
      heicConvert: convert,
      createImageBitmap,
      createCanvas: () => mock.canvas,
    };
    const result = await processImage(heic, deps);
    expect(convert).toHaveBeenCalledTimes(1);
    expect(result.mime).toBe('image/jpeg');
    expect(result.width).toBe(1000);
    expect(result.height).toBe(800);
  });

  it('downscales a 4000x3000 image to 2000x1500 and paints a white background', async () => {
    const jpeg = makeFile(5 * 1024 * 1024, 'image/jpeg', 'p.jpg');
    const createImageBitmap = vi.fn(async () => ({ width: 4000, height: 3000, close() {} }) as unknown as ImageBitmap);
    const mock = makeMockCanvas();
    const deps: PipelineDeps = {
      createImageBitmap,
      createCanvas: () => mock.canvas,
    };
    const result = await processImage(jpeg, deps);
    expect(result.width).toBe(2000);
    expect(result.height).toBe(1500);
    // White background paint must cover the full canvas.
    expect(mock.calls.fillRects).toEqual([[0, 0, 2000, 1500]]);
    expect(mock.calls.draws).toEqual([[0, 0, 2000, 1500]]);
  });

  it('does NOT upscale a small image', async () => {
    const jpeg = makeFile(200 * 1024, 'image/jpeg', 'p.jpg');
    const createImageBitmap = vi.fn(async () => ({ width: 600, height: 400, close() {} }) as unknown as ImageBitmap);
    const mock = makeMockCanvas();
    const deps: PipelineDeps = {
      createImageBitmap,
      createCanvas: () => mock.canvas,
    };
    const result = await processImage(jpeg, deps);
    expect(result.width).toBe(600);
    expect(result.height).toBe(400);
  });

  it('rejects when the encoded output exceeds 10 MB', async () => {
    const jpeg = makeFile(200 * 1024, 'image/jpeg', 'p.jpg');
    const createImageBitmap = vi.fn(async () => ({ width: 100, height: 100, close() {} }) as unknown as ImageBitmap);
    const overflowCanvas: CanvasLike = {
      width: 0,
      height: 0,
      getContext: () => ({
        fillStyle: '',
        fillRect() {},
        drawImage() {},
      }),
      async convertToBlob({ type }) {
        const bytes = new Uint8Array(MAX_OUT_BYTES + 1);
        return new Blob([bytes], { type });
      },
    };
    const deps: PipelineDeps = {
      createImageBitmap,
      createCanvas: () => overflowCanvas,
    };
    await expect(processImage(jpeg, deps)).rejects.toMatchObject({
      code: 'OUT_TOO_LARGE',
    });
  });

  it('surfaces a typed error when createImageBitmap is missing', async () => {
    const jpeg = makeFile(1024, 'image/jpeg', 'p.jpg');
    // Ensure the default global is absent for this call. We explicitly
    // leave `createImageBitmap` off the deps so the pipeline falls back to
    // globalThis — which we also clear.
    const g = globalThis as unknown as { createImageBitmap?: unknown };
    const globalBitmap = g.createImageBitmap;
    g.createImageBitmap = undefined;
    try {
      const deps: PipelineDeps = {};
      await expect(processImage(jpeg, deps)).rejects.toMatchObject({
        code: 'NO_IMAGE_BITMAP',
      });
    } finally {
      g.createImageBitmap = globalBitmap;
    }
  });
});
