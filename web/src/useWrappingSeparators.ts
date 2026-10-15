import { useLayoutEffect, useRef } from "react";

export function useWrappingSeparators() {
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const container = ref.current;
    if (!container) return;
    const items = Array.from(container.children) as HTMLElement[];
    const updateSeparators = () => {
      const centers = items.map((item) => {
        const bounds = item.getBoundingClientRect();
        return bounds.top + bounds.height / 2;
      });
      for (const [index, item] of items.entries()) {
        // 居中对齐的同一行可能高度不同，按垂直中心判断。
        item.dataset.separator = String(
          index + 1 < items.length &&
            Math.abs(centers[index] - centers[index + 1]) < 1,
        );
      }
    };
    const observer = new ResizeObserver(updateSeparators);
    observer.observe(container);
    items.forEach((item) => observer.observe(item));
    updateSeparators();
    return () => observer.disconnect();
  }, []);

  return ref;
}
