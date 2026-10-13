import { Volume2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { AUDIO_INDEX_URL, NailongAudioPlayer } from "./audio";

export function AudioButton() {
  const [ready, setReady] = useState(false);
  const player = useRef<NailongAudioPlayer | null>(null);
  useEffect(() => {
    const instance = new NailongAudioPlayer(() => setReady(true));
    player.current = instance;
    const idle =
      window.requestIdleCallback?.(() => void instance.load()) ??
      window.setTimeout(() => void instance.load(), 0);
    return () => {
      window.cancelIdleCallback?.(idle as number);
      window.clearTimeout(idle as number);
      instance.dispose();
    };
  }, []);
  return (
    <button
      className="audio-button"
      type="button"
      aria-label="播放奶龙音频"
      title="播放奶龙音频"
      disabled={!ready}
      onClick={() => player.current?.play()}
    >
      <Volume2 size={18} />
    </button>
  );
}

export { AUDIO_INDEX_URL };
