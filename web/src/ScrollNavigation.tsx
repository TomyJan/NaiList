import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ArrowUp } from "lucide-react";

export function ScrollNavigation({
  children,
  route,
  locked,
}: {
  children: ReactNode;
  route: string;
  locked: boolean;
}) {
  const navRef = useRef<HTMLElement>(null);
  const returningToTop = useRef(false);
  const visible = useRef(false);
  const [showBackToTop, setShowBackToTop] = useState(false);

  useLayoutEffect(() => {
    const nav = navRef.current!;
    const motion = matchMedia("(prefers-reduced-motion: reduce)");
    const scrollY = () =>
      Math.max(
        0,
        Math.min(
          window.scrollY,
          document.documentElement.scrollHeight - innerHeight,
        ),
      );
    let previousY = scrollY();
    let offset = 0;
    let upwardDistance = 0;
    let height = nav.offsetHeight;
    let frame = 0;
    visible.current = false;
    returningToTop.current = false;
    setShowBackToTop(false);
    nav.style.setProperty("--nav-offset", "0px");

    const setVisible = (next: boolean) => {
      if (next === visible.current) return;
      visible.current = next;
      setShowBackToTop(next);
    };
    const reveal = () => {
      if (!document.activeElement?.matches(":focus-visible")) return;
      offset = 0;
      nav.style.setProperty("--nav-offset", "0px");
    };
    const finishReturn = () => {
      returningToTop.current = false;
    };
    const interruptReturn = () => {
      if (!returningToTop.current) return;
      finishReturn();
      // Stop the automatic motion before the user's own scroll takes over.
      window.scrollTo({ top: window.scrollY, behavior: "instant" });
    };
    const keyScroll = (event: KeyboardEvent) => {
      if (
        !event.defaultPrevented &&
        [
          "ArrowUp",
          "ArrowDown",
          "PageUp",
          "PageDown",
          "Home",
          "End",
          " ",
        ].includes(event.key)
      )
        interruptReturn();
    };
    const update = () => {
      frame = 0;
      const y = scrollY();
      const delta = y - previousY;
      previousY = y;
      const pinned =
        locked ||
        !!document.querySelector("dialog[open]") ||
        (nav.contains(document.activeElement) &&
          document.activeElement?.matches(":focus-visible"));
      // Directly follow scroll distance: no timed transition or React rerender per frame.
      offset =
        y === 0 || pinned || motion.matches
          ? 0
          : Math.max(0, Math.min(height, offset + delta));
      nav.style.setProperty("--nav-offset", `${-offset}px`);

      if (delta > 0 || y === 0) finishReturn();
      if (y <= Math.max(320, innerHeight) || pinned || returningToTop.current) {
        upwardDistance = 0;
        setVisible(false);
      } else if (delta > 0) {
        upwardDistance = 0;
        setVisible(false);
      } else if (delta < 0) {
        upwardDistance -= delta;
        // Ignore tiny touchpad fluctuations; preserve visibility while stationary.
        if (upwardDistance >= 12) setVisible(true);
      }
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    const resize = new ResizeObserver(() => {
      height = nav.offsetHeight;
      schedule();
    });
    resize.observe(nav);
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    window.addEventListener("scrollend", finishReturn);
    window.addEventListener("wheel", interruptReturn, { passive: true });
    window.addEventListener("touchstart", interruptReturn, { passive: true });
    window.addEventListener("keydown", keyScroll);
    motion.addEventListener("change", schedule);
    // Keyboard navigation must never focus a control outside the viewport.
    nav.addEventListener("focusin", reveal);
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      window.removeEventListener("scrollend", finishReturn);
      window.removeEventListener("wheel", interruptReturn);
      window.removeEventListener("touchstart", interruptReturn);
      window.removeEventListener("keydown", keyScroll);
      motion.removeEventListener("change", schedule);
      nav.removeEventListener("focusin", reveal);
    };
  }, [route, locked]);

  return (
    <>
      <header ref={navRef} className="nav">
        {children}
      </header>
      <button
        type="button"
        className="back-to-top"
        aria-label="回到顶部"
        title="回到顶部"
        data-visible={showBackToTop}
        aria-hidden={!showBackToTop}
        inert={!showBackToTop}
        onClick={() => {
          returningToTop.current = true;
          visible.current = false;
          setShowBackToTop(false);
          window.scrollTo({
            top: 0,
            behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
              ? "instant"
              : "smooth",
          });
        }}
      >
        <ArrowUp size={20} aria-hidden="true" />
      </button>
    </>
  );
}
