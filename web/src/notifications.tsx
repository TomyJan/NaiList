import { useLayoutEffect, useState } from "react";
import { createPortal } from "react-dom";
import { CheckCircle2, CircleAlert, Info, X } from "lucide-react";
import {
  ToastContainer,
  cssTransition,
  toast,
  type Id,
} from "react-toastify/unstyled";
import "react-toastify/dist/ReactToastify.css";

const recent: Id[] = [];
function keepRecent(id: Id) {
  recent.push(id);
  // Keep new feedback visible instead of waiting behind older notifications.
  if (recent.length > 3) toast.dismiss(recent.shift());
  return id;
}

export const notify = {
  success: (message: string, duration = 3200) =>
    keepRecent(toast.success(message, { autoClose: duration, role: "status" })),
  error: (message: string) =>
    keepRecent(toast.error(message, { autoClose: 6500, role: "alert" })),
};

const transition = cssTransition({
  enter: "toast-enter",
  exit: "toast-exit",
  collapseDuration: 160,
});

export function Notifications() {
  const [host] = useState(() => document.createElement("div"));
  useLayoutEffect(() => {
    host.className = "toast-host";
    // Native dialogs live above z-index layers and make outside controls inert.
    // Move the same portal host so notifications remain visible and dismissible
    // without remounting the container or losing its queue.
    const place = () => {
      const dialogs =
        document.querySelectorAll<HTMLDialogElement>("dialog[open]");
      const target = dialogs.item(dialogs.length - 1) ?? document.body;
      if (host.parentElement !== target) target.appendChild(host);
    };
    place();
    const observer = new MutationObserver(place);
    observer.observe(document.body, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ["open"],
    });
    return () => {
      observer.disconnect();
      host.remove();
    };
  }, [host]);
  return createPortal(
    <ToastContainer
      position="bottom-center"
      className="app-toasts"
      toastClassName="app-toast"
      aria-label="通知"
      transition={transition}
      pauseOnHover
      closeOnClick={false}
      draggable={false}
      closeButton={({ closeToast }) => (
        <button
          type="button"
          className="toast-close"
          aria-label="关闭提示"
          onClick={closeToast}
        >
          <X size={16} />
        </button>
      )}
      icon={({ type }) =>
        type === "success" ? (
          <CheckCircle2 size={20} />
        ) : type === "error" ? (
          <CircleAlert size={20} />
        ) : (
          <Info size={20} />
        )
      }
    />,
    host,
  );
}
