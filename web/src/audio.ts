export const AUDIO_INDEX_URL =
  "https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/index.json";
const AUDIO_PREFIX =
  "https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/";
const MAX_DOWNLOADS = 2;

type AudioIndex = { audio?: unknown };

function validAudioURL(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.startsWith(AUDIO_PREFIX) &&
    /.(?:mp3|ogg|wav|m4a)(?:$|\?)/i.test(value)
  );
}

export class NailongAudioPlayer {
  private readonly audio = new Audio();
  private readonly cached = new Map<string, string>();
  private readonly controllers = new Set<AbortController>();
  private urls: string[] = [];
  private disposed = false;
  private loaded = false;

  constructor(private readonly onReady?: () => void) {
    this.audio.volume = 0.36;
    this.audio.preload = "auto";
  }

  async load() {
    if (this.disposed || this.loaded) return;
    const controller = new AbortController();
    this.controllers.add(controller);
    try {
      const response = await fetch(AUDIO_INDEX_URL, {
        signal: controller.signal,
      });
      if (!response.ok) return;
      const index = (await response.json()) as AudioIndex;
      const values = Array.isArray(index.audio) ? index.audio : [];
      this.urls = [...new Set(values.filter(validAudioURL))];
      if (!this.urls.length || this.disposed) return;
      this.loaded = true;
      this.onReady?.();
      await this.preload();
    } catch {
      // Audio is optional and must never affect the page.
    } finally {
      this.controllers.delete(controller);
    }
  }

  private async preload() {
    let next = 0;
    const worker = async () => {
      while (!this.disposed) {
        const index = next++;
        if (index >= this.urls.length) return;
        const url = this.urls[index];
        const controller = new AbortController();
        this.controllers.add(controller);
        try {
          const response = await fetch(url, { signal: controller.signal });
          if (response.ok && !this.disposed)
            this.cached.set(url, URL.createObjectURL(await response.blob()));
        } catch {
          // A later click can still play the original CDN URL.
        } finally {
          this.controllers.delete(controller);
        }
      }
    };
    await Promise.all(
      Array.from({ length: Math.min(MAX_DOWNLOADS, this.urls.length) }, worker),
    );
  }

  play() {
    if (this.disposed || !this.urls.length) return;
    const url = this.urls[Math.floor(Math.random() * this.urls.length)];
    this.audio.pause();
    this.audio.src = this.cached.get(url) ?? url;
    this.audio.currentTime = 0;
    this.audio.volume = 0.36;
    void this.audio.play().catch(() => undefined);
  }

  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    for (const controller of this.controllers) controller.abort();
    this.controllers.clear();
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.audio.load();
    for (const url of this.cached.values()) URL.revokeObjectURL(url);
    this.cached.clear();
    this.urls = [];
  }
}
