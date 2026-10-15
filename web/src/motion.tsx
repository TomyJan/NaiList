import {
  createContext,
  useContext,
  useLayoutEffect,
  useRef,
  useState,
  type DialogHTMLAttributes,
  type ReactNode,
  type RefObject,
} from "react";
import { RefreshCw } from "lucide-react";
import { releaseNotificationHost } from "./notificationHost";

const Exiting = createContext(false);
const reducedMotion = () =>
  matchMedia("(prefers-reduced-motion: reduce)").matches;

// Retain the last rendered content through its exit, including native backdrops.
// A timer guarantees cleanup even when animations are disabled or interrupted.
export function Presence({
  visible,
  children,
}: {
  visible: boolean;
  children: ReactNode;
}) {
  const [retained, setRetained] = useState(visible);
  const content = useRef(children);
  useLayoutEffect(() => {
    if (visible) content.current = children;
  }, [visible, children]);
  useLayoutEffect(() => {
    if (visible) {
      setRetained(true);
      return;
    }
    const timer = setTimeout(
      () => setRetained(false),
      reducedMotion() ? 0 : 160,
    );
    return () => clearTimeout(timer);
  }, [visible]);
  if (!visible && !retained) return null;
  return (
    <Exiting.Provider value={!visible}>
      <div className="motion-presence" data-exiting={!visible} inert={!visible}>
        {visible ? children : content.current}
      </div>
    </Exiting.Provider>
  );
}

export function MotionDialog({
  ref: suppliedRef,
  onCancel,
  ...props
}: DialogHTMLAttributes<HTMLDialogElement> & {
  ref?: RefObject<HTMLDialogElement | null>;
}) {
  const ownRef = useRef<HTMLDialogElement>(null);
  const ref = suppliedRef ?? ownRef;
  const exiting = useContext(Exiting);
  useLayoutEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => {
      releaseNotificationHost(dialog);
      dialog.close();
    };
  }, [ref]);
  return (
    <dialog
      {...props}
      ref={ref}
      inert={exiting}
      onCancel={(event) => {
        // Esc must run the same exit lifecycle as explicit close buttons.
        event.preventDefault();
        if (!exiting) onCancel?.(event);
      }}
    />
  );
}

export function PageTransition({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    if (reducedMotion()) return;
    const animation = ref.current!.animate(
      [
        { opacity: 0, transform: "translateY(6px)" },
        { opacity: 1, transform: "translateY(0)" },
      ],
      { duration: 220, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" },
    );
    return () => animation.cancel();
  }, [value]);
  return (
    <main ref={ref} className="main">
      {children}
    </main>
  );
}

const tabPositions = new Map<string, { width: string; transform: string }>();

// Preserve only visual geometry across admin route mounts, never page state.
export function TabIndicator({
  value,
  scope,
}: {
  value: string;
  scope?: string;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  const positioned = useRef(false);
  useLayoutEffect(() => {
    const line = ref.current!;
    const track = line.parentElement!;
    const container = track.parentElement!;
    let disposed = false;
    let first = true;
    let animation: Animation | undefined;
    const measure = () => {
      if (disposed) return;
      const active = container.querySelector<HTMLElement>(".active");
      if (!active) return;
      const position = {
        width: `${active.offsetWidth}px`,
        transform: `translateX(${active.offsetLeft}px)`,
      };
      if (!positioned.current) line.style.transition = "none";
      line.style.top = `${active.offsetTop + active.offsetHeight - 3}px`;
      line.style.width = position.width;
      line.style.transform = position.transform;
      if (!positioned.current) {
        // 首次定位在绘制前完成，后续 Tab 切换才启用过渡。
        void line.offsetWidth;
        line.style.transition = "";
        positioned.current = true;
      }
      const previous = scope ? tabPositions.get(scope) : undefined;
      if (first && previous && !reducedMotion()) {
        animation = line.animate([previous, position], {
          duration: 240,
          easing: "cubic-bezier(0.2, 0.8, 0.2, 1)",
        });
      }
      if (scope) tabPositions.set(scope, position);
      first = false;
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    for (const child of container.children)
      if (child !== track) observer.observe(child);
    void document.fonts.ready.then(measure);
    return () => {
      disposed = true;
      animation?.cancel();
      observer.disconnect();
    };
  }, [value, scope]);
  return (
    <span className="tab-indicator-track" aria-hidden="true">
      <span className="tab-indicator" ref={ref} />
    </span>
  );
}

export function RefreshIcon({
  active,
  cycle = 0,
  size = 20,
}: {
  active: boolean;
  cycle?: number;
  size?: number;
}) {
  const [spinning, setSpinning] = useState(active);
  const started = useRef(active ? performance.now() : 0);
  const previousCycle = useRef(cycle);
  useLayoutEffect(() => {
    const clicked = previousCycle.current !== cycle;
    previousCycle.current = cycle;
    if (active || clicked) {
      if (!started.current) started.current = performance.now();
      setSpinning(true);
    }
    if (!active && started.current) {
      const remaining = 700 - ((performance.now() - started.current) % 700);
      const timer = setTimeout(() => {
        started.current = 0;
        setSpinning(false);
      }, remaining);
      return () => clearTimeout(timer);
    }
  }, [active, cycle]);
  return (
    <RefreshCw
      size={size}
      aria-hidden="true"
      className={spinning ? "refresh-spinning" : undefined}
    />
  );
}
