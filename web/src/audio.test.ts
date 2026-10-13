import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AUDIO_INDEX_URL, NailongAudioPlayer } from "./audio";

const urls = ["one.mp3", "two.mp3", "three.mp3"].map(
  (name) =>
    new URL(
      name,
      "https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/",
    ).href,
);
let audio: {
  src: string;
  volume: number;
  currentTime: number;
  pause: ReturnType<typeof vi.fn>;
  play: ReturnType<typeof vi.fn>;
  load: ReturnType<typeof vi.fn>;
  removeAttribute: ReturnType<typeof vi.fn>;
};
const fetchMock = vi.fn<typeof fetch>();
const indexResponse = (audio: unknown = urls) =>
  new Response(JSON.stringify({ audio }));
const audioResponse = () =>
  new Response(new Blob(["audio"], { type: "audio/mpeg" }));

beforeEach(() => {
  audio = {
    src: "",
    volume: 1,
    currentTime: 0,
    pause: vi.fn(),
    play: vi.fn().mockResolvedValue(undefined),
    load: vi.fn(),
    removeAttribute: vi.fn(),
  };
  vi.stubGlobal(
    "Audio",
    vi.fn(function () {
      return audio;
    }),
  );
  vi.stubGlobal("fetch", fetchMock.mockReset());
  let id = 0;
  vi.spyOn(URL, "createObjectURL").mockImplementation(
    () => `blob:audio-${++id}`,
  );
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("奶龙音频", () => {
  it("预加载全部音频但不自动播放，点击随机播放且音量为 36%", async () => {
    fetchMock
      .mockResolvedValueOnce(indexResponse())
      .mockImplementation(async () => audioResponse());
    const ready = vi.fn();
    const error = vi.fn();
    const player = new NailongAudioPlayer(ready, error);
    await player.load();
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(fetchMock.mock.calls[0][0]).toBe(AUDIO_INDEX_URL);
    expect(ready).toHaveBeenCalledOnce();
    expect(audio.play).not.toHaveBeenCalled();
    vi.spyOn(Math, "random").mockReturnValue(0);
    player.play();
    const first = audio.src;
    vi.mocked(Math.random).mockReturnValue(0.99);
    player.play();
    expect(audio.src).not.toBe(first);
    expect(audio.src).toMatch(/^blob:/);
    expect(audio.volume).toBe(0.36);
    expect(audio.currentTime).toBe(0);
    expect(audio.pause).toHaveBeenCalledTimes(2);
    expect(audio.play).toHaveBeenCalledTimes(2);
    expect(error).not.toHaveBeenCalled();
    player.dispose();
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(3);
  });

  it("最多同时下载两段，未缓存也可在点击时播放原地址", async () => {
    const downloads: Array<() => void> = [];
    fetchMock
      .mockResolvedValueOnce(indexResponse())
      .mockImplementation(
        () =>
          new Promise((resolve) =>
            downloads.push(() => resolve(audioResponse())),
          ),
      );
    const player = new NailongAudioPlayer();
    const loading = player.load();
    await vi.waitFor(() => expect(downloads).toHaveLength(2));
    vi.spyOn(Math, "random").mockReturnValue(0.99);
    player.play();
    expect(audio.src).toBe(urls[2]);
    downloads[0]();
    await vi.waitFor(() => expect(downloads).toHaveLength(3));
    downloads[1]();
    downloads[2]();
    await loading;
    player.dispose();
  });

  it.each([
    new Response("oops", { status: 503 }),
    new Response("invalid JSON"),
    indexResponse("invalid"),
    indexResponse([]),
  ])("索引错误或为空时提示加载失败", async (response) => {
    fetchMock.mockResolvedValueOnce(response);
    const ready = vi.fn();
    const error = vi.fn();
    const player = new NailongAudioPlayer(ready, error);
    await expect(player.load()).resolves.toBeUndefined();
    player.play();
    expect(ready).not.toHaveBeenCalled();
    expect(error).toHaveBeenCalledWith("奶龙音频加载失败，请稍后重试");
    expect(audio.play).not.toHaveBeenCalled();
    player.dispose();
  });

  it("过滤错误地址、去重，单段下载和播放失败不影响后续点击", async () => {
    fetchMock
      .mockResolvedValueOnce(
        indexResponse([
          urls[0],
          urls[0],
          urls[1],
          42,
          "javascript:alert(1)",
          "https://other.test/audio.mp3",
        ]),
      )
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(audioResponse());
    audio.play.mockRejectedValue(new Error("not supported"));
    const error = vi.fn();
    const player = new NailongAudioPlayer(undefined, error);
    await expect(player.load()).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledTimes(3);
    player.play();
    player.play();
    await Promise.resolve();
    expect(audio.play).toHaveBeenCalledTimes(2);
    expect(error).toHaveBeenCalledTimes(1);
    expect(error).toHaveBeenCalledWith("奶龙音频播放失败，请稍后重试");
    player.dispose();
  });

  it("快速切换忽略上次播放的迟到失败，当前播放失败仍提示", async () => {
    fetchMock
      .mockResolvedValueOnce(indexResponse())
      .mockImplementation(async () => audioResponse());
    const error = vi.fn();
    const player = new NailongAudioPlayer(undefined, error);
    await player.load();
    let rejectPrevious!: (reason: Error) => void;
    let rejectCurrent!: (reason: Error) => void;
    audio.play
      .mockImplementationOnce(
        () =>
          new Promise((_, reject) => {
            rejectPrevious = reject;
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise((_, reject) => {
            rejectCurrent = reject;
          }),
      );
    player.play();
    player.play();
    rejectPrevious(new DOMException("interrupted by pause", "AbortError"));
    await Promise.resolve();
    expect(error).not.toHaveBeenCalled();
    rejectCurrent(new Error("unsupported audio"));
    await Promise.resolve();
    expect(error).toHaveBeenCalledExactlyOnceWith(
      "奶龙音频播放失败，请稍后重试",
    );
    player.dispose();
  });

  it("卸载中止请求，迟到响应不创建 Blob 或启用按钮", async () => {
    let complete!: (response: Response) => void;
    fetchMock
      .mockResolvedValueOnce(indexResponse([urls[0]]))
      .mockImplementation(
        () =>
          new Promise((resolve) => {
            complete = resolve;
          }),
      );
    const player = new NailongAudioPlayer();
    const loading = player.load();
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    player.dispose();
    expect((fetchMock.mock.calls[1][1]?.signal as AbortSignal).aborted).toBe(
      true,
    );
    complete(audioResponse());
    await loading;
    expect(URL.createObjectURL).not.toHaveBeenCalled();
    player.play();
    expect(audio.play).not.toHaveBeenCalled();
  });
});
